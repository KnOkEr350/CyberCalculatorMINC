package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cybercalc/internal/auth"
	"cybercalc/internal/curators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// DATA-09 / SEC-04: область куратора определяется закреплением на сегодняшнюю
// дату при каждом запросе. Закончившееся закрепление не даёт доступа, даже если
// кэш users.partner_id ещё не догнан воркером.
func TestCuratorScopeFollowsAssignmentDatesPerRequest(t *testing.T) {
	db, _ := integrationDB(t)
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
	newPartner := func() string {
		university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
		if err != nil {
			t.Fatal(err)
		}
		p, err := f.CreatePartner(ctx, company, university)
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	partnerA, partnerB := newPartner(), newPartner()
	curator, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: company.ID})
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions(token,token_hash,user_id,expires_at) VALUES($1,$1,$2,$3)`,
		auth.TokenHash(token), curator.ID, time.Now().Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	scope := func() string {
		t.Helper()
		var seen string
		handler := middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
			if u.PartnerID != nil {
				seen = *u.PartnerID
			}
		})
		request := httptest.NewRequest("GET", "/api/dashboard", nil)
		request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		recorder := httptest.NewRecorder()
		handler(recorder, request)
		if recorder.Code != 200 {
			t.Fatalf("запрос куратора вернул %d: %s", recorder.Code, recorder.Body.String())
		}
		return seen
	}
	staleCache := func(partner string) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `SELECT set_config('curators.sync','on',true)`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET partner_id=NULLIF($2,'')::uuid WHERE id::text=$1`, curator.ID, partner); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	today := curators.Today(now)
	if scope() != "" {
		t.Fatal("без закрепления куратор не видит партнёра")
	}
	// Действующее закрепление даёт область.
	if _, err := curators.Assign(ctx, db, curators.NewInput{CuratorID: curator.ID, PartnerID: partnerA, From: today.AddDate(0, 0, -3),
		Until: &[]time.Time{today.AddDate(0, 0, -1)}[0], AssignedBy: root.ID}, now); err != nil {
		t.Fatal(err)
	}
	// Закрепление закончилось вчера, а кэш всё ещё указывает на партнёра: доступа нет.
	staleCache(partnerA)
	if got := scope(); got != "" {
		t.Fatalf("закончившееся закрепление не должно давать доступ, увидели %q", got)
	}
	// Кэш указывает на чужого партнёра, а закрепление — на своего: верно закрепление.
	if _, err := curators.Assign(ctx, db, curators.NewInput{CuratorID: curator.ID, PartnerID: partnerB, From: today, AssignedBy: root.ID}, now); err != nil {
		t.Fatal(err)
	}
	staleCache(partnerA)
	if got := scope(); got != partnerB {
		t.Fatalf("область определяется закреплением, а не кэшем: %q, ожидался %q", got, partnerB)
	}
}
