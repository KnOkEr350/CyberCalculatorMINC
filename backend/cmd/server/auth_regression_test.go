package main

import (
	"context"
	"cybercalc/internal/auth"
	"cybercalc/internal/handlers"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func checkAtomicAuthentication(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hash, err := auth.HashPassword("RegressionPassword1!")
	if err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("race%d@workspace.test", time.Now().UnixNano())
	var id string
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name,role,entity_type) VALUES($1,$2,'Регрессия входа','admin','organization') RETURNING id`, email, hash).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(fmt.Sprintf(`{"email":%q,"password":"RegressionPassword1!"}`, email))).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() { defer close(done); (&handlers.AuthHandlers{DB: db}).Login(w, r) }()
	defer func() { _ = tx.Rollback(); <-done }()
	// Wait for the actual database lock, not a guessed KDF duration.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT password_hash,mfa_secret,is_active FROM users WHERE id=%')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("login did not reach the account lock")
		case <-ticker.C:
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_active=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	<-done
	if w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("stale login created a session: %d", w.Code)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale session persisted", count, err)
	}

	// Identical transaction timestamps must never evict the newest token.
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var newest auth.Session
	for i := 0; i < 8; i++ {
		newest, err = auth.InsertSession(ctx, tx, id, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, id).Scan(&count); err != nil || count != 5 {
		t.Fatal("session cap failed", count, err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE token_hash=$1`, auth.TokenHash(newest.Token)).Scan(&count); err != nil || count != 1 {
		t.Fatal("newest session evicted", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("rolled back session persisted", count, err)
	}
}
