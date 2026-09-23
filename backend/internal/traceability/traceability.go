// Package traceability связывает задачи плана реализации ТЗ 4.4 с кодом,
// тестами и документами (INTG-06).
//
// Связь строится из того, что уже лежит в репозитории: идентификаторы задач,
// упомянутые в исходниках, тестах и документах, плюс файл явных свидетельств
// для задач, которые в коде по номеру не названы. Инвариант проверяется
// тестом: выполненная задача без единой связи невозможна, а свидетельство,
// указывающее на несуществующий файл, ломает проверку. Матрица не подменяет
// приёмку: она показывает, где искать исполнение задачи.
package traceability

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Status — состояние задачи в плане.
type Status string

const (
	Done    Status = "выполнено"
	Partial Status = "частично"
	Open    Status = "не начато"
)

// Task — строка плана.
type Task struct {
	ID     string
	Title  string
	Status Status
}

var taskRow = regexp.MustCompile(`^\| ([A-Z]+-\d+) \|(.*)$`)

// ParsePlan читает таблицы плана. Идентификатор может встречаться в плане
// несколько раз (сводные таблицы); статус берётся сильнейший: выполненная в
// одной таблице задача выполнена.
func ParsePlan(r io.Reader) ([]Task, error) {
	byID := map[string]*Task{}
	var order []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		match := taskRow.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		line := match[2]
		status := Open
		switch {
		case strings.Contains(line, "ВЫПОЛНЕНО"):
			status = Done
		case strings.Contains(line, "ЧАСТИЧНО"):
			status = Partial
		}
		title := strings.TrimSpace(strings.SplitN(line, "|", 2)[0])
		title = strings.TrimSpace(strings.Split(title, " — **")[0])
		if existing, ok := byID[match[1]]; ok {
			if rank(status) > rank(existing.Status) {
				existing.Status = status
			}
			continue
		}
		byID[match[1]] = &Task{ID: match[1], Title: title, Status: status}
		order = append(order, match[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(order))
	for _, id := range order {
		tasks = append(tasks, *byID[id])
	}
	return tasks, nil
}

func rank(s Status) int {
	switch s {
	case Done:
		return 2
	case Partial:
		return 1
	}
	return 0
}

// Kind — вид связи.
type Kind string

const (
	Code Kind = "код"
	Test Kind = "тест"
	Doc  Kind = "документ"
)

func classify(path string) Kind {
	switch {
	case strings.HasSuffix(path, "_test.go"), strings.HasSuffix(path, ".test.cjs"),
		strings.HasPrefix(path, "tests/"), strings.Contains(path, "/testdata/"):
		return Test
	case strings.HasSuffix(path, ".md"):
		return Doc
	}
	return Code
}

// Row — задача со связями.
type Row struct {
	Task
	Code, Tests, Docs []string
	// Explicit — связи взяты из файла свидетельств, а не из упоминаний в тексте.
	Explicit bool
}

// References — есть ли у задачи хоть одна связь.
func (r Row) References() int { return len(r.Code) + len(r.Tests) + len(r.Docs) }

var idPattern = regexp.MustCompile(`\b([A-Z]{2,6}-\d{2})\b`)

var scanExtensions = map[string]bool{".go": true, ".js": true, ".cjs": true, ".mjs": true, ".sql": true, ".sh": true, ".yml": true, ".md": true}

var skipDirs = map[string]bool{"node_modules": true, ".git": true, "dist": true, "security-artifacts": true}

// Scan собирает упоминания идентификаторов задач. План и сама матрица не
// считаются: ссылаться на себя они могут сколько угодно.
func Scan(root string, known map[string]bool) (map[string]map[string]bool, error) {
	found := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if !scanExtensions[filepath.Ext(rel)] || rel == "docs/TZ_4_4_PARALLEL_PLAN.md" || rel == "docs/TRACEABILITY.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range idPattern.FindAllStringSubmatch(string(data), -1) {
			if known[match[1]] {
				if found[match[1]] == nil {
					found[match[1]] = map[string]bool{}
				}
				found[match[1]][rel] = true
			}
		}
		return nil
	})
	return found, err
}

// Evidence — явные свидетельства: задача → пути. Путь может быть каталогом.
type Evidence map[string][]string

// LoadEvidence читает файл свидетельств; отсутствующий файл — пустой набор.
func LoadEvidence(path string) (Evidence, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Evidence{}, nil
	}
	if err != nil {
		return nil, err
	}
	var evidence Evidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return nil, fmt.Errorf("свидетельства: %w", err)
	}
	return evidence, nil
}

