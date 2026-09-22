package handlers

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

// STORE-04: лимит одного вложения — 20 МБ, и backend с nginx должны
// отклонять превышение согласованно, иначе файл успевает дойти до диска.
func TestAttachmentSizeLimitAgreesWithNginx(t *testing.T) {
	const want = 20 << 20
	if maxAttachmentSize != want {
		t.Fatalf("лимит вложения %d байт, ожидалось %d (20 МБ)", maxAttachmentSize, want)
	}
	// Backend принимает тело на 1 МБ больше лимита файла — это запас на
	// служебные поля multipart. Прокси обязан пропускать ровно столько же:
	// меньше — и файл под лимитом оборвётся на прокси, больше — и лишние
	// мегабайты дойдут до диска прежде, чем их отклонит backend.
	wantProxyMB := int((maxAttachmentSize + (1 << 20)) >> 20)
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("не прочитать конфигурацию nginx: %v", err)
	}
	uploadLimit := regexp.MustCompile(`client_max_body_size\s+(\d+)m`).FindStringSubmatch(string(body))
	if uploadLimit == nil {
		t.Fatal("в конфигурации nginx нет client_max_body_size — лимит держится только на backend")
	}
	got, err := strconv.Atoi(uploadLimit[1])
	if err != nil {
		t.Fatal(err)
	}
	if got != wantProxyMB {
		t.Fatalf("nginx пропускает %d МБ, а backend рассчитан на %d МБ (файл 20 МБ плюс запас multipart)", got, wantProxyMB)
	}
	// Логин ограничен отдельно и гораздо жёстче, чем загрузка файлов.
	if !strings.Contains(string(body), "client_max_body_size 8k") {
		t.Fatal("для входа ожидался отдельный жёсткий лимит тела запроса")
	}
}

// WF-07: снимок на 1 мая формируют только администраторы, а скачивать
// полный снимок организации может ещё и аудитор — но не куратор и не
// профильные специалисты.
func TestSnapshotRolesAreRestricted(t *testing.T) {
	handlers := SnapshotHandlers{} // роль проверяется до обращения к БД
	call := func(role models.Role, create bool) int {
		user := middleware.AuthUser{Role: role, EntityType: models.EntityOrganization}
		recorder := httptest.NewRecorder()
		if create {
			handlers.Create(recorder, httptest.NewRequest("POST", "/api/report-snapshots?report_year=2026", nil), user)
		} else {
			handlers.Download(recorder, httptest.NewRequest("GET", "/api/report-snapshots/x", nil), user, "x")
		}
		return recorder.Code
	}
	mayCreate := []models.Role{models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin}
	mayNotCreate := []models.Role{models.RoleCurator, models.RoleHRSpecialist, models.RoleFinancialSpecialist, models.RoleLegalSpecialist, models.RoleAuditorViewer}
	for _, role := range mayCreate {
		if code := call(role, true); code == 403 {
			t.Fatalf("роль %q должна формировать снимок, получено 403", role)
		}
	}
	for _, role := range mayNotCreate {
		if code := call(role, true); code != 403 {
			t.Fatalf("роль %q не должна формировать снимок, получено %d", role, code)
		}
	}
	// Аудитор снимок не создаёт, но инспектирует готовый.
	if code := call(models.RoleAuditorViewer, false); code == 403 {
		t.Fatal("аудитор должен иметь доступ к скачиванию снимка для инспекции")
	}
	for _, role := range []models.Role{models.RoleCurator, models.RoleHRSpecialist, models.RoleLegalSpecialist} {
		if code := call(role, false); code != 403 {
			t.Fatalf("роль %q не должна скачивать полный снимок организации, получено %d", role, code)
		}
	}
}

// REPORT-03: Приложение № 4 — ровно 13 граф формы, где план на 31 декабря и
// факт на 1 мая стоят отдельными колонками.
func TestAnnex4HasThirteenColumns(t *testing.T) {
	if len(annex4Headers) != 13 {
		t.Fatalf("в форме %d граф, Приложение № 4 состоит из 13", len(annex4Headers))
	}
	joined := strings.Join(annex4Headers, "|")
	for _, fragment := range []string{
		"Наименование ОО / РОИВ", "Реквизиты соглашения", "Единица измерения",
		"Объём: план на 31 декабря", "Объём: факт на 1 мая",
		"Охват: план на 31 декабря, чел.", "Охват: факт на 1 мая, чел.",
		"тыс. руб.",
	} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("в графах нет обязательного элемента формы %q: %v", fragment, annex4Headers)
		}
	}
	seen := map[string]bool{}
	for _, header := range annex4Headers {
		if seen[header] {
			t.Fatalf("графа %q продублирована", header)
		}
		seen[header] = true
	}
}
