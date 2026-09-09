package handlers

import (
	"cybercalc/internal/auth"
	"cybercalc/internal/middleware"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

func (h *AuthHandlers) MFAEnroll(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req struct {
		Password string `json:"password"`
	}
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	allowed, err := auth.AllowAttempt(r.Context(), h.DB, "mfa-enroll:"+u.ID, 8)
	if err != nil || !allowed {
		middleware.WriteError(w, 429, "повторите позже")
		return
	}
	var password string
	var enabled bool
	if h.DB.QueryRowContext(r.Context(), `SELECT password_hash,mfa_secret IS NOT NULL FROM users WHERE id=$1`, u.ID).Scan(&password, &enabled) != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if enabled {
		middleware.WriteError(w, 409, "двухфакторная защита уже включена")
		return
	}
	if ok, _ := auth.VerifyPassword(req.Password, password); !ok {
		middleware.WriteError(w, 400, "неверный пароль")
		return
	}
	secret, err := auth.NewMFASecret()
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	sealed, err := auth.SealMFA(h.MFAKey, secret, u.ID)
	if err != nil {
		middleware.WriteError(w, 503, "на сервере не настроен ключ MFA")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE users SET mfa_pending_secret=$1,mfa_pending_expires=now()+interval '10 minutes' WHERE id=$2 AND mfa_secret IS NULL`, sealed, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"secret": secret, "issuer": "CyberCalculator", "algorithm": "SHA1", "digits": "6", "period": "30"})
}

func (h *AuthHandlers) MFAConfirm(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req struct {
		Code string `json:"code"`
	}
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	allowed, err := auth.AllowAttempt(r.Context(), h.DB, "mfa-confirm:"+u.ID, 8)
	if err != nil || !allowed {
		middleware.WriteError(w, 429, "повторите позже")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	var sealed string
	if tx.QueryRowContext(r.Context(), `SELECT mfa_pending_secret FROM users WHERE id=$1 AND mfa_secret IS NULL AND mfa_pending_expires>now() FOR UPDATE`, u.ID).Scan(&sealed) != nil {
		middleware.WriteError(w, 400, "начните настройку заново")
		return
	}
	secret, err := auth.OpenMFA(h.MFAKey, sealed, u.ID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка MFA")
		return
	}
	counter, valid := auth.VerifyTOTP(secret, req.Code, time.Now())
	if !valid {
		middleware.WriteError(w, 400, "неверный код подтверждения")
		return
	}
	codes := []string{}
	for i := 0; i < 8; i++ {
		code, e := auth.NewToken()
		if e != nil {
			middleware.WriteError(w, 500, "ошибка сервера")
			return
		}
		code = code[:20]
		codes = append(codes, code)
		if _, e := tx.ExecContext(r.Context(), `INSERT INTO mfa_recovery_codes(user_id,code_hash) VALUES($1,$2)`, u.ID, auth.TokenHash(code)); e != nil {
			middleware.WriteError(w, 500, "ошибка сервера")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET mfa_secret=$1,mfa_pending_secret=NULL,mfa_pending_expires=NULL,mfa_last_counter=$2 WHERE id=$3`, sealed, counter, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if logAudit(tx, "user", u.ID, "mfa_enabled", u.ID, "все сессии отозваны", nil, nil) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	auth.DestroySession(w, r, h.DB)
	middleware.WriteJSON(w, 200, map[string]interface{}{"recovery_codes": codes})
}

func (h *AuthHandlers) verifySecondFactor(r *http.Request, id, sealed, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) == 20 {
		var removed string
		return h.DB.QueryRowContext(r.Context(), `DELETE FROM mfa_recovery_codes WHERE user_id=$1 AND code_hash=$2 RETURNING code_hash`, id, auth.TokenHash(code)).Scan(&removed) == nil
	}
	secret, err := auth.OpenMFA(h.MFAKey, sealed, id)
	if err != nil {
		return false
	}
	counter, ok := auth.VerifyTOTP(secret, code, time.Now())
	if !ok {
		return false
	}
	var advanced int64
	err = h.DB.QueryRowContext(r.Context(), `UPDATE users SET mfa_last_counter=$1 WHERE id=$2 AND mfa_last_counter<$1 RETURNING mfa_last_counter`, counter, id).Scan(&advanced)
	return err != sql.ErrNoRows && err == nil
}
