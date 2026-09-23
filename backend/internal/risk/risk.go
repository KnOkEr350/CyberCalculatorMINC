// Package risk — общий движок правил риска (RISK-01, ADR-15, ADR-18).
//
// Риск — внутренняя аналитика: он вычисляется из независимых осей
// проверки (готовность, допуск к зачёту, утверждение, юридическое сомнение) и
// не хранится и не редактируется вручную. Ни одна ось не подменяется риском и
// не подменяет его: движок только читает их результаты.
//
// Правила — упорядоченный перечень с идентификатором и уровнем, а результат
// объясним: каждый сработавший фактор называет своё правило. Уровень равен
// самому тяжёлому из сработавших правил, поэтому добавить правило можно, не
// меняя остальные. Набор правил версионируется: смена состава или уровня —
// новая версия, а результат всегда несёт версию, по которой посчитан.
//
// Пересчёт по событию обеспечен тем, что значение нигде не хранится: движок
// вызывается при каждом чтении проекции, и любое событие (загрузка документа,
// утверждение отчёта, постановка сомнения) видно в следующем же ответе.
package risk

// RulesetVersion — версия набора правил. Меняется при любом изменении Rules.
const RulesetVersion = "risk-1.0"

// Level — уровень риска.
type Level string

const (
	Ready  Level = "READY"
	Medium Level = "MEDIUM"
	High   Level = "HIGH"
)

func (l Level) weight() int {
	switch l {
	case Ready:
		return 0
	case Medium:
		return 1
	default:
		return 2
	}
}

// State — цвет для существующих потребителей (дашборд, отчёты): READY — green,
// MEDIUM — yellow, HIGH — red.
func (l Level) State() string {
	switch l {
	case Ready:
		return "green"
	case Medium:
		return "yellow"
	default:
		return "red"
	}
}

// Inputs — результаты независимых проверок, над которыми работают правила.
type Inputs struct {
	// ReadinessState — green, yellow или red по движку готовности комплекта.
	ReadinessState    string
	ReadinessBlocking []string
	ReadinessWarnings []string
	// Eligible — запись допущена к зачёту нормативным согласованием.
	Eligible bool
	// Approved — отчётный комплект утверждён.
	Approved bool
	// Disputed — запись под действующим юридическим сомнением.
	Disputed      bool
	DisputeReason string
}

// Rule — правило: идентификатор, уровень, условие и текст причин.
type Rule struct {
	ID    string
	Level Level
	Title string
	// Messages возвращает причины, если правило сработало, иначе nil.
	Messages func(Inputs) []string
}

// Rules — набор правил версии RulesetVersion в порядке показа причин.
var Rules = []Rule{
	{
		ID: "readiness.blocking", Level: High, Title: "Комплект не готов: есть блокирующие замечания",
		Messages: func(in Inputs) []string {
			if in.ReadinessState == "red" {
				return nonNil(in.ReadinessBlocking)
			}
			return nil
		},
	},
	{
		ID: "readiness.unknown", Level: High, Title: "Состояние готовности неизвестно",
		Messages: func(in Inputs) []string {
			switch in.ReadinessState {
			case "green", "yellow", "red":
				return nil
			}
			return []string{"Состояние готовности не определено: без доказательства запись считается рискованной"}
		},
	},
	{
		ID: "legal_dispute.active", Level: High, Title: "Запись поставлена юристом под сомнение",
		Messages: func(in Inputs) []string {
			if !in.Disputed {
				return nil
			}
			if in.DisputeReason != "" {
				return []string{"Юридическое сомнение: " + in.DisputeReason}
			}
			return []string{"Запись поставлена под юридическое сомнение"}
		},
	},
	{
		ID: "readiness.warnings", Level: Medium, Title: "Комплект готов с предупреждениями",
		Messages: func(in Inputs) []string {
			if in.ReadinessState == "yellow" {
				return nonNil(in.ReadinessWarnings)
			}
			return nil
		},
	},
	{
		ID: "eligibility.not_passed", Level: Medium, Title: "Мероприятие не допущено к зачёту",
		Messages: func(in Inputs) []string {
			if !in.Eligible {
				return []string{"Мероприятие не прошло нормативное согласование"}
			}
			return nil
		},
	},
	{
		// Допущенная запись при неутверждённом комплекте — рассогласование
		// осей: зачёту доверять нельзя, пока комплект не утверждён.
		ID: "approval.not_approved", Level: Medium, Title: "Отчётный комплект не утверждён",
		Messages: func(in Inputs) []string {
			if in.Eligible && !in.Approved {
				return []string{"Отчётный комплект не утверждён"}
			}
			return nil
		},
	},
}

// nonNil отличает «сработало без причин» от «не сработало».
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// Finding — сработавшее правило с его причинами.
type Finding struct {
	Rule     string   `json:"rule"`
	Level    Level    `json:"level"`
	Title    string   `json:"title"`
	Messages []string `json:"messages"`
}

// Assessment — объяснимый результат.
type Assessment struct {
	Level    Level     `json:"level"`
	Findings []Finding `json:"findings"`
	Version  string    `json:"ruleset_version"`
}

// State — цвет результата.
func (a Assessment) State() string { return a.Level.State() }

// Reasons — плоский перечень причин в порядке правил.
func (a Assessment) Reasons() []string {
	out := []string{}
	for _, finding := range a.Findings {
		out = append(out, finding.Messages...)
	}
	return out
}

// Evaluate применяет все правила. Правило без текста причин всё равно
// срабатывает с названием: уровень не должен зависеть от того, заполнили ли
// причины.
func Evaluate(in Inputs) Assessment {
	result := Assessment{Level: Ready, Findings: []Finding{}, Version: RulesetVersion}
	for _, rule := range Rules {
		messages := rule.Messages(in)
		if messages == nil {
			continue
		}
		if len(messages) == 0 {
			messages = []string{rule.Title}
		}
		result.Findings = append(result.Findings, Finding{Rule: rule.ID, Level: rule.Level, Title: rule.Title, Messages: messages})
		if rule.Level.weight() > result.Level.weight() {
			result.Level = rule.Level
		}
	}
	return result
}
