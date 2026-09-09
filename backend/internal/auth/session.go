package auth

import (
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

// CreateSession создаёт запись сессии в БД и выставляет cookie.
func CreateSession(w http.ResponseWriter, db *sql.DB, userID string, ttl time.Duration, secure ...bool) error {
	token, err := NewToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(ttl)
	hash := TokenHash(token)
	_, err = db.Exec(`INSERT INTO sessions (token, token_hash, user_id, expires_at) VALUES ($1, $1, $2, $3)`,
		hash, userID, expires)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   len(secure) > 0 && secure[0],
	})
	return nil
}

// DestroySession удаляет сессию из БД и стирает cookie.
func DestroySession(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if c, err := r.Cookie(CookieName); err == nil {
		db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash = $1 OR (token_hash IS NULL AND token = $2)`, TokenHash(c.Value), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Secure:   r.TLS != nil || r.URL.Scheme == "https",
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
