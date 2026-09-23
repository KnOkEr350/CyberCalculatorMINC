package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// INTG-01: корень композиции — единственное место, где модуль подключается к
// приложению. Модуль, забытый в `BuildRoutes`, компилируется и проходит
// собственные тесты, но его маршрутов в приложении нет.
//
// Признак HTTP-модуля — собственная реализация `RegisterRoutes`. Каталоги без
// неё (например `teaching`, где лежит только доменная логика) маршрутов не
// заявляют и в композицию не входят.
func TestEveryModulePackageIsMountedInComposition(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	composition, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(composition)

	mounted := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		module := entry.Name()
		declaration, err := os.ReadFile(filepath.Join("..", "modules", module, "module.go"))
		if err != nil || !strings.Contains(string(declaration), ") RegisterRoutes(") {
			continue
		}
		mounted++
		if !strings.Contains(source, "cybercalc/internal/modules/"+module) {
			t.Errorf("модуль %s не импортирован в корне композиции", module)
			continue
		}
		// Импорта мало: модуль должен ещё и создаваться в RegisterAll.
		if !strings.Contains(source, module+".New(") {
			t.Errorf("модуль %s импортирован, но не подключён в RegisterAll", module)
		}
	}
	if mounted == 0 {
		t.Fatal("не найдено ни одного HTTP-модуля")
	}
}

var routePattern = regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+ /[^"]*)"`)

// Два модуля не должны заявлять один и тот же маршрут. ServeMux ловит это
// паникой, но только когда оба модуля включены одновременно: при разных
// флагах конфликт доходит до продакшена незамеченным.
func TestModulesDoNotClaimTheSameRoute(t *testing.T) {
	root := filepath.Join("..", "modules")
	owners := map[string]string{}
	total := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		module := filepath.Base(filepath.Dir(path))
		for _, match := range routePattern.FindAllStringSubmatch(string(source), -1) {
			route := match[1]
			total++
			if owner, claimed := owners[route]; claimed && owner != module {
				t.Fatalf("маршрут %q заявлен модулями %s и %s", route, owner, module)
			}
			if owner, claimed := owners[route]; claimed && owner == module {
				t.Fatalf("маршрут %q зарегистрирован модулем %s дважды", route, module)
			}
			owners[route] = module
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("не найдено ни одного зарегистрированного маршрута")
	}
}

// Маршруты одного модуля лежат в его собственном пакете: регистрация из
// другого места ломает границу модуля и делает состав приложения
// неотслеживаемым.
func TestRoutesAreRegisteredOnlyByModules(t *testing.T) {
	outside := []string{}
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if strings.Contains(path, filepath.Join("internal", "modules")) {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if routePattern.Match(source) {
			outside = append(outside, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range outside {
		// health переиспользует собственный обработчик внутри своего модуля.
		t.Errorf("маршрут регистрируется вне пакета модуля: %s", path)
	}
}