// Build строит матрицу для корня репозитория.
func Build(root string) ([]Row, error) {
	plan, err := os.Open(filepath.Join(root, "docs", "TZ_4_4_PARALLEL_PLAN.md"))
	if err != nil {
		return nil, err
	}
	defer plan.Close()
	tasks, err := ParsePlan(plan)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, task := range tasks {
		known[task.ID] = true
	}
	mentions, err := Scan(root, known)
	if err != nil {
		return nil, err
	}
	evidence, err := LoadEvidence(filepath.Join(root, "docs", "traceability_evidence.json"))
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(tasks))
	for _, task := range tasks {
		row := Row{Task: task}
		add := func(path string) {
			switch classify(path) {
			case Test:
				row.Tests = append(row.Tests, path)
			case Doc:
				row.Docs = append(row.Docs, path)
			default:
				row.Code = append(row.Code, path)
			}
		}
		for path := range mentions[task.ID] {
			add(path)
		}
		if paths, ok := evidence[task.ID]; ok {
			row.Explicit = true
			for _, path := range paths {
				add(path)
			}
		}
		sort.Strings(row.Code)
		sort.Strings(row.Tests)
		sort.Strings(row.Docs)
		rows = append(rows, row)
	}
	return rows, nil
}

// Problems проверяет инвариант матрицы: у выполненной и частично выполненной
// задачи есть хотя бы одна связь с кодом или тестом (документа мало — он не
// исполняет задачу, кроме свидетельств, явно названных документами), а каждое
// свидетельство указывает на существующий файл или каталог.
func Problems(root string, rows []Row) []string {
	var problems []string
	evidence, err := LoadEvidence(filepath.Join(root, "docs", "traceability_evidence.json"))
	if err != nil {
		return []string{err.Error()}
	}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[row.ID] = true
		if row.Status == Open {
			continue
		}
		if row.References() == 0 {
			problems = append(problems, fmt.Sprintf("%s (%s): задача %s, но не связана ни с кодом, ни с тестом, ни с документом — упомяните её номер в тесте или коде либо добавьте путь в docs/traceability_evidence.json", row.ID, row.Title, row.Status))
		}
	}
	var evidenceIDs []string
	for id := range evidence {
		evidenceIDs = append(evidenceIDs, id)
	}
	sort.Strings(evidenceIDs)
	for _, id := range evidenceIDs {
		if !ids[id] {
			problems = append(problems, fmt.Sprintf("свидетельство для неизвестной задачи %s", id))
		}
		if len(evidence[id]) == 0 {
			problems = append(problems, fmt.Sprintf("свидетельство %s пусто", id))
		}
		for _, path := range evidence[id] {
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				problems = append(problems, fmt.Sprintf("свидетельство %s: путь %s не существует", id, path))
			}
		}
	}
	return problems
}

// Markdown оформляет матрицу таблицей.
func Markdown(rows []Row) string {
	var b strings.Builder
	b.WriteString("# Трассировка задач плана ТЗ 4.4\n\n")
	b.WriteString("Файл создаётся командой `go run ./cmd/traceability > ../docs/TRACEABILITY.md` (из каталога `backend`).\n")
	b.WriteString("Связи берутся из упоминаний идентификаторов задач в коде, тестах и документах и из\n")
	b.WriteString("`docs/traceability_evidence.json`. Матрица показывает, где искать исполнение задачи; результат\n")
	b.WriteString("приёмки она не заменяет.\n\n")
	counts := map[Status]int{}
	for _, row := range rows {
		counts[row.Status]++
	}
	fmt.Fprintf(&b, "Задач: %d — выполнено %d, частично %d, не начато %d.\n\n", len(rows), counts[Done], counts[Partial], counts[Open])
	b.WriteString("| Задача | Название | Статус | Код | Тесты | Документы |\n|---|---|---|---|---|---|\n")
	cell := func(paths []string) string {
		if len(paths) == 0 {
			return "—"
		}
		shown := paths
		suffix := ""
		if len(shown) > 4 {
			shown, suffix = shown[:4], fmt.Sprintf(" и ещё %d", len(paths)-4)
		}
		quoted := make([]string, len(shown))
		for i, path := range shown {
			quoted[i] = "`" + path + "`"
		}
		return strings.Join(quoted, "<br>") + suffix
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", row.ID, strings.ReplaceAll(row.Title, "|", "/"), row.Status, cell(row.Code), cell(row.Tests), cell(row.Docs))
	}
	return b.String()
}
