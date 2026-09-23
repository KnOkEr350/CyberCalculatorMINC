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

// SEC-07: льготный период даёт новому сотруднику время настроить приложение,
// но заканчивается. Политика без требования 2FA не принуждает никого.
func TestMFAGracePeriodEndsAndPolicyGovernsEnforcement(t *testing.T) {
	created := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		policy MFAPolicy
		now    time.Time
		want   bool
	}{
		{"2FA не требуется — принуждения нет", MFAPolicy{Required: false, GraceHours: 0}, created.Add(time.Hour), false},
		{"без льготного периода требование действует сразу", MFAPolicy{Required: true}, created, true},
		{"внутри льготного периода", MFAPolicy{Required: true, GraceHours: 48}, created.Add(47 * time.Hour), false},
		{"на границе льготного периода", MFAPolicy{Required: true, GraceHours: 48}, created.Add(48 * time.Hour), true},
		{"после льготного периода", MFAPolicy{Required: true, GraceHours: 48}, created.Add(72 * time.Hour), true},
		{"льготный период не отменяет требование навсегда", MFAPolicy{Required: true, GraceHours: 720}, created.Add(721 * time.Hour), true},
	}
	for _, tc := range cases {
		if got := MFAEnforcedFor(tc.policy, created, tc.now); got != tc.want {
			t.Errorf("%s: получено %v, ожидалось %v", tc.name, got, tc.want)
		}
	}
}

// SEC-07 на реальной БД: политику задаёт только системный администратор, а
// сброс второго фактора снимает секрет, коды восстановления и все сессии.
func TestMFAPolicyAndResetAreAdministered(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	handlers := AdminHandlers{DB: db}
	superAdmin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	setting := func(user middleware.AuthUser, key, value string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("POST", "/api/admin/settings",
			strings.NewReader(`{"key":"`+key+`","value":"`+value+`"}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handlers.UpdateSetting(recorder, request, user)
		return recorder
	}

	t.Run("политику задаёт только системный администратор", func(t *testing.T) {
		orgAdmin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleOrgAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
		if got := setting(orgAdmin, settingMFARequired, "true"); got.Code != 403 {
			t.Fatalf("администратор организации не включает 2FA всем, получено %d", got.Code)
		}
		if got := setting(superAdmin, settingMFARequired, "yes"); got.Code != 400 {
			t.Fatalf("значение должно быть true или false, получено %d", got.Code)
		}
		if got := setting(superAdmin, settingMFAGraceHours, "1000"); got.Code != 400 {
			t.Fatalf("льготный период ограничен 720 часами, получено %d", got.Code)
		}
		if got := setting(superAdmin, settingMFARequired, "true"); got.Code != 200 {
			t.Fatalf("системный администратор должен включать 2FA: %d %s", got.Code, got.Body.String())
		}
		if got := setting(superAdmin, settingMFAGraceHours, "48"); got.Code != 200 {
			t.Fatalf("льготный период должен сохраняться: %d %s", got.Code, got.Body.String())
		}
		// Возвращаем исходное состояние, чтобы не влиять на другие тесты.
		t.Cleanup(func() { setting(superAdmin, settingMFARequired, "false") })
	})

	t.Run("сброс снимает второй фактор и закрывает сессии", func(t *testing.T) {
		user, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator,
			EntityType: models.EntityOrganization, ITCompanyID: tenant.company})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE users SET mfa_secret='sealed',mfa_last_counter=42 WHERE id::text=$1`, user.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO mfa_recovery_codes(user_id,code_hash) VALUES($1,$2)`,
			user.ID, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO sessions(token,token_hash,user_id,expires_at)
			VALUES($1,$1,$2,now()+interval '8 hours')`, "mfa-"+user.ID, user.ID); err != nil {
			t.Fatal(err)
		}

		// Сброс — полномочие системного администратора.
		orgAdmin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleOrgAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
		denied := httptest.NewRecorder()
		handlers.ResetMFA(denied, httptest.NewRequest("POST", "/api/admin/users/"+user.ID+"/mfa-reset", nil), orgAdmin, user.ID)
		if denied.Code != 403 {
			t.Fatalf("администратор организации не сбрасывает 2FA, получено %d", denied.Code)
		}

		recorder := httptest.NewRecorder()
		handlers.ResetMFA(recorder, httptest.NewRequest("POST", "/api/admin/users/"+user.ID+"/mfa-reset", nil), superAdmin, user.ID)
		if recorder.Code != 200 {
			t.Fatalf("сброс вернул %d: %s", recorder.Code, recorder.Body.String())
		}

		var secret *string
		var counter int64
		var codes, sessions int
		if err := db.QueryRowContext(ctx, `SELECT mfa_secret,mfa_last_counter FROM users WHERE id::text=$1`, user.ID).
			Scan(&secret, &counter); err != nil {
			t.Fatal(err)
		}
		if secret != nil {
			t.Fatal("после сброса секрет должен быть снят")
		}
		if counter != -1 {
			t.Fatalf("счётчик защиты от повтора должен сбрасываться, получено %d", counter)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mfa_recovery_codes WHERE user_id::text=$1`, user.ID).Scan(&codes); err != nil {
			t.Fatal(err)
		}
		if codes != 0 {
			t.Fatal("коды восстановления привязаны к снятому секрету и должны удаляться")
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id::text=$1`, user.ID).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 0 {
			t.Fatal("после сброса второго фактора сессии должны закрываться")
		}
		// Сброс такой силы обязан оставлять след.
		var audited int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action='mfa_reset' AND entity_id::text=$1`, user.ID).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if audited != 1 {
			t.Fatalf("сброс второго фактора должен попадать в журнал, записей %d", audited)
		}
	})
}

// Защита от повтора кода: использованный счётчик TOTP не принимается второй раз.
func TestTOTPCounterCannotBeReused(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	user, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET mfa_last_counter=100 WHERE id::text=$1`, user.ID); err != nil {
		t.Fatal(err)
	}

	// Тот же счётчик: обновление не проходит, значит код отвергается.
	advance := func(counter int64) bool {
		var updated int64
		err := db.QueryRowContext(ctx,
			`UPDATE users SET mfa_last_counter=$1 WHERE id::text=$2 AND mfa_last_counter<$1 RETURNING mfa_last_counter`,
			counter, user.ID).Scan(&updated)
		return err == nil
	}
	if advance(100) {
		t.Fatal("повторное использование того же кода должно отклоняться")
	}
	if advance(99) {
		t.Fatal("код из прошлого окна должен отклоняться")
	}
	if !advance(101) {
		t.Fatal("следующий код должен приниматься")
	}
}
