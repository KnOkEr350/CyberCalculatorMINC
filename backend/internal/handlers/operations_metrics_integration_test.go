package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// OPS-12 на реальной БД: срез состояния подсистем собирается целиком и
// показывает то, по чему принимают решения — сроки, снимок, хранилище, журнал,
// базу. Инстанс стоит в закрытом контуре, и другого источника этих данных нет.
func TestOperationsMetricsCoverEverySubsystem(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	handlers := AdminHandlers{DB: db}
	admin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	collect := func(user middleware.AuthUser) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		handlers.Metrics(recorder, httptest.NewRequest("GET", "/api/admin/metrics", nil), user)
		return recorder
	}

	t.Run("показатели доступны администратору", func(t *testing.T) {
		recorder := collect(admin)
		if recorder.Code != 200 {
			t.Fatalf("срез вернул %d: %s", recorder.Code, recorder.Body.String())
		}
		var metrics operationsMetrics
		if err := json.Unmarshal(recorder.Body.Bytes(), &metrics); err != nil {
			t.Fatal(err)
		}
		if metrics.CollectedAt.IsZero() {
			t.Fatal("срез без времени сбора бесполезен")
		}
		if !metrics.Database.Reachable {
			t.Fatal("база доступна, показатель должен это отражать")
		}
		if metrics.Timers.ReportYear == 0 || metrics.Snapshot.ReportYear == 0 {
			t.Fatal("срез должен называть отчётный год")
		}
		// Целостность журнала — то, ради чего срез и смотрят.
		if !metrics.Audit.ChainVerified {
			t.Fatalf("цепочка журнала должна быть целой: %s", metrics.Audit.ChainProblem)
		}
		// Счётчик должен расти вместе с журналом, а не просто быть заполнен.
		writeAuditEvent(t, ctx, db, "create", "metrics_probe")
		second := collect(admin)
		var after operationsMetrics
		if err := json.Unmarshal(second.Body.Bytes(), &after); err != nil {
			t.Fatal(err)
		}
		if after.Audit.Records != metrics.Audit.Records+1 {
			t.Fatalf("журнал вырос на %d записей вместо одной", after.Audit.Records-metrics.Audit.Records)
		}
	})

	t.Run("хранилище отражает загруженные документы", func(t *testing.T) {
		before := collect(admin)
		var was operationsMetrics
		if err := json.Unmarshal(before.Body.Bytes(), &was); err != nil {
			t.Fatal(err)
		}

		var entryID string
		if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
			tenant.partner, tenant.agreement, tenant.company, was.Timers.ReportYear, tenant.admin).Scan(&entryID); err != nil {
			t.Fatal(err)
		}
		uploadHandlers := AttachmentHandlers{DB: db, UploadDir: t.TempDir()}
		content := append([]byte("%PDF-1.4\n"), []byte("документ для показателей")...)
		if response := uploadFile(t, uploadHandlers, admin, entryID, "metrics.pdf", content); response.Code != 201 {
			t.Fatalf("загрузка вернула %d: %s", response.Code, response.Body.String())
		}

		after := collect(admin)
		var now operationsMetrics
		if err := json.Unmarshal(after.Body.Bytes(), &now); err != nil {
			t.Fatal(err)
		}
		if now.Storage.Documents <= was.Storage.Documents {
			t.Fatalf("число документов не выросло: было %d, стало %d", was.Storage.Documents, now.Storage.Documents)
		}
		if now.Storage.Bytes <= was.Storage.Bytes {
			t.Fatal("объём хранилища должен учитывать новый документ")
		}
		if now.Storage.DistinctBlobs > now.Storage.Documents {
			t.Fatal("различных файлов не может быть больше, чем ссылок на них")
		}
	})

	t.Run("срез закрыт от неадминистративных ролей", func(t *testing.T) {
		for _, role := range []models.Role{models.RoleCurator, models.RoleHRSpecialist,
			models.RoleFinancialSpecialist, models.RoleLegalSpecialist, models.RoleAuditorViewer} {
			user := middleware.AuthUser{ID: tenant.admin, Role: role,
				EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
			if got := collect(user); got.Code != 403 {
				t.Errorf("%s не должна видеть показатели эксплуатации, получено %d", role, got.Code)
			}
		}
	})
}
