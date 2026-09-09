package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"
)

const CookieName = "cybercalc_session"

// NewToken генерирует криптографически случайный токен сессии.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type Session struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

type sessionExecer interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

// InsertSession participates in the login transaction. The caller locks the
// user row, commits authentication and only then sends the bearer cookie.
func InsertSession(ctx context.Context, db sessionExecer, userID string, ttl time.Duration) (Session, error) {
	token, err := NewToken()
	if err != nil {
		return Session{}, err
	}
	expires := time.Now().Add(ttl)
	hash := TokenHash(token)
	_, err = db.ExecContext(ctx, `INSERT INTO sessions (token, token_hash, user_id, expires_at) VALUES ($1, $1, $2, $3)`,
		hash, userID, expires)
	if err != nil {
		return Session{}, err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token<>$2 AND (expires_at<=now() OR token NOT IN (SELECT token FROM sessions WHERE user_id=$1 AND token<>$2 ORDER BY created_at DESC,token LIMIT 4))`, userID, hash); err != nil {
		return Session{}, err
	}
	return Session{Token: token, UserID: userID, ExpiresAt: expires}, nil
}

func SetSessionCookie(w http.ResponseWriter, session Session, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// DestroySession удаляет сессию из БД и стирает cookie.
func DestroySession(w http.ResponseWriter, r *http.Request, db *sql.DB, secure bool) error {
	if c, err := r.Cookie(CookieName); err == nil {
		if _, err := db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = $1 OR (token_hash IS NULL AND token = $2)`, TokenHash(c.Value), c.Value); err != nil {
			return err
		}
	}
	ClearSessionCookie(w, secure)
	return nil
}

func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Secure:   secure,
	})
}

// UserIDFromRequest возвращает user_id по cookie сессии, если она валидна.
func UserIDFromRequest(r *http.Request, db *sql.DB) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || len(c.Value) != 43 {
		return "", false
	}
	var userID string
	var expires time.Time
	err = db.QueryRowContext(r.Context(), `SELECT user_id, expires_at FROM sessions WHERE token_hash = $1 OR (token_hash IS NULL AND token = $2)`, TokenHash(c.Value), c.Value).
		Scan(&userID, &expires)
	if err != nil {
		return "", false
	}
	if time.Now().After(expires) {
		db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = $1 OR token = $2`, TokenHash(c.Value), c.Value)
		return "", false
	}
	return userID, true
}

func TokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
