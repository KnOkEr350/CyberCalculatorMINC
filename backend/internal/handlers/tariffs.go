package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
)

// tariffItem — ставка, действующая на отчётный год, с основанием: по ней
// пользователь видит, из какой редакции Методики посчитана сумма.
type tariffItem struct {
	Code        string       `json:"code"`
	Category    string       `json:"category_code"`
	Label       string       `json:"label"`
	Unit        string       `json:"unit"`
	AmountRub   money.Amount `json:"amount_rub"`
	VersionID   int64        `json:"version_id"`
	ValidFrom   string       `json:"valid_from"`
	ValidUntil  string       `json:"valid_until,omitempty"`
	Source      string       `json:"source_reference"`
	PublishedAt string       `json:"published_at"`
}

// Tariffs отдаёт ставки, действующие на отчётный год. Ставки открыты любому
// вошедшему пользователю: по ним считается сумма каждого мероприятия, и
// сверять её без знания ставки нечем.
func (h *EntryHandlers) Tariffs(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	year := time.Now().In(businessLocation).Year()
	if value := r.URL.Query().Get("report_year"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 2000 || parsed > 2100 {
			middleware.WriteError(w, http.StatusBadRequest, "некорректный год")
			return
		}
		year = parsed
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT DISTINCT ON (v.rate_code)
			v.rate_code,r.category_code,r.label,r.unit,v.amount_rub::text,v.id,
			v.valid_from::text,COALESCE(v.valid_until::text,''),v.source_reference,v.created_at::text
		FROM tariff_versions v JOIN tariff_rates r ON r.code=v.rate_code
		WHERE v.valid_from <= $1::date AND (v.valid_until IS NULL OR v.valid_until >= $1::date)
		ORDER BY v.rate_code,v.valid_from DESC`, tariffs.EffectiveOn(year))
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения тарифов")
		return
	}
	defer rows.Close()
	out := make([]tariffItem, 0, len(tariffs.Definitions()))
	for rows.Next() {
		var item tariffItem
		var amount string
		if err := rows.Scan(&item.Code, &item.Category, &item.Label, &item.Unit, &amount, &item.VersionID,
			&item.ValidFrom, &item.ValidUntil, &item.Source, &item.PublishedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения тарифов")
			return
		}
		parsed, err := money.Parse(amount)
		if err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения тарифов")
			return
		}
		item.AmountRub = parsed
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения тарифов")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"report_year": year, "tariffs": out})
}

type publishTariffRequest struct {
	RateCode        string       `json:"rate_code"`
	AmountRub       money.Amount `json:"amount_rub"`
	ValidFrom       string       `json:"valid_from"`
	SourceReference string       `json:"source_reference"`
}

// PublishTariff вводит новую редакцию ставки. Это решение уровня всей системы:
// оно меняет суммы всех новых мероприятий, поэтому доступно только системному
// администратору, требует основания и попадает в журнал.
func (h *AdminHandlers) PublishTariff(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	if admin.Role != models.RoleSuperAdmin {
		middleware.WriteError(w, http.StatusForbidden, "новую редакцию тарифа вводит только системный администратор")
		return
	}
	var req publishTariffRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	req.SourceReference = strings.TrimSpace(req.SourceReference)
	if req.SourceReference == "" || len([]rune(req.SourceReference)) > 2000 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите официальную редакцию — основание тарифа (до 2000 символов)")
		return
	}
	if req.AmountRub <= 0 {
		middleware.WriteError(w, http.StatusBadRequest, "ставка должна быть положительной")
		return
	}
	from, err := time.Parse("2006-01-02", req.ValidFrom)
	if err != nil || from.Month() != time.January || from.Day() != 1 {
		middleware.WriteError(w, http.StatusBadRequest, "редакция вступает в силу с 1 января: укажите дату вида ГГГГ-01-01")
		return
	}
	known := false
	for _, definition := range tariffs.Definitions() {
		if definition.Code == req.RateCode {
			known = true
			break
		}
	}
	if !known {
		middleware.WriteError(w, http.StatusBadRequest, "неизвестная ставка")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	// Новая редакция обязана начинаться позже действующей: задним числом
	// вставить версию в середину истории нельзя, иначе суммы прошлых лет
	// поменяли бы основание.
	var latest string
	if err := tx.QueryRowContext(r.Context(),
		`SELECT COALESCE(max(valid_from)::text,'') FROM tariff_versions WHERE rate_code=$1`, req.RateCode).Scan(&latest); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения тарифов")
		return
	}
	if latest != "" && req.ValidFrom <= latest {
		middleware.WriteError(w, http.StatusConflict, "новая редакция должна начинаться позже действующей ("+latest+")")
		return
	}
	// Действующая версия закрывается днём накануне: периоды не пересекаются.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE tariff_versions SET valid_until=$2::date - 1 WHERE rate_code=$1 AND valid_until IS NULL`,
		req.RateCode, req.ValidFrom); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка закрытия прежней редакции")
		return
	}
	var id int64
	if err := tx.QueryRowContext(r.Context(),
		`INSERT INTO tariff_versions(rate_code,amount_rub,valid_from,source_reference,created_by)
		 VALUES($1,$2,$3::date,$4,$5) RETURNING id`,
		req.RateCode, req.AmountRub, req.ValidFrom, req.SourceReference, admin.ID).Scan(&id); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения тарифа")
		return
	}
	if logAudit(r.Context(), tx, "tariff", "", "tariff_publish", admin.ID, req.SourceReference, nil, req) != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}
