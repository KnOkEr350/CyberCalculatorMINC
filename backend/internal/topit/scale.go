package topit

import "cybercalc/internal/money"

// Шкала софинансирования (ADR-14, ТЗ 4.4 §7.5): знаменатель — плановая сумма
// софинансирования X по договору, числитель — сумма, фактически списанная
// вузом. ≥100% — зелёный, от 70% до 100% — жёлтый, ниже 70% — красный.
// Это показатель хода программы: официальный зачётный объём по-прежнему берётся
// из принятого отчёта о софинансировании (ADR-19).
const (
	ScaleGreenPercent  = 100.0
	ScaleYellowPercent = 70.0
	// UniversityNormPercent — норма вуза от гранта; показывается отдельно от
	// шкалы и в её расчёте не участвует.
	UniversityNormPercent = 70
)

// Progress — ход софинансирования.
type Progress struct {
	Known   bool    `json:"known"`
	Percent float64 `json:"percent"`
	State   string  `json:"state"` // green | yellow | red | unknown
	Planned string  `json:"planned_rub,omitempty"`
	Spent   string  `json:"spent_rub"`
}

// Scale считает ход по плану X и списанной сумме. Пока план не задан или равен
// нулю, шкала неизвестна: делить не на что, а «красное» без плана вводило бы в
// заблуждение.
func Scale(planned, spent money.Amount) Progress {
	p := Progress{State: "unknown", Spent: spent.String()}
	if planned <= 0 {
		return p
	}
	p.Known, p.Planned = true, planned.String()
	// Расчёт в копейках: 1 копейка списанного не должна давать «100%».
	p.Percent = float64(spent) * 100 / float64(planned)
	p.Percent = float64(int64(p.Percent*10)) / 10 // вниз до десятой: 99,96% — не 100%
	switch {
	case spent >= planned:
		p.State = "green"
	case p.Percent >= ScaleYellowPercent:
		p.State = "yellow"
	default:
		p.State = "red"
	}
	return p
}

// UniversityNorm — норма вуза: 70% от гранта, в копейках, округление вниз.
func UniversityNorm(grant money.Amount) money.Amount {
	return grant * UniversityNormPercent / 100
}
