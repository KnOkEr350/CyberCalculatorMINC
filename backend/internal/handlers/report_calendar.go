package handlers

import (
	"net/http"
	"strconv"
	"time"

	"cybercalc/internal/middleware"
)

// ReportCalendarHandlers отдаёт контрольные даты Приказа № 270 (WF-05).
type ReportCalendarHandlers struct{}

type regulatoryMilestone struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Date     string `json:"date"` // YYYY-MM-DD, московская дата
	DaysLeft int    `json:"days_left"`
	Overdue  bool   `json:"overdue"`
	Basis    string `json:"basis"`
}

// regulatoryMilestones возвращает контрольные даты отчётного года. Счёт дней
// ведётся по московским датам (ADR-02): «осталось дней» не должно зависеть от
// часового пояса браузера, а сама дата — от времени суток.
func regulatoryMilestones(year int, now time.Time) []regulatoryMilestone {
	nowYear, nowMonth, nowDay := now.In(businessLocation).Date()
	today := time.Date(nowYear, nowMonth, nowDay, 0, 0, 0, 0, businessLocation)
	plan := []struct {
		code, label, basis string
		date               time.Time
	}{
		{"budget_notified", "Минцифры доводит объём финансового обеспечения", "Приказ № 270, ежегодно до 31 июля",
			time.Date(year, time.July, 31, 0, 0, 0, 0, businessLocation)},
		{"epgu_published", "Перечни ИТ-специальностей и ОО размещены на ЕПГУ", "Приказ № 270, ежегодно до 15 марта",
			time.Date(year, time.March, 15, 0, 0, 0, 0, businessLocation)},
		{"preliminary_cutoff", "Срез предварительного перечня", "Приказ № 270, факт на 1 ноября",
			time.Date(year, time.November, 1, 0, 0, 0, 0, businessLocation)},
		{"preliminary_sent", "Предварительный перечень направлен в ОО / РОИВ", "Приказ № 270, не позднее 10 ноября",
			time.Date(year, time.November, 10, 0, 0, 0, 0, businessLocation)},
		{"preliminary_report", "Предварительный отчёт направлен в Минцифры", "Приказ № 270, не позднее 10 декабря",
			time.Date(year, time.December, 10, 0, 0, 0, 0, businessLocation)},
		{"final_cutoff", "Итоговый срез", "Приказ № 270, факт на 31 декабря",
			time.Date(year, time.December, 31, 0, 0, 0, 0, businessLocation)},
		{"final_sent", "Итоговый перечень направлен в ОО / РОИВ", "Приказ № 270, не позднее 1 марта следующего года",
			time.Date(year+1, time.March, 1, 0, 0, 0, 0, businessLocation)},
	}
	milestones := make([]regulatoryMilestone, 0, len(plan))
	for _, item := range plan {
		days := int(item.date.Sub(today).Hours() / 24)
		milestones = append(milestones, regulatoryMilestone{
			Code: item.code, Label: item.label, Date: item.date.Format("2006-01-02"),
			DaysLeft: days, Overdue: days < 0, Basis: item.basis,
		})
	}
	return milestones
}

func (ReportCalendarHandlers) Get(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
	year, err := strconv.Atoi(r.URL.Query().Get("report_year"))
	if err != nil || year < 2000 || year > 2100 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите report_year")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, regulatoryMilestones(year, time.Now()))
}
