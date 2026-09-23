package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

// SEC-07: политика второго фактора хранится в настройках, а не выводится из
// окружения. Требование 2FA — управленческое решение, и включать его должен
// системный администратор, а не переменная среды на сервере.
const (
	settingMFARequired   = "mfa_required"
	settingMFAGraceHours = "mfa_grace_period_hours"
)

// MFAPolicy — действующая политика второго фактора.
type MFAPolicy struct {
	Required   bool
	GraceHours int
}

// mfaPolicy читает политику. Отсутствие настройки означает «не требуется»:
// молча включить 2FA для всех и запереть пользователей нельзя.
func mfaPolicy(r *http.Request, db *sql.DB) MFAPolicy {
	policy := MFAPolicy{}
	rows, err := db.QueryContext(r.Context(),
		`SELECT key,value FROM settings WHERE key IN ($1,$2)`, settingMFARequired, settingMFAGraceHours)
	if err != nil {
		return policy
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) != nil {
			return MFAPolicy{}
		}
		switch key {
		case settingMFARequired:
			policy.Required = value == "true"
		case settingMFAGraceHours:
			if hours, convErr := strconv.Atoi(value); convErr == nil && hours >= 0 && hours <= 720 {
				policy.GraceHours = hours
			}
		}
	}
	return policy
}

// MFAEnforcedFor отвечает, обязан ли этот пользователь входить со вторым
// фактором прямо сейчас. Льготный период отсчитывается от создания учётной
// записи: новому сотруднику нужно время, чтобы настроить приложение, но срок
// конечен и не превращается в бессрочное исключение.
func MFAEnforcedFor(policy MFAPolicy, createdAt time.Time, now time.Time) bool {
	if !policy.Required {
		return false
	}
	if policy.GraceHours <= 0 {
		return true
	}
	return !now.Before(createdAt.Add(time.Duration(policy.GraceHours) * time.Hour))
}

// ResetMFA снимает второй фактор с учётной записи. Нужен, когда сотрудник
// потерял устройство и коды восстановления: иначе доступ теряется навсегда.
// Операция администраторская, поэтому она закрывает все сессии пользователя и
// попадает в журнал — сброс 2FA слишком силён, чтобы оставлять его незаметным.
func (h *AdminHandlers) ResetMFA(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser, userID string) {
	if admin.Role != models.RoleSuperAdmin {
		middleware.WriteError(w, http.StatusForbidden, "второй фактор сбрасывает только системный администратор")
		return
	}
	if userID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "укажите пользователя")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()

	var email string
	var enabled bool
	if err := tx.QueryRowContext(r.Context(),
		`SELECT email, mfa_secret IS NOT NULL FROM users WHERE id::text=$1 FOR UPDATE`, userID).
		Scan(&email, &enabled); err != nil {
		middleware.WriteError(w, http.StatusNotFound, "пользователь не найден")
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE users SET mfa_secret=NULL,mfa_pending_secret=NULL,mfa_pending_expires=NULL,
			mfa_last_counter=-1,updated_at=now() WHERE id::text=$1`, userID); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сброса второго фактора")
		return
	}
	// Коды восстановления привязаны к снятому секрету и дальше бесполезны.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM mfa_recovery_codes WHERE user_id::text=$1`, userID); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сброса кодов восстановления")
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE user_id::text=$1`, userID); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка отзыва сессий")
		return
	}
	if logAudit(r.Context(), tx, "user", userID, "mfa_reset", admin.ID,
		"сброс второго фактора администратором", map[string]any{"mfa_enabled": enabled},
		map[string]any{"mfa_enabled": false}) != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка аудита")
		return
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
