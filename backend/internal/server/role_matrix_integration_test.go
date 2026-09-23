package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/apicontract"
	"cybercalc/internal/auth"
	"cybercalc/internal/config"
	"cybercalc/internal/dbx"
	"cybercalc/internal/models"
	"cybercalc/internal/platform/featureflags"
	"cybercalc/internal/testfixtures"
)

// SEC-09: матрица «роль × маршрут» по всему инвентарю. Каждый зарегистрированный
// маршрут вызывается от имени каждой роли реальной сессией на реальной БД, а
// результат сверяется с зафиксированным эталоном testdata/role_matrix.golden.
//
// Классы ответа:
//   - 401 — не принят как вошедший;
//   - 403 — роль отклонена до обращения к данным;
//   - pass — запрос дошёл до обработчика (любой статус, кроме 401, 403 и 5xx:
//     400 на пустое тело или 404 на несуществующий объект тоже считаются
//     допуском — обработчик рассматривает запрос по существу);
//   - 5xx — ошибка сервера: недопустимо ни для одной роли.
//
// Эталон не «одобряет всё, что есть»: он фиксирует, кому что открыто, и любое
// изменение видно в обзоре. Новый маршрут или смена доступа роли ломают тест,
// пока строка эталона не пересмотрена (UPDATE_GOLDEN=1 переписывает файл).
// Доступ на уровне объекта (чужой арендатор, чужой партнёр) проверяют
// отдельные интеграционные тесты: здесь объекта нет намеренно.

type matrixRole struct {
	name   string
	role   models.Role
	entity models.EntityType
}

var matrixRoles = []matrixRole{
	{"super_admin", models.RoleSuperAdmin, models.EntityOrganization},
	{"holding_admin", models.RoleHoldingAdmin, models.EntityOrganization},
	{"org_admin", models.RoleOrgAdmin, models.EntityOrganization},
	{"curator", models.RoleCurator, models.EntityOrganization},
	{"hr_specialist", models.RoleHRSpecialist, models.EntityOrganization},
	{"financial_specialist", models.RoleFinancialSpecialist, models.EntityOrganization},
	{"legal_specialist", models.RoleLegalSpecialist, models.EntityOrganization},
	{"auditor_viewer", models.RoleAuditorViewer, models.EntityOrganization},
	{"edu_curator", models.RoleCurator, models.EntityEduInst},
}

func matrixDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("isolated workspace_test database required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRoleByEndpointMatrix(t *testing.T) {
	db := matrixDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())

	root, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	partner, err := f.CreatePartner(ctx, company, university)
	if err != nil {
		t.Fatal(err)
	}

	tokens := map[string]string{}
	for _, r := range matrixRoles {
		params := testfixtures.UserParams{Role: r.role, EntityType: r.entity}
		if r.entity == models.EntityOrganization {
			params.ITCompanyID = company.ID
		} else {
			params.PartnerID = partner.ID
		}
		user, err := f.CreateUser(ctx, params)
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		session, err := auth.InsertSession(ctx, db, user.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		tokens[r.name] = session.Token
	}

	flags, err := featureflags.Parse("all")
	if err != nil {
		t.Fatal(err)
	}
	handler := BuildRoutes(db, config.Config{UploadDir: t.TempDir(), BackendFeatureFlags: flags,
		MFAKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="})

	operations, err := apicontract.DiscoverModuleRoutes(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	const objectID = "00000000-0000-4000-8000-000000000001"
	var lines []string
	for _, operation := range operations {
		name := operation.String()
		if _, public := publicOperations[name]; public {
			continue
		}
		path := operation.Path
		for strings.Contains(path, "{") {
			start := strings.Index(path, "{")
			end := strings.Index(path[start:], "}")
			path = path[:start] + objectID + path[start+end+1:]
		}
		classes := make([]string, 0, len(matrixRoles))
		for _, r := range matrixRoles {
			var request *http.Request
			if operation.Method == http.MethodGet || operation.Method == http.MethodHead {
				request = httptest.NewRequest(operation.Method, path, nil)
			} else {
				request = httptest.NewRequest(operation.Method, path, strings.NewReader("{}"))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Cybercalc-Request", "1")
			}
			request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[r.name]})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			class := "pass"
			switch {
			case recorder.Code == http.StatusUnauthorized:
				class = "401"
			case recorder.Code == http.StatusForbidden:
				class = "403"
			case recorder.Code >= 500:
				t.Errorf("%s от имени %s: ошибка сервера %d: %s", name, r.name, recorder.Code, strings.TrimSpace(recorder.Body.String()))
				class = "5xx"
			}
			classes = append(classes, r.name+"="+class)
		}
		lines = append(lines, name+" | "+strings.Join(classes, " "))
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"

	golden := filepath.Join("testdata", "role_matrix.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("эталон переписан: %d маршрутов", len(lines))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("нет эталона (создайте UPDATE_GOLDEN=1): %v", err)
	}
	if string(want) != got {
		wantLines, gotLines := strings.Split(string(want), "\n"), strings.Split(got, "\n")
		wantSet := map[string]bool{}
		for _, l := range wantLines {
			wantSet[l] = true
		}
		gotSet := map[string]bool{}
		for _, l := range gotLines {
			gotSet[l] = true
		}
		var diff []string
		for _, l := range gotLines {
			if !wantSet[l] && l != "" {
				diff = append(diff, "+ "+l)
			}
		}
		for _, l := range wantLines {
			if !gotSet[l] && l != "" {
				diff = append(diff, "- "+l)
			}
		}
		t.Fatalf("матрица доступа изменилась (проверьте, что это намеренно, и перепишите эталон UPDATE_GOLDEN=1):\n%s", fmt.Sprint(strings.Join(diff, "\n")))
	}
}
