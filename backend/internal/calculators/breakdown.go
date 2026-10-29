package calculators

import (
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// BreakdownInput - один множитель расчёта: показатель из записи (Key) или
// ставка Приказа (RateCode).
type BreakdownInput struct {
	Key      string
	RateCode string
	Label    string
	Value    *big.Rat
}

// IsRate сообщает, что множитель - ставка тарифной карточки.
func (in BreakdownInput) IsRate() bool { return in.RateCode != "" }

// BreakdownNode - выражение расчёта: лист ссылается на Inputs[Input],
// узел складывает или перемножает детей.
type BreakdownNode struct {
	Op    byte
	Input int
	Kids  []BreakdownNode
}

// Breakdown раскладывает сумму по методике на показатели и ставки, чтобы
// выгрузка могла записать её формулой Excel. Выражение повторяет
// CalculateAmountWith; совпадение проверяется тестом.
type Breakdown struct {
	Inputs []BreakdownInput
	Root   BreakdownNode
	Amount money.Amount
}

func leaf(index int) BreakdownNode { return BreakdownNode{Input: index} }
func sum(kids ...BreakdownNode) BreakdownNode {
	return BreakdownNode{Op: '+', Kids: kids}
}
func product(kids ...BreakdownNode) BreakdownNode {
	return BreakdownNode{Op: '*', Kids: kids}
}

// BreakdownWith строит разложение суммы записи по ставкам карточки card.
func BreakdownWith(card tariffs.Card, code string, audience models.Audience, p map[string]interface{}) (Breakdown, error) {
	amount, err := CalculateAmountWith(card, code, audience, p)
	if err != nil {
		return Breakdown{}, err
	}
	calc, err := Get(code)
	if err != nil {
		return Breakdown{}, err
	}
	labels := map[string]string{}
	for _, field := range calc.Fields() {
		labels[field.Key] = field.Label
	}
	b := Breakdown{Amount: amount}
	var buildErr error
	field := func(key string) BreakdownNode {
		value, err := ratValue(p[key])
		if err != nil && buildErr == nil {
			buildErr = fmt.Errorf("поле %s: %w", key, err)
		}
		label := labels[key]
		if label == "" {
			label = key
		}
		b.Inputs = append(b.Inputs, BreakdownInput{Key: key, Label: label, Value: value})
		return leaf(len(b.Inputs) - 1)
	}
	rate := func(rateCode string) BreakdownNode {
		value, err := card.Rat(rateCode)
		if err != nil && buildErr == nil {
			buildErr = err
		}
		label := rateCode
		if definition, ok := tariffs.DefinitionOf(rateCode); ok {
			label = fmt.Sprintf("Ставка: %s, руб./%s", definition.Label, definition.Unit)
		}
		b.Inputs = append(b.Inputs, BreakdownInput{RateCode: rateCode, Label: label, Value: value})
		return leaf(len(b.Inputs) - 1)
	}
	text := func(key string) string {
		value, _ := p[key].(string)
		return value
	}
	switch code {
	case "teachers":
		hourly := tariffs.TeacherHourVuz
		if audience == models.AudienceKolledj {
			hourly = tariffs.TeacherHourKolledj
		}
		b.Root = product(field("academic_hours"), rate(hourly))
	case "internship", "employment_practice":
		b.Root = product(sum(
			product(field("student_load_hours_per_month"), rate(tariffs.InternshipStudentHour)),
			product(field("mentor_load_hours_per_month"), rate(tariffs.InternshipMentorHour)),
		), field("duration_months"))
	case "ood_rpd":
		b.Root = rate(tariffs.OODRPD(text("doc_type"), text("level"), text("activity_type")))
	case "top_it":
		amountKey := "cofinancing_amount_rub"
		if _, ok := p["actual_spent_amount_rub"]; ok {
			amountKey = "actual_spent_amount_rub"
		}
		b.Root = field(amountKey)
	case "minc_decision":
		b.Root = field("amount_manual")
	case "it_clubs":
		b.Root = sum(
			product(field("academic_hours"), rate(tariffs.SchoolProgramHour)),
			product(field("developed_programs_count"), rate(tariffs.SchoolProgramDevelopment)),
		)
	case "teacher_training":
		b.Root = sum(
			product(field("developed_programs_count"), rate(tariffs.TeacherTrainingDevelopment)),
			product(field("academic_hours_per_teacher"), field("trained_teachers_count"), rate(tariffs.TeacherTrainingHour)),
		)
	case "edu_content":
		b.Root = sum(
			product(field("student_platform_months"), rate(tariffs.PlatformStudentMonth)),
			product(field("teacher_platform_months"), rate(tariffs.PlatformTeacherMonth)),
		)
	default:
		return Breakdown{}, fmt.Errorf("нет разложения для вида %s", code)
	}
	if buildErr != nil {
		return Breakdown{}, buildErr
	}
	return b, nil
}

func ratValue(raw interface{}) (*big.Rat, error) {
	var s string
	switch v := raw.(type) {
	case json.Number:
		s = v.String()
	case float64:
		s = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		s = strconv.Itoa(v)
	case string:
		s = strings.TrimSpace(v)
	default:
		return new(big.Rat), fmt.Errorf("не число")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return new(big.Rat), fmt.Errorf("не число")
	}
	return r, nil
}

// Eval вычисляет выражение точной арифметикой.
func (b Breakdown) Eval() *big.Rat {
	var eval func(BreakdownNode) *big.Rat
	eval = func(node BreakdownNode) *big.Rat {
		switch node.Op {
		case '+':
			out := new(big.Rat)
			for _, kid := range node.Kids {
				out.Add(out, eval(kid))
			}
			return out
		case '*':
			out := big.NewRat(1, 1)
			for _, kid := range node.Kids {
				out.Mul(out, eval(kid))
			}
			return out
		default:
			return new(big.Rat).Set(b.Inputs[node.Input].Value)
		}
	}
	return eval(b.Root)
}

// Excel записывает выражение формулой: ref возвращает адрес ячейки
// множителя. Округление до копеек - как при сохранении записи.
func (b Breakdown) Excel(ref func(index int) string) string {
	var render func(BreakdownNode, bool) string
	render = func(node BreakdownNode, nested bool) string {
		switch node.Op {
		case '+':
			parts := make([]string, len(node.Kids))
			for i, kid := range node.Kids {
				parts[i] = render(kid, false)
			}
			text := strings.Join(parts, "+")
			if nested {
				return "(" + text + ")"
			}
			return text
		case '*':
			parts := make([]string, len(node.Kids))
			for i, kid := range node.Kids {
				parts[i] = render(kid, true)
			}
			return strings.Join(parts, "*")
		default:
			return ref(node.Input)
		}
	}
	return "ROUND(" + render(b.Root, false) + ",2)"
}

// Describe - словесная запись формулы для графы «Расчёт».
func (b Breakdown) Describe() string {
	text := b.Excel(func(index int) string { return "\x00" + strconv.Itoa(index) + "\x00" })
	text = strings.TrimSuffix(strings.TrimPrefix(text, "ROUND("), ",2)")
	text = strings.NewReplacer("*", " × ", "+", " + ").Replace(text)
	for index, input := range b.Inputs {
		text = strings.ReplaceAll(text, "\x00"+strconv.Itoa(index)+"\x00", "["+input.Label+"]")
	}
	return text
}
