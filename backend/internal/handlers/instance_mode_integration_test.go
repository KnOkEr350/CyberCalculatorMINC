package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// SEC-02 на реальной БД: режим инстанса — свойство экземпляра, а не
// пользователя. Он хранится в настройках, переключается только системным
// администратором и принимает лишь два значения.
func TestInstanceModeIsAdministeredBySuperAdminOnly(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	handlers := AdminHandlers{DB: db}

	setting := func(user middleware.AuthUser, value string) *httptest.ResponseRecorder {
		t.Helper()
		body := `{"key":"system_owner_role","value":"` + value + `"}`
		request := httptest.NewRequest("POST", "/api/admin/settings", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handlers.UpdateSetting(recorder, request, user)
		return recorder
	}
	superAdmin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	t.Run("значение по умолчанию задано", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handlers.GetSettings(recorder, httptest.NewRequest("GET", "/api/admin/settings", nil), superAdmin)
		if recorder.Code != 200 {
			t.Fatalf("настройки вернули %d", recorder.Code)
		}
		var settings map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		if settings["system_owner_role"] != "IT_COMPANY" && settings["system_owner_role"] != "HEI" {
			t.Fatalf("режим инстанса не задан: %q", settings["system_owner_role"])
		}
	})

	t.Run("допустимы только два режима", func(t *testing.T) {
		if got := setting(superAdmin, "PARTNER"); got.Code != 400 {
			t.Fatalf("произвольный режим не должен приниматься: %d %s", got.Code, got.Body.String())
		}
		for _, mode := range []string{"HEI", "IT_COMPANY"} {
			if got := setting(superAdmin, mode); got.Code != 200 {
				t.Fatalf("режим %s должен приниматься: %d %s", mode, got.Code, got.Body.String())
			}
		}
	})

	t.Run("переключает только системный администратор", func(t *testing.T) {
		for _, role := range []models.Role{models.RoleHoldingAdmin, models.RoleOrgAdmin,
			models.RoleCurator, models.RoleAuditorViewer} {
			user := middleware.AuthUser{ID: tenant.admin, Role: role,
				EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
			if got := setting(user, "HEI"); got.Code != 403 {
				t.Errorf("%s не может переключать режим инстанса, получено %d", role, got.Code)
			}
		}
	})

	t.Run("режим сохраняется и читается", func(t *testing.T) {
		if got := setting(superAdmin, "HEI"); got.Code != 200 {
			t.Fatalf("сохранение вернуло %d: %s", got.Code, got.Body.String())
		}
		var value string
		if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='system_owner_role'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value != "HEI" {
			t.Fatalf("сохранён режим %q", value)
		}
		// Возвращаем значение по умолчанию, чтобы не влиять на другие тесты.
		if got := setting(superAdmin, "IT_COMPANY"); got.Code != 200 {
			t.Fatal("не удалось вернуть режим по умолчанию")
		}
	})
}
