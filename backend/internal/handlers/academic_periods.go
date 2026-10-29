package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/middleware"
)

type academicPeriodWriteRequest struct {
	Title    string `json:"title"`
	StartsOn string `json:"starts_on"`
	EndsOn   string `json:"ends_on"`
}

type academicPeriod struct {
	ID            string `json:"id"`
	PartnerID     string `json:"partner_id"`
	Title         string `json:"title"`
	StartsOn      string `json:"starts_on"`
	EndsOn        string `json:"ends_on"`
	DurationWeeks int    `json:"duration_weeks"`
}

func normalizeAcademicPeriod(req *academicPeriodWriteRequest) error {
	title, err := normalizeShortField(req.Title, "название учебного периода", 200, true)
	if err != nil {
		return err
	}
	if len([]rune(title)) < 2 {
		return fmt.Errorf("название учебного периода: не менее 2 символов")
	}
	req.Title = title
	start, startErr := time.Parse("2006-01-02", strings.TrimSpace(req.StartsOn))
	end, endErr := time.Parse("2006-01-02", strings.TrimSpace(req.EndsOn))
	if startErr != nil || endErr != nil {
		return fmt.Errorf("укажите даты начала и окончания учебного периода")
	}
	if end.Before(start) {
		return fmt.Errorf("окончание учебного периода не может быть раньше начала")
	}
	if end.Sub(start) > 366*24*time.Hour {
		return fmt.Errorf("учебный период не может быть длиннее года")
	}
	req.StartsOn, req.EndsOn = start.Format("2006-01-02"), end.Format("2006-01-02")
	return nil
}

func academicPeriodWeeks(start, end time.Time) int {
	days := int(end.Sub(start).Hours()/24) + 1
	return (days + 6) / 7
}

type execQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func requireTeachingPartner(ctx context.Context, q execQueryer, partnerID string) error {
	var kind string
	if err := q.QueryRowContext(ctx, `SELECT partner_kind FROM partners WHERE id::text=$1`, partnerID).Scan(&kind); err != nil {
		return fmt.Errorf("учебное заведение не найдено")
	}
	if kind != "vuz" && kind != "kolledj" {
		return fmt.Errorf("учебные периоды задаются только для вуза или колледжа")
	}
	return nil
}

func insertAcademicPeriod(ctx context.Context, q execQueryer, partnerID, userID string, req academicPeriodWriteRequest) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `INSERT INTO partner_academic_periods(partner_id,title,starts_on,ends_on,created_by)
		VALUES($1,$2,$3,$4,$5) RETURNING id`, partnerID, req.Title, req.StartsOn, req.EndsOn, userID).Scan(&id)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return "", fmt.Errorf("учебный период «%s» уже есть у этого учебного заведения", req.Title)
	}
	return id, err
}

func (h *PartnerStructureHandlers) ListAcademicPeriods(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,partner_id,title,starts_on,ends_on FROM partner_academic_periods
		WHERE partner_id::text=$1 ORDER BY starts_on,title LIMIT 200`, partnerID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать учебные периоды")
		return
	}
	defer rows.Close()
	items := []academicPeriod{}
	for rows.Next() {
		var item academicPeriod
		var start, end time.Time
		if err := rows.Scan(&item.ID, &item.PartnerID, &item.Title, &start, &end); err != nil {
			middleware.WriteError(w, 500, "не удалось прочитать учебные периоды")
			return
		}
		item.StartsOn, item.EndsOn = start.Format("2006-01-02"), end.Format("2006-01-02")
		item.DurationWeeks = academicPeriodWeeks(start, end)
		items = append(items, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "не удалось прочитать учебные периоды")
		return
	}
	middleware.WriteJSON(w, 200, items)
}

func (h *PartnerStructureHandlers) CreateAcademicPeriod(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, partnerID string) {
	if !requireStructureWrite(w, r, h.DB, u, partnerID) {
		return
	}
	var req academicPeriodWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if err := normalizeAcademicPeriod(&req); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if err := requireTeachingPartner(r.Context(), tx, partnerID); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	id, err := insertAcademicPeriod(r.Context(), tx, partnerID, u.ID, req)
	if err != nil {
		middleware.WriteError(w, 409, err.Error())
		return
	}
	if err := logAudit(r.Context(), tx, "academic_period", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id})
}

func (h *PartnerStructureHandlers) academicPeriodPartner(r *http.Request, id string) (string, error) {
	var partnerID string
	err := h.DB.QueryRowContext(r.Context(), `SELECT partner_id FROM partner_academic_periods WHERE id::text=$1`, id).Scan(&partnerID)
	return partnerID, err
}

func (h *PartnerStructureHandlers) UpdateAcademicPeriod(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	partnerID, err := h.academicPeriodPartner(r, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "учебный период не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать учебный период")
		return
	}
	if !requireStructureWrite(w, r, h.DB, u, partnerID) {
		return
	}
	var req academicPeriodWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if err := normalizeAcademicPeriod(&req); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE partner_academic_periods SET title=$2,starts_on=$3,ends_on=$4,updated_at=now()
		WHERE id::text=$1`, id, req.Title, req.StartsOn, req.EndsOn); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			middleware.WriteError(w, 409, fmt.Sprintf("учебный период «%s» уже есть у этого учебного заведения", req.Title))
			return
		}
		middleware.WriteError(w, 500, "не удалось сохранить учебный период")
		return
	}
	if err := logAudit(r.Context(), tx, "academic_period", id, "update", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"id": id})
}

func (h *PartnerStructureHandlers) DeleteAcademicPeriod(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	partnerID, err := h.academicPeriodPartner(r, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "учебный период не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать учебный период")
		return
	}
	if !requireStructureWrite(w, r, h.DB, u, partnerID) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM partner_academic_periods WHERE id::text=$1`, id); err != nil {
		middleware.WriteError(w, 500, "не удалось удалить учебный период")
		return
	}
	if err := logAudit(r.Context(), tx, "academic_period", id, "delete", u.ID, "", nil, nil); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка удаления")
		return
	}
	middleware.WriteJSON(w, 200, map[string]bool{"ok": true})
}
