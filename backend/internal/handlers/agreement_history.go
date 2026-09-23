package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"cybercalc/internal/middleware"
)

// AgreementHistoryHandlers — история изменений соглашения и его куратор (DATA-02).
type AgreementHistoryHandlers struct{ DB *sql.DB }

type setAgreementCuratorRequest struct {
	CuratorID string `json:"curator_id"`
	Reason    string `json:"reason,omitempty"`
}

type agreementRevision struct {
	Revision  int             `json:"revision"`
	ChangedBy string          `json:"changed_by,omitempty"`
	ChangedAt string          `json:"changed_at"`
	Snapshot  json.RawMessage `json:"snapshot"`
}

// agreementInTenant проверяет, что соглашение принадлежит арендатору
// пользователя; чужое и несуществующее неразличимы.
func (h *AgreementHistoryHandlers) agreementInTenant(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) bool {
	tenant := itCompanyScope(u)
	var exists bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM agreements WHERE id::text=$1 AND ($2='' OR it_company_id::text=$2))`, id, tenant).Scan(&exists); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка проверки соглашения")
		return false
	}
	if !exists {
		middleware.WriteError(w, http.StatusNotFound, "соглашение не найдено")
		return false
	}
	return true
}

// History отдаёт редакции соглашения от первой к последней.
func (h *AgreementHistoryHandlers) History(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if u.EntityType != "organization" || !canReadTenantData(u) {
		middleware.WriteError(w, http.StatusForbidden, "история соглашения доступна ИТ-организации")
		return
	}
	if !h.agreementInTenant(w, r, u, id) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT revision,COALESCE(changed_by::text,''),changed_at::text,snapshot
		FROM agreement_revisions WHERE agreement_id::text=$1 ORDER BY revision`, id)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории")
		return
	}
	defer rows.Close()
	out := []agreementRevision{}
	for rows.Next() {
		var item agreementRevision
		var snapshot []byte
		if err := rows.Scan(&item.Revision, &item.ChangedBy, &item.ChangedAt, &snapshot); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории")
			return
		}
		item.Snapshot = snapshot
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

// SetCurator закрепляет куратора за соглашением (пустой идентификатор снимает).
// Назначать вправе те, кто закрепляет кураторов за партнёрами; куратором может
// быть только куратор той же ИТ-компании. Изменение попадает в историю.
func (h *AgreementHistoryHandlers) SetCurator(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canManageCuratorAssignments(u) {
		middleware.WriteError(w, http.StatusForbidden, "куратора соглашения назначает администрация")
		return
	}
	var req setAgreementCuratorRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if !h.agreementInTenant(w, r, u, id) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var previous sql.NullString
	if err := tx.QueryRowContext(r.Context(), `SELECT curator_id::text FROM agreements WHERE id::text=$1 FOR UPDATE`, id).Scan(&previous); err != nil {
		middleware.WriteError(w, http.StatusNotFound, "соглашение не найдено")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE agreements SET curator_id=NULLIF($2,'')::uuid,updated_by=$3::uuid,updated_at=now() WHERE id::text=$1`,
		id, strings.TrimSpace(req.CuratorID), u.ID); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "куратором может быть только куратор организации той же ИТ-компании")
		return
	}
	if logAudit(r.Context(), tx, "agreement", id, "curator", u.ID, strings.TrimSpace(req.Reason), map[string]interface{}{"curator_id": previous.String}, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
