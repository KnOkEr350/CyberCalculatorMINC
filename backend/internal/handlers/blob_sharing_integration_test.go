package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"cybercalc/internal/filestore"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/retention"
	"cybercalc/internal/testfixtures"
)

// uploadFile отправляет файл через реальный обработчик загрузки.
func uploadFile(t *testing.T, handlers AttachmentHandlers, user middleware.AuthUser, entryID, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("files", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/entries/"+entryID+"/attachments?document_type=labor_contract", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	handlers.Upload(recorder, request, user, entryID)
	return recorder
}

// STORE-02 на реальной БД: один blob обслуживает несколько типизированных
// ссылок. Одинаковый документ, приложенный к двум мероприятиям, занимает в
// хранилище один файл, но остаётся двумя самостоятельными записями.
func TestOneBlobServesSeveralAttachments(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	uploadDir := t.TempDir()
	handlers := AttachmentHandlers{DB: db, UploadDir: uploadDir}
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	newEntry := func() string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
			tenant.partner, tenant.agreement, tenant.company, year, tenant.admin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second := newEntry(), newEntry()
	// Реальный PDF: загрузка проверяет соответствие содержимого расширению.
	content := append([]byte("%PDF-1.4\n"), []byte("трудовой договор стажёра, один и тот же файл в двух мероприятиях")...)

	if got := uploadFile(t, handlers, user, first, "contract.pdf", content); got.Code != 201 {
		t.Fatalf("первая загрузка вернула %d: %s", got.Code, got.Body.String())
	}
	if got := uploadFile(t, handlers, user, second, "contract.pdf", content); got.Code != 201 {
		t.Fatalf("вторая загрузка вернула %d: %s", got.Code, got.Body.String())
	}

	// Две записи, один файл на диске.
	var references, blobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*),count(DISTINCT storage_path) FROM attachments WHERE entry_id::text IN ($1,$2)`,
		first, second).Scan(&references, &blobs); err != nil {
		t.Fatal(err)
	}
	if references != 2 {
		t.Fatalf("ожидались две ссылки на документ, получено %d", references)
	}
	if blobs != 1 {
		t.Fatalf("одинаковое содержимое должно лежать в одном blob, путей %d", blobs)
	}
	var storagePath string
	if err := db.QueryRowContext(ctx, `SELECT storage_path FROM attachments WHERE entry_id::text=$1`, first).Scan(&storagePath); err != nil {
		t.Fatal(err)
	}
	if !filestore.IsBlobPath(uploadDir, storagePath) {
		t.Fatalf("загруженный файл должен адресоваться содержимым, путь %q", storagePath)
	}
	if err := filestore.VerifyBlob(uploadDir, storagePath); err != nil {
		t.Fatalf("содержимое не соответствует адресу: %v", err)
	}

	// Снятие одной ссылки не должно уносить документ второго мероприятия.
	if _, err := db.ExecContext(ctx, `DELETE FROM attachments WHERE entry_id::text=$1`, first); err != nil {
		t.Fatal(err)
	}
	retention.DrainDeletionQueue(ctx, db, uploadDir)
	if _, err := os.Stat(storagePath); err != nil {
		t.Fatalf("blob удалён, хотя на него осталась ссылка: %v", err)
	}

	// Снятие последней ссылки освобождает файл.
	if _, err := db.ExecContext(ctx, `DELETE FROM attachments WHERE entry_id::text=$1`, second); err != nil {
		t.Fatal(err)
	}
	retention.DrainDeletionQueue(ctx, db, uploadDir)
	if _, err := os.Stat(storagePath); !os.IsNotExist(err) {
		t.Fatalf("после удаления последней ссылки файл должен исчезнуть: %v", err)
	}
}
