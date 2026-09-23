package exchange

import (
	"encoding/json"
	"fmt"
	"time"

	"cybercalc/internal/xlsx"
)

// Action — решение оператора по записи с расхождением.
type Action string

const (
	ActionAccept Action = "accept" // принять входящее значение
	ActionReject Action = "reject" // оставить локальное значение
	ActionAdd    Action = "add"    // добавить запись, которой у нас нет
)

var actionLabels = map[Action]string{
	ActionAccept: "принять входящее",
	ActionReject: "оставить локальное",
	ActionAdd:    "добавить",
}

// Decision — решение оператора вместе с причиной и автором: без причины
// решение по спорной записи не имеет смысла.
type Decision struct {
	Action Action
	Reason string
	By     string
	At     time.Time
}

// ProtocolSource описывает пакет, по которому составлен протокол.
type ProtocolSource struct {
	Manifest        Manifest
	SenderSignerID  string
	SenderOrgID     string
	CheckedAt       time.Time
	Operator        string
	PackageSHA256   string
	LocalRecordsNum int
}

var statusLabels = map[Status]string{
	StatusIdentical:       "совпадает",
	StatusCollision:       "расхождение",
	StatusMissingLocal:    "нет у нас",
	StatusMissingIncoming: "нет в пакете",
	StatusAmbiguous:       "неоднозначный ключ",
}

// BuildProtocol составляет протокол разногласий: все различия без
// исключения и решение оператора по каждому. Решение, которого ещё нет,
// показывается как «не принято»: протокол честно отражает незакрытые вопросы.
//
// Идентичные записи в перечень различий не входят — их число видно в сводке.
func BuildProtocol(source ProtocolSource, result Result, decisions map[string]Decision) ([]byte, error) {
	summary := [][]interface{}{
		{"Отправитель (организация)", source.SenderOrgID},
		{"Подписант", source.SenderSignerID},
		{"Пакет создан", source.Manifest.CreatedAt.Format(time.RFC3339)},
		{"Отчётный год", source.Manifest.ReportYear},
		{"Период", source.Manifest.Period},
		{"SHA-256 пакета", source.PackageSHA256},
		{"Проверен", source.CheckedAt.UTC().Format(time.RFC3339)},
		{"Оператор", source.Operator},
		{"Записей в пакете", source.Manifest.RecordCount},
		{"Совпадают", result.Counts[StatusIdentical]},
		{"Расхождения", result.Counts[StatusCollision]},
		{"Нет у нас", result.Counts[StatusMissingLocal]},
		{"Нет в пакете", result.Counts[StatusMissingIncoming]},
		{"Неоднозначные ключи", result.Counts[StatusAmbiguous]},
	}

	headers := []string{"Ключ", "Вид", "Состояние", "Поле", "Локальное значение", "Входящее значение",
		"Решение", "Причина", "Принял решение", "Когда"}
	rows := [][]interface{}{}
	undecided := 0
	for _, item := range result.Items {
		if item.Status == StatusIdentical {
			continue
		}
		decision, decided := decisions[item.Key.String()]
		action, reason, by, at := "не принято", "", "", ""
		if decided {
			action = actionLabels[decision.Action]
			if action == "" {
				action = string(decision.Action)
			}
			reason, by = decision.Reason, decision.By
			if !decision.At.IsZero() {
				at = decision.At.UTC().Format(time.RFC3339)
			}
		} else {
			undecided++
		}
		base := []interface{}{item.Key.String(), item.Key.Category, statusLabels[item.Status]}
		tail := []interface{}{action, reason, by, at}
		switch {
		case len(item.Differences) > 0:
			for _, diff := range item.Differences {
				rows = append(rows, concat(base, []interface{}{diff.Field, display(diff.Local), display(diff.Incoming)}, tail))
			}
		case item.Status == StatusAmbiguous:
			rows = append(rows, concat(base, []interface{}{"—", item.Detail, ""}, tail))
		case item.Local != nil:
			rows = append(rows, concat(base, []interface{}{"—", display(item.Local.Payload), ""}, tail))
		case item.Incoming != nil:
			rows = append(rows, concat(base, []interface{}{"—", "", display(item.Incoming.Payload)}, tail))
		}
	}
	summary = append(summary, []interface{}{"Различий без решения", undecided})

	workbook := xlsx.New()
	workbook.AddSheet("Сводка", []string{"Показатель", "Значение"}, summary)
	workbook.AddSheet("Различия", headers, rows)
	return workbook.Bytes()
}

func concat(parts ...[]interface{}) []interface{} {
	out := []interface{}{}
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

// display приводит значение к читаемому виду для ячейки.
func display(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool, int, int64, float64:
		return fmt.Sprint(v)
	case interface{ String() string }:
		return v.String()
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}
