package featureflags

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Без флагов модули ТЗ 4.4 не подключаются и не видны в интерфейсе, поэтому
// выкладка обязана включать их явно и проверять, что они включены. Тест ловит
// расхождение: новый флаг в реестре без включения в деплое и без проверки.
func TestDeploymentEnablesEveryKnownFlagAndVerifiesIt(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	workflow := read(".github/workflows/ci-cd.yml")
	for _, variable := range []string{"BACKEND_FEATURE_FLAGS", "FRONTEND_FEATURE_FLAGS"} {
		match := regexp.MustCompile(variable + `: \$\{\{ vars\.PROD_` + variable + ` \|\| '([^']*)' \}\}`).FindStringSubmatch(workflow)
		if match == nil {
			t.Fatalf("деплой не задаёт %s: модули ТЗ 4.4 остались бы выключены", variable)
		}
		if strings.Contains(match[1], "all") {
			t.Fatalf("%s по умолчанию не должен использовать all: в production он запрещён", variable)
		}
		listed := map[string]bool{}
		for _, name := range strings.Split(match[1], ",") {
			listed[name] = true
		}
		set, err := Parse(match[1])
		if err != nil {
			t.Fatalf("%s: %v", variable, err)
		}
		for _, name := range Names() {
			if !listed[string(name)] || !set.Enabled(name) {
				t.Errorf("%s: флаг %s не включён при выкладке", variable, name)
			}
		}
	}
	script := read("scripts/verify-deployment.sh")
	for _, name := range Names() {
		if !strings.Contains(script, string(name)) {
			t.Errorf("verify-deployment.sh не проверяет маршрут модуля %s", name)
		}
	}
	for _, want := range []string{"/api/features", "instead of 401"} {
		if !strings.Contains(script, want) {
			t.Errorf("verify-deployment.sh: нет проверки %q", want)
		}
	}
	if !strings.Contains(workflow, "APP_ENV: ${{ vars.PROD_APP_ENV || 'development' }}") ||
		!strings.Contains(workflow, `[[ "$PUBLIC_URL" == https://* ]]`) {
		t.Error("режим production включается переменной и требует HTTPS")
	}
}
