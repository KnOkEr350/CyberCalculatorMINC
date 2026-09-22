package compliance

import (
	"strings"
	"testing"
)

// QA-03 / ADR-15: риск всегда вычисляется движком. Каждый результат несёт
// версию правил и объяснимые причины, а состояние может быть только одним из
// трёх — «своего» цвета пользователь выбрать не может.
func TestEveryVerdictIsExplainableAndVersioned(t *testing.T) {
	type scenario struct {
		category  string
		period    string
		payload   map[string]interface{}
		documents []string
	}
	scenarios := []scenario{
		{"teachers", "fact", teacherPayload(), teacherDocuments()},
		{"teachers", "fact", map[string]interface{}{}, nil},
		{"ood_rpd", "fact", programPayload(), programDocuments()},
		{"ood_rpd", "plan", map[string]interface{}{}, nil},
		{"internship", "fact", internshipPayload(), internshipDocuments()},
		{"employment_practice", "fact", practicePayload(), practiceDocuments()},
		{"top_it", "fact", topPayload(1000), topDocuments()},
		{"top_it", "fact", topPayload(100), topDocuments()},
		{"it_clubs", "fact", map[string]interface{}{}, schoolDocuments()},
		{"teacher_training", "plan", map[string]interface{}{}, nil},
		{"edu_content", "fact", map[string]interface{}{}, nil},
		{"minc_decision", "fact", map[string]interface{}{}, nil},
	}
	allowed := map[string]bool{"green": true, "yellow": true, "red": true}
	for _, s := range scenarios {
		got := Evaluate(s.category, s.period, s.payload, s.documents)
		if !allowed[got.State] {
			t.Fatalf("%s/%s: состояние %q вне green/yellow/red", s.category, s.period, got.State)
		}
		if got.RulesetVersion == "" {
			t.Fatalf("%s/%s: результат без версии правил", s.category, s.period)
		}
		if len(got.Checks) == 0 {
			t.Fatalf("%s/%s: результат без перечня проверок", s.category, s.period)
		}
		switch got.State {
		case "red":
			if len(got.Blocking) == 0 {
				t.Fatalf("%s/%s: красная зона без блокирующих причин", s.category, s.period)
			}
			if got.Ready || got.Eligible {
				t.Fatalf("%s/%s: красная зона не может быть готовой или допустимой", s.category, s.period)
			}
		case "yellow":
			if len(got.Warnings) == 0 {
				t.Fatalf("%s/%s: жёлтая зона без предупреждений", s.category, s.period)
			}
		case "green":
			if len(got.Blocking) != 0 || len(got.Warnings) != 0 {
				t.Fatalf("%s/%s: зелёная зона с замечаниями: %+v", s.category, s.period, got)
			}
		}
		for _, reason := range append(append([]string{}, got.Blocking...), got.Warnings...) {
			if strings.TrimSpace(reason) == "" {
				t.Fatalf("%s/%s: пустая формулировка причины", s.category, s.period)
			}
		}
	}
}

// Значения в payload не могут подменить вердикт движка: поля вроде
// «compliance_state» не участвуют в правилах, а сам движок пересчитывает
// статус из фактов и документов.
func TestPayloadCannotOverrideVerdict(t *testing.T) {
	forged := teacherPayload()
	delete(forged, "course_name") // объективная причина красной зоны
	for _, key := range []string{"state", "compliance_state", "risk", "risk_state", "readiness", "ready", "eligible"} {
		forged[key] = "green"
	}
	got := Evaluate("teachers", "fact", forged, teacherDocuments())
	if got.State != "red" || got.Ready || got.Eligible {
		t.Fatalf("подложенные в payload поля не должны красить строку в зелёный: %+v", got)
	}
}
