package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/curators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// DATA-09 на реальной БД: закрепление куратора за партнёром — период, один
// партнёр в день, граница арендатора, отзыв с причиной и неизменяемая история.
func TestCuratorAssignments(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())

	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	foreignCompany, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	newPartner := func(c testfixtures.ITCompany) string {
		t.Helper()
		university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
		if err != nil {
			t.Fatal(err)
		}
		partner, err := f.CreatePartner(ctx, c, university)
		if err != nil {
			t.Fatal(err)
		}
		return partner.ID
	}
	partnerA, partnerB, foreignPartner := newPartner(company), newPartner(company), newPartner(foreignCompany)
	newCurator := func(c testfixtures.ITCompany) string {
		t.Helper()
		user, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: c.ID})
		if err != nil {
			t.Fatal(err)
		}
		return user.ID
	}
	curator := newCurator(company)
	now := time.Now()
	today := curators.Today(now)
	day := func(offset int) time.Time { return today.AddDate(0, 0, offset) }
	cachedPartner := func(id string) string {
		t.Helper()
		var partner string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(partner_id::text,'') FROM users WHERE id::text=$1`, id).Scan(&partner); err != nil {
			t.Fatal(err)
		}
		return partner
	}
	assign := func(user, partner string, from time.Time, until *time.Time) (curators.Assignment, error) {
		return curators.Assign(ctx, db, curators.NewInput{CuratorID: user, PartnerID: partner, From: from, Until: until, AssignedBy: admin.ID, Reason: "тест"}, now)
	}

	t.Run("будущее закрепление ещё не действует, текущее действует", func(t *testing.T) {
		future, err := assign(curator, partnerA, day(10), nil)
		if err != nil {
			t.Fatal(err)
		}
		if future.Active || cachedPartner(curator) != "" {
			t.Fatalf("закрепление со следующей недели не должно действовать сегодня: %+v, кэш %q", future, cachedPartner(curator))
		}
		if _, err := curators.Revoke(ctx, db, future.ID, admin.ID, "перенос", now); err != nil {
			t.Fatal(err)
		}
		current, err := assign(curator, partnerA, day(-5), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !current.Active || cachedPartner(curator) != partnerA {
			t.Fatalf("действующее закрепление должно попасть в кэш профиля: %+v, кэш %q", current, cachedPartner(curator))
		}
		var derived string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(current_partner_of($1::uuid)::text,'')`, curator).Scan(&derived); err != nil || derived != partnerA {
			t.Fatalf("current_partner_of = %q, %v", derived, err)
		}
	})

	t.Run("в один день куратор закреплён не больше чем за одним партнёром", func(t *testing.T) {
		if _, err := assign(curator, partnerB, day(0), nil); err != curators.ErrOverlap {
			t.Fatalf("пересечение должно отвергаться: %v", err)
		}
		if _, err := assign(curator, partnerB, day(-2), &[]time.Time{day(3)}[0]); err != curators.ErrOverlap {
			t.Fatalf("пересечение по части периода должно отвергаться: %v", err)
		}
	})

	t.Run("окончание сокращает период, продлить нельзя", func(t *testing.T) {
		list, err := curators.List(ctx, db, curators.Filter{CuratorID: curator, ActiveOnly: true}, now)
		if err != nil || len(list) != 1 {
			t.Fatalf("действующее закрепление одно: %v %v", list, err)
		}
		ended, err := curators.End(ctx, db, list[0].ID, day(2), now)
		if err != nil || ended.ValidUntil != day(2).Format("2006-01-02") {
			t.Fatalf("окончание: %+v %v", ended, err)
		}
		if _, err := curators.End(ctx, db, list[0].ID, day(30), now); err != curators.ErrNothingToChange {
			t.Fatalf("продление окончанием должно отвергаться: %v", err)
		}
		if _, err := curators.End(ctx, db, list[0].ID, day(-30), now); err != curators.ErrInvalidPeriod {
			t.Fatalf("окончание раньше начала: %v", err)
		}
		// Следующий партнёр — встык, без пересечения.
		if _, err := assign(curator, partnerB, day(3), nil); err != nil {
			t.Fatalf("закрепление встык должно приниматься: %v", err)
		}
	})

	t.Run("закрепить можно только куратора своей ИТ-компании", func(t *testing.T) {
		other := newCurator(company)
		if _, err := assign(other, foreignPartner, day(0), nil); err != curators.ErrForeignTenant {
			t.Fatalf("чужой партнёр: %v", err)
		}
		hr, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleHRSpecialist, EntityType: models.EntityOrganization, ITCompanyID: company.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := assign(hr.ID, partnerA, day(0), nil); err != curators.ErrNotCurator {
			t.Fatalf("не куратор: %v", err)
		}
	})

	t.Run("отзыв требует причины, отозванное не действует и не мешает", func(t *testing.T) {
		user := newCurator(company)
		a, err := assign(user, partnerA, day(-1), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := curators.Revoke(ctx, db, a.ID, admin.ID, "  ", now); err != curators.ErrReasonRequired {
			t.Fatalf("без причины отзыв недопустим: %v", err)
		}
		revoked, err := curators.Revoke(ctx, db, a.ID, admin.ID, "закреплён по ошибке", now)
		if err != nil || revoked.Active || revoked.RevokeNote != "закреплён по ошибке" {
			t.Fatalf("отзыв: %+v %v", revoked, err)
		}
		if cachedPartner(user) != "" {
			t.Fatal("после отзыва куратор не должен видеть партнёра")
		}
		if _, err := curators.Revoke(ctx, db, a.ID, admin.ID, "ещё раз", now); err != curators.ErrAlreadyRevoked {
			t.Fatalf("повторный отзыв: %v", err)
		}
		if _, err := assign(user, partnerB, day(-1), nil); err != nil {
			t.Fatalf("отозванное закрепление не должно блокировать новое: %v", err)
		}
	})

	t.Run("история не переписывается и не удаляется", func(t *testing.T) {
		list, err := curators.List(ctx, db, curators.Filter{CuratorID: curator}, now)
		if err != nil || len(list) == 0 {
			t.Fatal(list, err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM user_partner_assignments WHERE id::text=$1`, list[0].ID); err == nil {
			t.Fatal("удалять закрепления нельзя")
		}
		if _, err := db.ExecContext(ctx, `UPDATE user_partner_assignments SET partner_id=$2 WHERE id::text=$1`, list[0].ID, partnerB); err == nil {
			t.Fatal("менять партнёра в записанном закреплении нельзя")
		}
		if _, err := db.ExecContext(ctx, `UPDATE user_partner_assignments SET valid_from=valid_from-10 WHERE id::text=$1`, list[0].ID); err == nil {
			t.Fatal("менять начало периода нельзя")
		}
	})

	t.Run("прямая запись партнёра в профиль превращается в закрепление", func(t *testing.T) {
		user := newCurator(company)
		if _, err := db.ExecContext(ctx, `UPDATE users SET partner_id=$2 WHERE id::text=$1`, user, partnerA); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE users SET partner_id=$2 WHERE id::text=$1`, user, partnerB); err != nil {
			t.Fatal(err)
		}
		if cachedPartner(user) != partnerB {
			t.Fatalf("итоговый партнёр %q, ожидался %q", cachedPartner(user), partnerB)
		}
		all, err := curators.List(ctx, db, curators.Filter{CuratorID: user}, now)
		if err != nil || len(all) != 2 {
			t.Fatalf("история должна сохранить оба закрепления: %v %v", all, err)
		}
		active := 0
		for _, a := range all {
			if a.Active {
				active++
				if a.PartnerID != partnerB {
					t.Fatalf("действует не то закрепление: %+v", a)
				}
			} else if a.RevokedAt == "" || !strings.Contains(a.RevokeNote, "заменено") {
				t.Fatalf("прежнее закрепление должно быть отозвано с причиной: %+v", a)
			}
		}
		if active != 1 {
			t.Fatalf("действующих закреплений %d, ожидалось 1", active)
		}
		if _, err := db.ExecContext(ctx, `UPDATE users SET partner_id=NULL WHERE id::text=$1`, user); err != nil {
			t.Fatal(err)
		}
		if list, _ := curators.List(ctx, db, curators.Filter{CuratorID: user, ActiveOnly: true}, now); len(list) != 0 {
			t.Fatalf("снятие партнёра должно снимать закрепление: %v", list)
		}
	})

	t.Run("пересчёт догоняет даты и идемпотентен", func(t *testing.T) {
		user := newCurator(company)
		if _, err := assign(user, partnerA, day(-1), &[]time.Time{day(1)}[0]); err != nil {
			t.Fatal(err)
		}
		if cachedPartner(user) != partnerA {
			t.Fatal("действующее закрепление должно быть в кэше")
		}
		// Через три дня закрепление кончилось само, без записи в БД.
		if _, err := curators.Sync(ctx, db, now.AddDate(0, 0, 3)); err != nil {
			t.Fatal(err)
		}
		if cachedPartner(user) != "" {
			t.Fatal("после окончания периода кэш должен очиститься")
		}
		again, err := curators.Sync(ctx, db, now.AddDate(0, 0, 3))
		if err != nil {
			t.Fatal(err)
		}
		if again != 0 {
			t.Fatalf("повторный пересчёт изменил %d профилей", again)
		}
		list, _ := curators.List(ctx, db, curators.Filter{CuratorID: user}, now)
		if len(list) != 1 || list[0].RevokedAt != "" {
			t.Fatalf("пересчёт не должен трогать закрепления: %v", list)
		}
		// Возвращаемся в настоящее: профиль снова совпадает с закреплением.
		if _, err := curators.Sync(ctx, db, now); err != nil {
			t.Fatal(err)
		}
		if cachedPartner(user) != partnerA {
			t.Fatal("в текущую дату закрепление действует")
		}
	})

	t.Run("API: права и граница арендатора", func(t *testing.T) {
		handlers := CuratorAssignmentHandlers{DB: db}
		manager := middleware.AuthUser{ID: admin.ID, Role: models.RoleOrgAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
		foreignManager := middleware.AuthUser{ID: admin.ID, Role: models.RoleOrgAdmin, EntityType: models.EntityOrganization, ITCompanyID: &foreignCompany.ID}
		hrUser := middleware.AuthUser{ID: admin.ID, Role: models.RoleHRSpecialist, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
		user := newCurator(company)
		create := func(u middleware.AuthUser, body string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/curator-assignments", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			handlers.Create(rec, req, u)
			return rec
		}
		body := `{"curator_id":"` + user + `","partner_id":"` + partnerA + `","reason":"назначение"}`
		if rec := create(hrUser, body); rec.Code != 403 {
			t.Fatalf("кадровая служба не закрепляет кураторов: %d", rec.Code)
		}
		if rec := create(foreignManager, body); rec.Code != 404 {
			t.Fatalf("чужой арендатор не должен видеть куратора: %d %s", rec.Code, rec.Body.String())
		}
		rec := create(manager, body)
		if rec.Code != 201 {
			t.Fatalf("закрепление своим управляющим: %d %s", rec.Code, rec.Body.String())
		}
		var created curators.Assignment
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if rec := create(manager, body); rec.Code != 409 {
			t.Fatalf("повтор закрепления: %d", rec.Code)
		}
		revoke := func(u middleware.AuthUser, reason string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/curator-assignments/"+created.ID+"/revoke", strings.NewReader(`{"reason":"`+reason+`"}`))
			req.Header.Set("Content-Type", "application/json")
			handlers.Revoke(rec, req, u, created.ID)
			return rec
		}
		if rec := revoke(foreignManager, "чужой"); rec.Code != 404 {
			t.Fatalf("чужой арендатор не отзывает: %d", rec.Code)
		}
		if rec := revoke(manager, ""); rec.Code != 400 {
			t.Fatalf("отзыв без причины: %d", rec.Code)
		}
		if rec := revoke(manager, "ошибка"); rec.Code != 200 {
			t.Fatalf("отзыв: %d %s", rec.Code, rec.Body.String())
		}
		// Куратор видит только свои закрепления, посторонние роли — ничего.
		listAs := func(u middleware.AuthUser) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			handlers.List(rec, httptest.NewRequest("GET", "/api/curator-assignments", nil), u)
			return rec
		}
		self := middleware.AuthUser{ID: user, Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
		if rec := listAs(self); rec.Code != 200 || !strings.Contains(rec.Body.String(), user) || strings.Contains(rec.Body.String(), curator) {
			t.Fatalf("куратор должен видеть только своё: %d %s", rec.Code, rec.Body.String())
		}
		if rec := listAs(hrUser); rec.Code != 403 {
			t.Fatalf("кадровая служба не читает закрепления: %d", rec.Code)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='curator_assignment' AND entity_id=$1`, created.ID).Scan(&count); err != nil || count != 2 {
			t.Fatalf("в аудите должны быть закрепление и отзыв: %d %v", count, err)
		}
	})
}
