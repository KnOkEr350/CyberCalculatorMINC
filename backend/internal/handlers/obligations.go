package handlers

import (
	"cybercalc/internal/middleware"
	"net/http"
	"strconv"
)

type obligationStatus struct {
	Teachers bool     `json:"teachers"`
	Programs bool     `json:"ood_rpd"`
	TopIT    bool     `json:"top_it"`
	Required bool     `json:"required"`
	Missing  []string `json:"missing"`
}

func obligations(kind string, teachers, programs, top bool) obligationStatus {
	s := obligationStatus{Teachers: teachers, Programs: programs, TopIT: top, Required: kind == "vuz" && !top, Missing: []string{}}
	if s.Required {
		if !teachers {
			s.Missing = append(s.Missing, "teachers")
		}
		if !programs {
			s.Missing = append(s.Missing, "ood_rpd")
		}
	}
	return s
}
func (h *EntryHandlers) Obligations(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	q := r.URL.Query()
	partner := q.Get("partner_id")
	agreement := q.Get("agreement_id")
	year, err := strconv.Atoi(q.Get("report_year"))
	period := q.Get("period_type")
	if err != nil || year < 2000 || year > 2100 || (period != "plan" && period != "fact") || partner == "" {
		middleware.WriteError(w, 400, "укажите партнёра, год и план/факт")
		return
	}
	if !requirePartner(w, u, partner) {
		return
	}
	var kind string
	if h.DB.QueryRowContext(r.Context(), `SELECT partner_kind FROM partners WHERE id::text=$1`, partner).Scan(&kind) != nil {
		middleware.WriteError(w, 404, "партнёр не найден")
		return
	}
	var teacher, program, top bool
	err = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(bool_or(category_code='teachers'),false), COALESCE(bool_or(category_code='ood_rpd'),false), COALESCE(bool_or(category_code='top_it'),false) FROM entries WHERE partner_id::text=$1 AND report_year=$2 AND period_type=$3 AND ($4='' OR agreement_id::text=$4) AND amount_rub>0`, partner, year, period, agreement).Scan(&teacher, &program, &top)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка проверки обязательностей")
		return
	}
	middleware.WriteJSON(w, 200, obligations(kind, teacher, program, top))
}
