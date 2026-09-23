package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/auth"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	platformaudit "cybercalc/internal/platform/audit"
	"cybercalc/internal/testfixtures"
)

// AUDIT-01 на реальной БД: журнал только дополняется. Раньше неизменяемость
// держалась договорённостью — очистка по сроку хранения была единственным
// местом, где записи удалялись, но сама таблица принимала любой UPDATE и
// DELETE. Теперь запрет живёт в БД.
func TestAuditLogIsAppendOnly(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()

	if err := platformaudit.Write(ctx, db, platformaudit.Event{
		Actor:  platformaudit.Actor{Type: platformaudit.ActorSystem},
		Action: "update",
		Entity: platformaudit.Entity{Type: "entry"},
		Before: map[string]string{"amount_rub": "100000.00"},
		After:  map[string]string{"amount_rub": "150000.00"},
	}); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM audit_log ORDER BY created_at DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	// Мутация записи — попытка переписать историю.
	if _, err := db.ExecContext(ctx, `UPDATE audit_log SET new_value='{"amount_rub":"0.00"}' WHERE id::text=$1`, id); err == nil {
		t.Fatal("журнал аудита не должен принимать UPDATE")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("ожидался отказ append-only, получено: %v", err)
	}
	// Точечное удаление мимо очистки по сроку хранения.
	if _, err := db.ExecContext(ctx, `DELETE FROM audit_log WHERE id::text=$1`, id); err == nil {
		t.Fatal("журнал аудита не должен принимать точечный DELETE")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("ожидался отказ append-only, получено: %v", err)
	}

	// Запись пережила обе попытки в исходном виде.
	var oldValue, newValue []byte
	if err := db.QueryRowContext(ctx, `SELECT old_value,new_value FROM audit_log WHERE id::text=$1`, id).Scan(&oldValue, &newValue); err != nil {
		t.Fatal(err)
	}
	var before, after map[string]string
	if json.Unmarshal(oldValue, &before) != nil || json.Unmarshal(newValue, &after) != nil {
		t.Fatalf("снимки до и после должны быть читаемым JSON: %s / %s", oldValue, newValue)
	}
	if before["amount_rub"] != "100000.00" || after["amount_rub"] != "150000.00" {
		t.Fatalf("мутация изменила запись: было %v, стало %v", before, after)
	}
}

// Очистка по сроку хранения остаётся единственным разрешённым удалением:
// иначе запрет превратил бы журнал в вечно растущую таблицу.
func TestAuditRetentionStillPurgesExpiredRecords(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO audit_log(actor_type,action,entity_type,request_id,created_at)
		VALUES('system','delete','entry',$1,now()-interval '400 days')`, "test:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	var fresh string
	if err := db.QueryRowContext(ctx, `INSERT INTO audit_log(actor_type,action,entity_type,request_id)
		VALUES('system','create','entry',$1) RETURNING id::text`, "test-fresh:"+t.Name()).Scan(&fresh); err != nil {
		t.Fatal(err)
	}

	var purged int64
	if err := db.QueryRowContext(ctx, `SELECT purge_expired_audit()`).Scan(&purged); err != nil {
		t.Fatalf("очистка по сроку хранения должна работать: %v", err)
	}
	if purged < 1 {
		t.Fatal("просроченная запись должна удаляться очисткой")
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE id::text=$1`, fresh).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatal("очистка не должна трогать записи в пределах срока хранения")
	}

	// Флаг очистки живёт только внутри её транзакции: следующее удаление
	// снова запрещено.
	if _, err := db.ExecContext(ctx, `DELETE FROM audit_log WHERE id::text=$1`, fresh); err == nil {
		t.Fatal("после очистки журнал снова должен быть защищён")
	}
}

// SEC-08 на реальной БД: изменение роли, арендатора или отключение учётной
// записи немедленно закрывает доступ — обработчик правки профиля отзывает все
// сессии пользователя той же транзакцией. Проверяется через сам обработчик:
// иначе тест подтверждал бы только SQL, написанный в тесте.
func TestSessionsAreRevokedOnProfileChange(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	handlers := AdminHandlers{DB: db}
	admin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"смена роли", `{"role":"auditor_viewer"}`},
		{"закрепление за другой ОО", `{"role":"curator","partner_id":"` + tenant.partner + `"}`},
		{"деактивация", `{"is_active":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator,
				EntityType: models.EntityOrganization, ITCompanyID: tenant.company})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.AssignUserToCompany(ctx, user.ID, tenant.company); err != nil {
				t.Fatal(err)
			}
			token := "hash-" + user.ID
			if _, err := db.ExecContext(ctx, `INSERT INTO sessions(token,token_hash,user_id,expires_at)
				VALUES($1,$1,$2,now()+interval '8 hours')`, token, user.ID); err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("PATCH", "/api/admin/users/"+user.ID, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			handlers.UpdateUser(recorder, request, admin, user.ID)
			if recorder.Code != 200 {
				t.Fatalf("правка профиля вернула %d: %s", recorder.Code, recorder.Body.String())
			}

			var live int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id::text=$1`, user.ID).Scan(&live); err != nil {
				t.Fatal(err)
			}
			if live != 0 {
				t.Fatalf("после изменения профиля осталось живых сессий: %d", live)
			}
		})
	}
}

// Абсолютный срок жизни сессии соблюдается самой проверкой запроса, а не
// только фоновой очисткой: истёкшая cookie не пускает в систему и в тот же
// момент удаляется.
func TestExpiredSessionIsNotAccepted(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	user, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}

	issue := func(ttl time.Duration) string {
		t.Helper()
		token, err := auth.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO sessions(token,token_hash,user_id,expires_at) VALUES($1,$1,$2,$3)`,
			auth.TokenHash(token), user.ID, time.Now().Add(ttl)); err != nil {
			t.Fatal(err)
		}
		return token
	}
	request := func(token string) *http.Request {
		r := httptest.NewRequest("GET", "/api/dashboard", nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		return r
	}

	live := issue(8 * time.Hour)
	if id, ok := auth.UserIDFromRequest(request(live), db); !ok || id != user.ID {
		t.Fatalf("действующая сессия должна приниматься: id=%q ok=%v", id, ok)
	}

	expired := issue(-time.Minute)
	if _, ok := auth.UserIDFromRequest(request(expired), db); ok {
		t.Fatal("истёкшая сессия не должна приниматься")
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE token_hash=$1`, auth.TokenHash(expired)).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("истёкшая сессия должна удаляться при обращении, а не ждать фоновой очистки")
	}
	// Отказ по сроку не задевает другие сессии того же пользователя.
	if _, ok := auth.UserIDFromRequest(request(live), db); !ok {
		t.Fatal("действующая сессия не должна пострадать")
	}
}
