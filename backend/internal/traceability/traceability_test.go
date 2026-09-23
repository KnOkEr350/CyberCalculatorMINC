package traceability

import (
	"strings"
	"testing"
)

const samplePlan = `| ID | Название |
|---|---|
| DATA-01 | Организации — **ВЫПОЛНЕНО** | L | BASE-07 | описание |
| DATA-02 | Соглашения | L | DATA-01 | описание |
| SEC-09 | Тесты — **ЧАСТИЧНО** | M | SEC-02 | описание |
| DATA-02 | Соглашения — **ВЫПОЛНЕНО** | L | DATA-01 | повтор в сводной таблице |
`

func TestParsePlanTakesTheStrongestStatus(t *testing.T) {
	tasks, err := ParsePlan(strings.NewReader(samplePlan))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Status{}
	for _, task := range tasks {
		got[task.ID] = task.Status
	}
	if len(tasks) != 3 || got["DATA-01"] != Done || got["DATA-02"] != Done || got["SEC-09"] != Partial {
		t.Fatalf("разбор плана: %+v", tasks)
	}
	if tasks[0].Title != "Организации" {
		t.Fatalf("название без пометки статуса: %q", tasks[0].Title)
	}
}

func TestClassifyPaths(t *testing.T) {
	for path, want := range map[string]Kind{
		"backend/internal/x/y.go": Code, "backend/internal/x/y_test.go": Test, "frontend/a.test.cjs": Test,
		"tests/e2e.go": Test, "docs/ADR.md": Doc, "migrations/0001.sql": Code, "backend/x/testdata/g.golden": Test,
	} {
		if got := classify(path); got != want {
			t.Errorf("%s: %s, ожидалось %s", path, got, want)
		}
	}
}

// Инвариант на самом репозитории: выполненная или частичная задача не бывает
// без связи, а каждое свидетельство указывает на существующий путь.
func TestEveryStartedTaskIsTraceable(t *testing.T) {
	rows, err := Build("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 100 {
		t.Fatalf("план разобран не полностью: %d задач", len(rows))
	}
	for _, problem := range Problems("../../..", rows) {
		t.Error(problem)
	}
}

func TestProblemsNameEveryGap(t *testing.T) {
	rows := []Row{{Task: Task{ID: "X-01", Title: "Пустая", Status: Done}}, {Task: Task{ID: "X-02", Title: "Ещё не начата", Status: Open}}}
	problems := Problems(t.TempDir(), rows)
	if len(problems) != 1 || !strings.Contains(problems[0], "X-01") {
		t.Fatalf("непривязанная выполненная задача должна называться: %v", problems)
	}
}
