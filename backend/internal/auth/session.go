package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
func CreateSession(w http.ResponseWriter, db *sql.DB, userID string, ttl time.Duration) error {
	token, err := NewToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(ttl)
	_, err = db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, userID, expires)
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
		// Secure: true — включить, когда сервис работает по HTTPS (напр. за reverse-proxy)
	})
	return nil
}

// DestroySession удаляет сессию из БД и стирает cookie.
func DestroySession(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if c, err := r.Cookie(CookieName); err == nil {
		db.Exec(`DELETE FROM sessions WHERE token = $1`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
}

// UserIDFromRequest возвращает user_id по cookie сессии, если она валидна.
func UserIDFromRequest(r *http.Request, db *sql.DB) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return "", false
	}
	var userID string
	var expires time.Time
	err = db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = $1`, c.Value).
		Scan(&userID, &expires)
	if err != nil {
		return "", false
	}
	if time.Now().After(expires) {
		db.Exec(`DELETE FROM sessions WHERE token = $1`, c.Value)
		return "", false
	}
	return userID, true
}
