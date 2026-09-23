package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// DATA-12 и INT-08 на реальной БД: реквизиты документа, сброс проверки при их
// изменении и юридическое сомнение с причиной, историей и правами.
func TestDocumentMetadataAndLegalDispute(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)
	handlers := AttachmentHandlers{DB: db, UploadDir: t.TempDir()}

	var entryID, attachmentID string
	if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
		audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
		own.partner, own.agreement, own.company, year, own.admin).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,
		uploaded_by,retention_expires_at,document_type,review_status,reviewed_by,reviewed_at,content_sha256)
		VALUES($1,'a.pdf','x/a.bin','application/pdf',10,$2,$3,'practice_agreement','approved',$2,now(),repeat('a',64)) RETURNING id::text`,
		entryID, own.admin, time.Now().Add(24*time.Hour)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}

	owner := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
	stranger := middleware.AuthUser{ID: foreign.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &foreign.company}
	hr := middleware.AuthUser{ID: own.admin, Role: models.RoleHRSpecialist, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
	legal := middleware.AuthUser{ID: own.admin, Role: models.RoleLegalSpecialist, EntityType: models.EntityOrganization, ITCompanyID: &own.company}

	patch := func(u middleware.AuthUser, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PATCH", "/api/attachments/"+attachmentID+"/metadata", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handlers.UpdateMetadata(rec, req, u, attachmentID)
		return rec
	}
	reviewStatus := func() string {
		var s string
		if err := db.QueryRowContext(ctx, `SELECT review_status FROM attachments WHERE id::text=$1`, attachmentID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	const meta = `{"document_number":"ДП-17","document_date":"2024-03-01","signer_name":"Иванов Иван","certificate_serial":"0A1B2C3D","certificate_valid_from":"2024-01-01","certificate_valid_until":"2025-01-01"}`

	t.Run("чужой арендатор реквизиты не меняет", func(t *testing.T) {
		if rec := patch(stranger, meta); rec.Code != 403 && rec.Code != 404 {
			t.Fatalf("ожидался отказ, получено %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("роль без права загрузки такого типа отклоняется", func(t *testing.T) {
		if rec := patch(hr, meta); rec.Code != 403 {
			t.Fatalf("HR не владеет практическим договором: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("подпись описывается целиком", func(t *testing.T) {
		if rec := patch(owner, `{"signer_name":"Иванов Иван"}`); rec.Code != 400 {
			t.Fatalf("неполные сведения о подписи должны отклоняться: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("изменение реквизитов возвращает документ на проверку", func(t *testing.T) {
		if rec := patch(owner, meta); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"review_reset":true`) {
			t.Fatalf("реквизиты не сохранены: %d %s", rec.Code, rec.Body.String())
		}
		if got := reviewStatus(); got != "pending" {
			t.Fatalf("после изменения реквизитов проверка должна быть сброшена, статус %q", got)
		}
	})
	t.Run("повтор тех же реквизитов проверку не сбрасывает", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE attachments SET review_status='approved',reviewed_by=$2::uuid,reviewed_at=now() WHERE id::text=$1`, attachmentID, own.admin); err != nil {
			t.Fatal(err)
		}
		if rec := patch(owner, meta); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"review_reset":false`) {
			t.Fatalf("повтор: %d %s", rec.Code, rec.Body.String())
		}
		if got := reviewStatus(); got != "approved" {
			t.Fatalf("неизменившиеся реквизиты не должны лишать одобрения, статус %q", got)
		}
	})

	dispute := func(u middleware.AuthUser, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/entries/"+entryID+"/legal-disputes", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handlers.ChangeLegalDispute(rec, req, u, entryID)
		return rec
	}
	activeDisputes := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM legal_disputes WHERE entry_id::text=$1 AND lifted_at IS NULL`, entryID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("сомнение ставит только юридическое управление и только с причиной", func(t *testing.T) {
		if rec := dispute(hr, `{"action":"raise","reason":"подозрение"}`); rec.Code != 403 {
			t.Fatalf("HR не ставит сомнение: %d", rec.Code)
		}
		if rec := dispute(stranger, `{"action":"raise","reason":"чужая запись"}`); rec.Code != 403 && rec.Code != 404 {
			t.Fatalf("чужой арендатор: %d", rec.Code)
		}
		if rec := dispute(legal, `{"action":"raise","reason":"   "}`); rec.Code != 400 {
			t.Fatalf("без причины сомнение недопустимо: %d", rec.Code)
		}
		if rec := dispute(legal, `{"action":"maybe","reason":"x"}`); rec.Code != 400 {
			t.Fatalf("неизвестное действие: %d", rec.Code)
		}
		if activeDisputes() != 0 {
			t.Fatal("отклонённые запросы не должны оставлять сомнений")
		}
	})
	t.Run("документ должен относиться к записи", func(t *testing.T) {
		rec := dispute(legal, `{"action":"raise","reason":"x","attachment_id":"00000000-0000-0000-0000-000000000001"}`)
		if rec.Code != 400 {
			t.Fatalf("чужой документ: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("постановка, повтор, снятие", func(t *testing.T) {
		if rec := dispute(legal, `{"action":"raise","reason":"подпись вызывает сомнение","attachment_id":"`+attachmentID+`"}`); rec.Code != 200 {
			t.Fatalf("постановка: %d %s", rec.Code, rec.Body.String())
		}
		if rec := dispute(legal, `{"action":"raise","reason":"ещё раз"}`); rec.Code != 409 {
			t.Fatalf("второе действующее сомнение недопустимо: %d", rec.Code)
		}
		// Граница зачёта одна для дашбордов, отчётов и выгрузок — её определение
		// обязано учитывать действующее сомнение.
		var definition string
		if err := db.QueryRowContext(ctx, `SELECT pg_get_viewdef('entry_eligibility'::regclass)`).Scan(&definition); err != nil ||
			!strings.Contains(definition, "legal_disputes") {
			t.Fatalf("entry_eligibility не учитывает юридическое сомнение: %v", err)
		}
		var eligible bool
		if err := db.QueryRowContext(ctx, `SELECT eligible FROM entry_eligibility WHERE id::text=$1`, entryID).Scan(&eligible); err != nil || eligible {
			t.Fatalf("запись под сомнением не может быть допущена к зачёту: eligible=%v err=%v", eligible, err)
		}
		if rec := dispute(legal, `{"action":"lift","reason":"подпись подтверждена"}`); rec.Code != 200 {
			t.Fatalf("снятие: %d %s", rec.Code, rec.Body.String())
		}
		if rec := dispute(legal, `{"action":"lift","reason":"ещё раз"}`); rec.Code != 409 {
			t.Fatalf("снимать нечего: %d", rec.Code)
		}
		if activeDisputes() != 0 {
			t.Fatal("после снятия действующих сомнений быть не должно")
		}
	})
	t.Run("история сохраняется и не переписывается", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handlers.LegalDisputes(rec, httptest.NewRequest("GET", "/api/entries/"+entryID+"/legal-disputes", nil), owner, entryID)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "подпись вызывает сомнение") || !strings.Contains(rec.Body.String(), "подпись подтверждена") {
			t.Fatalf("история неполна: %d %s", rec.Code, rec.Body.String())
		}
		if _, err := db.ExecContext(ctx, `UPDATE legal_disputes SET reason='иначе' WHERE entry_id::text=$1`, entryID); err == nil {
			t.Fatal("причину сомнения менять нельзя")
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM legal_disputes WHERE entry_id::text=$1`, entryID); err == nil {
			t.Fatal("сомнение удалять нельзя")
		}
	})
}
