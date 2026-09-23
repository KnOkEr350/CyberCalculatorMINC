package risk

import (
	"reflect"
	"testing"
)

func clean() Inputs { return Inputs{ReadinessState: "green", Eligible: true, Approved: true} }

func TestCleanRecordIsReady(t *testing.T) {
	got := Evaluate(clean())
	if got.Level != Ready || got.State() != "green" || len(got.Findings) != 0 || got.Version != RulesetVersion {
		t.Fatalf("чистая запись должна быть READY без замечаний: %+v", got)
	}
	if len(got.Reasons()) != 0 {
		t.Fatalf("у READY нет причин: %v", got.Reasons())
	}
}

func TestEachAxisMovesTheLevelIndependently(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*Inputs)
		level Level
		rule  string
	}{
		{"готовность: блокирующие замечания", func(in *Inputs) {
			in.ReadinessState = "red"
			in.ReadinessBlocking = []string{"нет договора"}
		}, High, "readiness.blocking"},
		{"готовность: предупреждения", func(in *Inputs) {
			in.ReadinessState = "yellow"
			in.ReadinessWarnings = []string{"документ ждёт проверки"}
		}, Medium, "readiness.warnings"},
		{"нет допуска к зачёту", func(in *Inputs) { in.Eligible = false; in.Approved = false }, Medium, "eligibility.not_passed"},
		{"допущена, но комплект не утверждён", func(in *Inputs) { in.Approved = false }, Medium, "approval.not_approved"},
		{"юридическое сомнение", func(in *Inputs) { in.Disputed = true; in.DisputeReason = "подпись" }, High, "legal_dispute.active"},
		{"неизвестное состояние готовности", func(in *Inputs) { in.ReadinessState = "purple" }, High, "readiness.unknown"},
	}
	for _, tc := range cases {
		in := clean()
		tc.edit(&in)
		got := Evaluate(in)
		if got.Level != tc.level {
			t.Errorf("%s: уровень %s, ожидался %s", tc.name, got.Level, tc.level)
		}
		found := false
		for _, finding := range got.Findings {
			if finding.Rule == tc.rule {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: не сработало правило %s: %+v", tc.name, tc.rule, got.Findings)
		}
	}
}

// Уровень равен самому тяжёлому правилу, а причины не теряются: жёлтое
// замечание не прячется за красным.
func TestLevelIsTheWorstRuleAndReasonsKeepTheirOrder(t *testing.T) {
	got := Evaluate(Inputs{ReadinessState: "red", ReadinessBlocking: []string{"нет акта"}, Eligible: false, Disputed: true, DisputeReason: "подпись"})
	if got.Level != High {
		t.Fatalf("самое тяжёлое правило — HIGH: %s", got.Level)
	}
	want := []string{"нет акта", "Юридическое сомнение: подпись", "Мероприятие не прошло нормативное согласование"}
	if !reflect.DeepEqual(got.Reasons(), want) {
		t.Fatalf("причины: %v, ожидалось %v", got.Reasons(), want)
	}
}

// Правило, которое сработало без причин, не выпадает из результата: уровень не
// зависит от того, заполнены ли пояснения.
func TestFiredRuleWithoutMessagesStillCounts(t *testing.T) {
	got := Evaluate(Inputs{ReadinessState: "red", Eligible: true, Approved: true})
	if got.Level != High || len(got.Findings) != 1 || len(got.Findings[0].Messages) != 1 {
		t.Fatalf("красная готовность без причин должна остаться HIGH с названием правила: %+v", got)
	}
}

func TestSamplesAreDeterministicAndRulesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, rule := range Rules {
		if rule.ID == "" || rule.Title == "" || rule.Messages == nil {
			t.Fatalf("правило описано не полностью: %+v", rule)
		}
		if seen[rule.ID] {
			t.Fatalf("правило %s повторяется", rule.ID)
		}
		seen[rule.ID] = true
		if rule.Level != Medium && rule.Level != High {
			t.Fatalf("правило %s: уровень %q не может быть READY", rule.ID, rule.Level)
		}
	}
	in := Inputs{ReadinessState: "yellow", ReadinessWarnings: []string{"а"}, Eligible: false}
	if !reflect.DeepEqual(Evaluate(in), Evaluate(in)) {
		t.Fatal("результат должен быть детерминирован")
	}
}
