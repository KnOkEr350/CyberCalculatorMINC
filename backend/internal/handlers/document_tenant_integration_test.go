package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"cybercalc/internal/filestore"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// STORE-02 на реальной БД: подтверждающий документ виден только своему
// арендатору. Проверка идёт через сами обработчики: список вложений записи и
// скачивание файла — оба пути должны отказывать соседней ИТ-компании.
func TestDocumentsAreIsolatedPerTenant(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)

	uploadDir := t.TempDir()
	handlers := AttachmentHandlers{DB: db, UploadDir: uploadDir}

	// Мероприятие и приложенный к нему типизированный документ арендатора A.
	var entryID string
	if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
		audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
		own.partner, own.agreement, own.company, year, own.admin).Scan(&entryID); err != nil {
		t.Fatal(err)
	}

	// Файл кладём тем же путём, что и обработчик загрузки: storage_path —
	// это в точности то, что возвращает хранилище.
	content := []byte("договор о практической подготовке")
	digest := sha256.Sum256(content)
	file, storagePath, err := filestore.Create(uploadDir, "entry", "isolated.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	file.Close()
	var attachmentID string
	if err := db.QueryRowContext(ctx, `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,
		uploaded_by,retention_expires_at,document_type,review_status,content_sha256)
		VALUES($1,'practice.pdf',$2,'application/pdf',$3,$4,$5,'practice_agreement','pending',$6) RETURNING id::text`,
		entryID, storagePath, len(content), own.admin, time.Now().Add(365*24*time.Hour),
		hex.EncodeToString(digest[:])).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}

	user := func(tenant tenantScenario) middleware.AuthUser {
		return middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
	}
	list := func(tenant tenantScenario) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handlers.ListForEntry(recorder, httptest.NewRequest("GET", "/api/entries/"+entryID+"/attachments", nil), user(tenant), entryID)
		return recorder
	}
	download := func(tenant tenantScenario) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handlers.Download(recorder, httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/download", nil), user(tenant), attachmentID)
		return recorder
	}

	// Свой арендатор видит документ вместе с его типом и статусом проверки.
	listed := list(own)
	if listed.Code != 200 {
		t.Fatalf("список вложений вернул %d: %s", listed.Code, listed.Body.String())
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(listed.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("ожидался один документ, получено %d", len(items))
	}
	if items[0]["document_type"] != "practice_agreement" || items[0]["review_status"] != "pending" {
		t.Fatalf("тип и статус проверки должны возвращаться: %+v", items[0])
	}
	if got := download(own); got.Code != 200 {
		t.Fatalf("свой арендатор должен скачивать документ, получено %d: %s", got.Code, got.Body.String())
	}

	// Соседний арендатор не видит ни список, ни файл.
	if got := list(foreign); got.Code == 200 {
		t.Fatalf("другая ИТ-компания не должна получать список вложений: %s", got.Body.String())
	}
	if got := download(foreign); got.Code == 200 {
		t.Fatal("другая ИТ-компания не должна скачивать документ")
	}
}

// Целостность документа проверяется при каждой выдаче: подменённые на диске
// байты не уходят пользователю под видом подтверждающего документа.
func TestTamperedDocumentIsNotServed(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	uploadDir := t.TempDir()
	handlers := AttachmentHandlers{DB: db, UploadDir: uploadDir}

	var entryID string
	if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
		audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
		tenant.partner, tenant.agreement, tenant.company, year, tenant.admin).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	// В БД записан хеш исходных байтов, а на диск кладём другие: так
	// выглядит подмена файла в хранилище.
	original := []byte("акт сдачи-приёмки")
	digest := sha256.Sum256(original)
	file, storagePath, err := filestore.Create(uploadDir, "entry", "tampered.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("подменённое содержимое")); err != nil {
		t.Fatal(err)
	}
	file.Close()
	var attachmentID string
	if err := db.QueryRowContext(ctx, `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,
		uploaded_by,retention_expires_at,document_type,review_status,content_sha256)
		VALUES($1,'act.pdf',$2,'application/pdf',$3,$4,$5,'acceptance_act','pending',$6) RETURNING id::text`,
		entryID, storagePath, len(original), tenant.admin, time.Now().Add(365*24*time.Hour),
		hex.EncodeToString(digest[:])).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
	handlers.Download(recorder, httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/download", nil), user, attachmentID)
	if recorder.Code != 410 {
		t.Fatalf("подменённый документ не должен выдаваться, получено %d: %s", recorder.Code, recorder.Body.String())
	}
}
