package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cybercalc/internal/apicontract"
	"cybercalc/internal/config"
	"cybercalc/internal/platform/featureflags"
)

// publicOperations — единственные операции, доступные без входа в систему.
// Список закрытый: новый маршрут по умолчанию требует аутентификации, и чтобы
// открыть его всем, нужно осознанно добавить строку сюда.
var publicOperations = map[string]string{
	"GET /api/health":       "проверка живости для балансировщика",
	"GET /api/live":         "проверка живости контейнера",
	"GET /api/ready":        "готовность принимать запросы",
	"GET /api/features":     "набор включённых возможностей для интерфейса",
	"POST /api/auth/login":  "вход в систему",
	"POST /api/auth/logout": "выход, в том числе с истёкшей сессией",
}

// QA-02: перебор всех зарегистрированных маршрутов. Отдельные тесты проверяют
// отдельные обработчики, и маршрут, добавленный без защиты, между ними
// проскальзывает. Здесь проверяется весь инвентарь целиком.
func TestEveryRegisteredEndpointRequiresAuthentication(t *testing.T) {
	operations, err := apicontract.DiscoverModuleRoutes(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) == 0 {
		t.Fatal("не найдено ни одного зарегистрированного маршрута")
	}
	allFlags, err := featureflags.Parse("all")
	if err != nil {
		t.Fatal(err)
	}
	// База не подключается: запрос обязан быть отклонён до обращения к ней.
	handler := BuildRoutes(nil, config.Config{BackendFeatureFlags: allFlags})

	checked := 0
	for _, operation := range operations {
		name := operation.String()
		if _, public := publicOperations[name]; public {
			continue
		}
		checked++
		// Параметры пути заменяются значением, чтобы маршрут сопоставился.
		path := operation.Path
		for strings.Contains(path, "{") {
			start := strings.Index(path, "{")
			end := strings.Index(path[start:], "}")
			if end < 0 {
				break
			}
			path = path[:start] + "example" + path[start+end+1:]
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(operation.Method, path, nil))
		// Безопасный метод отклоняется отсутствием сессии; изменяющий сначала
		// упирается в защиту от межсайтовых запросов — она срабатывает раньше
		// и тоже не пускает запрос к обработчику.
		if operation.Method == http.MethodGet || operation.Method == http.MethodHead {
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("%s: без входа в систему ожидался 401, получено %d", name, recorder.Code)
			}
			continue
		}
		if recorder.Code != http.StatusUnauthorized && recorder.Code != http.StatusForbidden {
			t.Errorf("%s: без входа в систему ожидался отказ 401 или 403, получено %d", name, recorder.Code)
		}
	}
	if checked == 0 {
		t.Fatal("перебор не проверил ни одного защищённого маршрута")
	}
}

// Публичными остаются только перечисленные операции: если маршрут выпал из
// инвентаря, список устарел и его нужно пересмотреть вместе с маршрутом.
func TestPublicOperationsListStaysAccurate(t *testing.T) {
	operations, err := apicontract.DiscoverModuleRoutes(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, operation := range operations {
		registered[operation.String()] = true
	}
	for name, reason := range publicOperations {
		if !registered[name] {
			t.Errorf("операция %s больше не зарегистрирована, но числится публичной (%s)", name, reason)
		}
		if reason == "" {
			t.Errorf("у публичной операции %s не указана причина", name)
		}
	}
}

// Административные маршруты защищены проверкой роли. Исключения допускаются
// только осознанно: обработчик может проверять полномочия сам, если доступ шире
// администраторского.
var adminRoutesGuardedInHandler = map[string]string{
	"GET /api/admin/directory-template": "справочник ОО просматривает любой профиль организации, проверка внутри обработчика",
	"POST /api/admin/directory-import":  "загрузку справочника ведёт профиль организации, проверка внутри обработчика",
}

func TestAdministrationEndpointsRequireAdminRole(t *testing.T) {
	operations, err := apicontract.DiscoverModuleRoutes(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := readAllModuleSources(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}

	administrative := 0
	for _, operation := range operations {
		if !strings.HasPrefix(operation.Path, "/api/admin/") {
			continue
		}
		administrative++
		name := operation.String()
		declaration := routeDeclaration(sources, operation.Method, operation.Path)
		if declaration == "" {
			t.Errorf("%s: не найдено объявление маршрута", name)
			continue
		}
		if strings.Contains(declaration, "middleware.RequireAdmin") {
			continue
		}
		if reason, allowed := adminRoutesGuardedInHandler[name]; allowed {
			if reason == "" {
				t.Errorf("%s: исключение без объяснения", name)
			}
			continue
		}
		t.Errorf("%s: административный маршрут без RequireAdmin и без объяснённого исключения", name)
	}
	if administrative == 0 {
		t.Fatal("административные маршруты не найдены")
	}
	// Список исключений не должен переживать сами маршруты.
	registered := map[string]bool{}
	for _, operation := range operations {
		registered[operation.String()] = true
	}
	for name := range adminRoutesGuardedInHandler {
		if !registered[name] {
			t.Errorf("исключение %s указано для несуществующего маршрута", name)
		}
	}
}

// routeDeclaration находит строку регистрации маршрута в исходниках модулей.
func routeDeclaration(sources, method, path string) string {
	needle := `"` + method + " " + path + `"`
	for _, line := range strings.Split(sources, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// readAllModuleSources собирает исходники всех модулей: инвентарь маршрутов
// проверяется по объявлениям, а не по поведению.
func readAllModuleSources(root string) (string, error) {
	var builder strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		builder.Write(body)
		builder.WriteString("\n")
		return nil
	})
	return builder.String(), err
}
