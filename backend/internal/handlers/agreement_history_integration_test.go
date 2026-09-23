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

// DATA-02 на реальной БД: каждое изменение соглашения, его сторон, ответственных
// и видов оставляет снимок; история только дополняется; куратор соглашения —
// куратор той же ИТ-компании.
func TestAgreementHistoryAndCurator(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own, foreign := newTenant(ctx, t, f), newTenant(ctx, t, f)
	handlers := AgreementHistoryHandlers{DB: db}

	revisions := func() []agreementRevision {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT revision,COALESCE(changed_by::text,''),changed_at::text,snapshot FROM agreement_revisions WHERE agreement_id::text=$1 ORDER BY revision`, own.agreement)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []agreementRevision
		for rows.Next() {
			var r agreementRevision
			var snapshot []byte
			if err := rows.Scan(&r.Revision, &r.ChangedBy, &r.ChangedAt, &snapshot); err != nil {
				t.Fatal(err)
			}
			r.Snapshot = snapshot
			out = append(out, r)
		}
		return out
	}
	snapshotOf := func(r agreementRevision) map[string]interface{} {
		var m map[string]interface{}
		if err := json.Unmarshal(r.Snapshot, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	t.Run("создание оставляет первую редакцию с составом", func(t *testing.T) {
		got := revisions()
		if len(got) != 1 || got[0].Revision != 1 {
			t.Fatalf("после создания одна редакция: %+v", got)
		}
		snap := snapshotOf(got[0])
		if partners, _ := snap["partner_ids"].([]interface{}); len(partners) != 1 || partners[0] != own.partner {
			t.Fatalf("в снимке должны быть стороны: %+v", snap["partner_ids"])
		}
		if _, leaked := snap["updated_at"]; leaked {
			t.Fatal("служебные поля в снимок не входят")
		}
	})

	t.Run("изменение, повтор без изменений и правка состава", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE agreements SET notes='Первое примечание',updated_by=$2 WHERE id::text=$1`, own.agreement, own.admin); err != nil {
			t.Fatal(err)
		}
		got := revisions()
		if len(got) != 2 || snapshotOf(got[1])["notes"] != "Первое примечание" || got[1].ChangedBy != own.admin {
			t.Fatalf("вторая редакция несёт изменение и автора: %+v", got)
		}
		// Запись без изменений не создаёт редакции.
		if _, err := db.ExecContext(ctx, `UPDATE agreements SET notes='Первое примечание' WHERE id::text=$1`, own.agreement); err != nil {
			t.Fatal(err)
		}
		if len(revisions()) != 2 {
			t.Fatal("одинаковое состояние не должно давать новую редакцию")
		}
		// Правка состава: ответственное лицо и вид мероприятия.
		if _, err := db.ExecContext(ctx, `INSERT INTO agreement_responsible_people(agreement_id,party,full_name) VALUES($1,'cyberprotect','Иванов Иван')`, own.agreement); err != nil {
			t.Fatal(err)
		}
		got = revisions()
		people, _ := snapshotOf(got[len(got)-1])["responsible_people"].([]interface{})
		if len(got) != 3 || len(people) != 3 {
			t.Fatalf("добавление ответственного — новая редакция: %d %+v", len(got), people)
		}
	})

	t.Run("несколько правок в одной транзакции дают одну редакцию", func(t *testing.T) {
		before := len(revisions())
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, note := range []string{"а", "б", "в"} {
			if _, err := tx.ExecContext(ctx, `UPDATE agreements SET notes=$2 WHERE id::text=$1`, own.agreement, note); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		got := revisions()
		if len(got) != before+1 || snapshotOf(got[len(got)-1])["notes"] != "в" {
			t.Fatalf("одна редакция с финальным состоянием: было %d, стало %d", before, len(got))
		}
	})

	t.Run("история только дополняется", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE agreement_revisions SET snapshot='{}' WHERE agreement_id::text=$1`, own.agreement); err == nil {
			t.Fatal("редакцию менять нельзя")
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM agreement_revisions WHERE agreement_id::text=$1`, own.agreement); err == nil {
			t.Fatal("редакцию удалять нельзя")
		}
	})

	t.Run("API истории и граница арендатора", func(t *testing.T) {
		admin := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
		foreignAdmin := middleware.AuthUser{ID: foreign.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &foreign.company}
		edu := middleware.AuthUser{ID: own.admin, Role: models.RoleCurator, EntityType: models.EntityEduInst, PartnerID: &own.partner}
		get := func(u middleware.AuthUser) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			handlers.History(rec, httptest.NewRequest("GET", "/api/agreements/"+own.agreement+"/history", nil), u, own.agreement)
			return rec
		}
		rec := get(admin)
		var items []agreementRevision
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &items) != nil || len(items) < 4 || items[0].Revision != 1 {
			t.Fatalf("история: %d %s", rec.Code, rec.Body.String())
		}
		if get(foreignAdmin).Code != 404 {
			t.Fatal("чужой арендатор не видит историю")
		}
		if get(edu).Code != 403 {
			t.Fatal("образовательная организация историю не читает")
		}
	})

	t.Run("куратор соглашения", func(t *testing.T) {
		curator, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: own.company})
		if err != nil {
			t.Fatal(err)
		}
		foreignCurator, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: foreign.company})
		if err != nil {
			t.Fatal(err)
		}
		hr := middleware.AuthUser{ID: own.admin, Role: models.RoleHRSpecialist, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
		admin := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
		put := func(u middleware.AuthUser, curatorID string) int {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("PUT", "/x", strings.NewReader(`{"curator_id":"`+curatorID+`","reason":"назначение"}`))
			req.Header.Set("Content-Type", "application/json")
			handlers.SetCurator(rec, req, u, own.agreement)
			return rec.Code
		}
		if put(hr, curator.ID) != 403 {
			t.Fatal("кадровая служба куратора не назначает")
		}
		if put(admin, foreignCurator.ID) != 400 {
			t.Fatal("куратор другой ИТ-компании недопустим")
		}
		if put(admin, own.admin) != 400 {
			t.Fatal("не куратор недопустим")
		}
		if put(admin, curator.ID) != 200 {
			t.Fatal("куратор той же компании назначается")
		}
		got := revisions()
		if snapshotOf(got[len(got)-1])["curator_id"] != curator.ID {
			t.Fatalf("назначение куратора попадает в историю: %v", snapshotOf(got[len(got)-1])["curator_id"])
		}
		if put(admin, "") != 200 {
			t.Fatal("куратора можно снять")
		}
		got = revisions()
		if snapshotOf(got[len(got)-1])["curator_id"] != nil {
			t.Fatal("снятие куратора попадает в историю")
		}
	})
}
