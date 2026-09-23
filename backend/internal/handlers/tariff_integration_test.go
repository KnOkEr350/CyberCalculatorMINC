package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/tariffs"
	"cybercalc/internal/testfixtures"
)

// DATA-07 на реальной БД: засеянная редакция совпадает с редакцией поставки в
// коде в обе стороны, а загрузка ставок на отчётный год даёт полную карточку.
func TestSeededTariffsMatchTheShippedEdition(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()

	card, err := tariffs.Load(ctx, db, year)
	if err != nil {
		t.Fatalf("карточка на %d год: %v", year, err)
	}
	shipped := tariffs.Default()
	for _, definition := range tariffs.Definitions() {
		want, _ := shipped.Rat(definition.Code)
		got, err := card.Rat(definition.Code)
		if err != nil {
			t.Fatalf("%s: %v", definition.Code, err)
		}
		if got.Cmp(want) != 0 {
			t.Errorf("%s: в БД %s, в редакции поставки %s", definition.Code, got.RatString(), want.RatString())
		}
	}

	// В каталоге нет ставок, которых нет в редакции поставки: расхождение в
	// эту сторону означало бы ставку, которую не считает ни один расчёт.
	rows, err := db.QueryContext(ctx, `SELECT code FROM tariff_rates`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	known := map[string]bool{}
	for _, definition := range tariffs.Definitions() {
		known[definition.Code] = true
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		if !known[code] {
			t.Errorf("ставка %s есть в каталоге БД, но не описана в коде", code)
		}
	}
}

// Версии не переписываются и не пересекаются: единственное допустимое
// изменение — закрыть действующую версию датой окончания.
func TestTariffVersionsAreImmutableAndNeverOverlap(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `UPDATE tariff_versions SET amount_rub=1`); err == nil {
		t.Fatal("сумму версии менять нельзя")
	}
	if _, err := db.ExecContext(ctx, `UPDATE tariff_versions SET source_reference='подмена'`); err == nil {
		t.Fatal("основание версии менять нельзя")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM tariff_versions`); err == nil {
		t.Fatal("версию удалять нельзя")
	}
	// Действующая версия пересекается с любой новой, начинающейся внутри её периода.
	if _, err := db.ExecContext(ctx, `INSERT INTO tariff_versions(rate_code,amount_rub,valid_from,source_reference)
		VALUES($1,5000,DATE '2010-01-01','пересечение')`, tariffs.TeacherHourVuz); err == nil {
		t.Fatal("пересекающийся период должен отклоняться")
	}
	// Редакция вступает в силу с начала года.
	if _, err := db.ExecContext(ctx, `INSERT INTO tariff_versions(rate_code,amount_rub,valid_from,source_reference)
		VALUES($1,5000,DATE '2099-06-15','середина года')`, tariffs.TeacherHourVuz); err == nil {
		t.Fatal("версия с началом посреди года должна отклоняться")
	}
	// Ставка обязана быть в каталоге.
	if _, err := db.ExecContext(ctx, `INSERT INTO tariff_versions(rate_code,amount_rub,valid_from,source_reference)
		VALUES('нет.такой',5000,DATE '2099-01-01','вне каталога')`); err == nil {
		t.Fatal("версия неизвестной ставки должна отклоняться")
	}
}

// Ввод новой редакции: доступ, основание, порядок дат и закрытие прежней
// версии; сумма нового мероприятия считается по редакции своего года и хранит
// версии, из которых она посчитана.
func TestPublishingATariffChangesOnlyTheYearsItCovers(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	admins := AdminHandlers{DB: db}
	superAdmin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	// Редакция вводится на год, которого ещё нет ни у одной версии: база общая,
	// и версии не удаляются, поэтому каждый прогон берёт следующий свободный год.
	var latest int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(max(extract(year FROM valid_from))::int,2000) FROM tariff_versions WHERE rate_code=$1`,
		tariffs.PlatformTeacherMonth).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	newYear := latest + 1
	if latest < 2090 {
		newYear = 2090
	}
	if newYear > 2100 {
		t.Skip("в общей тестовой базе исчерпаны годы для новых редакций; пересоздайте базу")
	}
	publish := func(user middleware.AuthUser, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("POST", "/api/admin/tariffs", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		admins.PublishTariff(recorder, request, user)
		return recorder
	}
	body := func(from string, amount string, source string) string {
		return fmt.Sprintf(`{"rate_code":%q,"amount_rub":%q,"valid_from":%q,"source_reference":%q}`,
			tariffs.PlatformTeacherMonth, amount, from, source)
	}
	start := fmt.Sprintf("%d-01-01", newYear)

	t.Run("вводит только системный администратор", func(t *testing.T) {
		orgAdmin := middleware.AuthUser{ID: tenant.admin, Role: models.RoleOrgAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
		if got := publish(orgAdmin, body(start, "9000.00", "Приказ № 1")); got.Code != 403 {
			t.Fatalf("администратор организации не вводит тарифы, получено %d", got.Code)
		}
	})
	t.Run("основание и даты проверяются", func(t *testing.T) {
		for name, request := range map[string]string{
			"без основания":       body(start, "9000.00", "  "),
			"нулевая ставка":      body(start, "0.00", "Приказ № 1"),
			"начало посреди года": body(fmt.Sprintf("%d-06-01", newYear), "9000.00", "Приказ № 1"),
			"неизвестная ставка":  `{"rate_code":"нет.такой","amount_rub":"9000.00","valid_from":"` + start + `","source_reference":"Приказ № 1"}`,
		} {
			if got := publish(superAdmin, request); got.Code != 400 {
				t.Errorf("%s: ожидался 400, получено %d %s", name, got.Code, got.Body.String())
			}
		}
	})
	t.Run("редакция вводится и закрывает прежнюю", func(t *testing.T) {
		if got := publish(superAdmin, body(start, "9000.00", "Приказ Минцифры (изм.) № 2")); got.Code != 201 {
			t.Fatalf("ввод редакции вернул %d: %s", got.Code, got.Body.String())
		}
		var closed string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(valid_until::text,'') FROM tariff_versions
			WHERE rate_code=$1 AND valid_from < $2::date ORDER BY valid_from DESC LIMIT 1`,
			tariffs.PlatformTeacherMonth, start).Scan(&closed); err != nil {
			t.Fatal(err)
		}
		if closed != fmt.Sprintf("%d-12-31", newYear-1) {
			t.Fatalf("прежняя редакция закрыта датой %q, ожидался последний день предыдущего года", closed)
		}
		// Ввод задним числом или в ту же дату запрещён.
		if got := publish(superAdmin, body(start, "9500.00", "Приказ № 3")); got.Code != 409 {
			t.Fatalf("та же дата начала должна отклоняться: %d %s", got.Code, got.Body.String())
		}
		if got := publish(superAdmin, body(fmt.Sprintf("%d-01-01", newYear-1), "9500.00", "Приказ № 3")); got.Code != 409 {
			t.Fatalf("дата раньше действующей редакции должна отклоняться: %d %s", got.Code, got.Body.String())
		}
		// Событие попало в журнал.
		var audited int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action='tariff_publish' AND user_id::text=$1`,
			tenant.admin).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if audited < 1 {
			t.Fatal("ввод тарифа должен попадать в журнал")
		}
	})
	t.Run("каждый год считается по своей редакции", func(t *testing.T) {
		before, err := tariffs.Load(ctx, db, newYear-1)
		if err != nil {
			t.Fatal(err)
		}
		after, err := tariffs.Load(ctx, db, newYear)
		if err != nil {
			t.Fatal(err)
		}
		oldRate, _ := before.Rat(tariffs.PlatformTeacherMonth)
		newRate, _ := after.Rat(tariffs.PlatformTeacherMonth)
		if oldRate.RatString() != "8590" || newRate.RatString() != "9000" {
			t.Fatalf("ставки по годам: %s и %s, ожидались 8590 и 9000", oldRate.RatString(), newRate.RatString())
		}
		// Остальные ставки нового года не тронуты.
		other, _ := after.Rat(tariffs.PlatformStudentMonth)
		if other.RatString() != "6800" {
			t.Fatalf("соседняя ставка изменилась: %s", other.RatString())
		}
		// Открытый API показывает ту же картину.
		entries := EntryHandlers{DB: db}
		recorder := httptest.NewRecorder()
		entries.Tariffs(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/tariffs?report_year=%d", newYear), nil), superAdmin)
		if recorder.Code != 200 {
			t.Fatalf("список тарифов вернул %d", recorder.Code)
		}
		var listing struct {
			Tariffs []struct {
				Code      string `json:"code"`
				AmountRub string `json:"amount_rub"`
				Source    string `json:"source_reference"`
			} `json:"tariffs"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
			t.Fatal(err)
		}
		if len(listing.Tariffs) != len(tariffs.Definitions()) {
			t.Fatalf("в списке %d ставок из %d", len(listing.Tariffs), len(tariffs.Definitions()))
		}
		for _, item := range listing.Tariffs {
			if item.Code == tariffs.PlatformTeacherMonth &&
				(item.AmountRub != "9000.00" || !strings.Contains(item.Source, "№ 2")) {
				t.Fatalf("список показывает %s по основанию %q", item.AmountRub, item.Source)
			}
		}
	})
}

// Сумма мероприятия считается по ставкам его отчётного года и хранит версии,
// по которым посчитана: по записи видно, какая редакция дала эту сумму.
func TestEntryAmountRecordsTheTariffItWasCalculatedWith(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	handlers := EntryHandlers{DB: db}
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	create := func(reportYear int) (string, string) {
		t.Helper()
		body := fmt.Sprintf(`{"category_code":"ood_rpd","partner_id":%q,"agreement_id":%q,"period_type":"plan",
			"report_year":%d,"audience":"vuz","payload":{"org_name":%q,"doc_type":"rpd",
			"level":"vo","activity_type":"development","program_name":"Программа %d"}}`,
			tenant.partner, tenant.agreement, reportYear, tenant.partner, reportYear)
		request := httptest.NewRequest("POST", "/api/entries", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handlers.Create(recorder, request, user)
		if recorder.Code != 201 {
			t.Fatalf("создание вернуло %d: %s", recorder.Code, recorder.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		var amount string
		if err := db.QueryRowContext(ctx, `SELECT amount_rub::text FROM entries WHERE id::text=$1`, created.ID).Scan(&amount); err != nil {
			t.Fatal(err)
		}
		return created.ID, amount
	}

	id, amount := create(year)
	if amount != "300000.00" {
		t.Fatalf("сумма РПД ВО «разработка» %s, ожидалось 300000.00", amount)
	}
	var basis []byte
	if err := db.QueryRowContext(ctx, `SELECT array_to_json(formula_tariff_ids) FROM entries WHERE id::text=$1`, id).Scan(&basis); err != nil {
		t.Fatal(err)
	}
	var versionIDs []int64
	if err := json.Unmarshal(basis, &versionIDs); err != nil {
		t.Fatalf("основание суммы не сохранено: %v (%s)", err, basis)
	}
	if len(versionIDs) != len(tariffs.CategoryCodes("ood_rpd")) {
		t.Fatalf("сохранено %d версий, ожидалось по числу ставок вида (%d)", len(versionIDs), len(tariffs.CategoryCodes("ood_rpd")))
	}
	// Версии действительно принадлежат ставкам этого вида, а не чужим.
	var foreign int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tariff_versions v JOIN tariff_rates r ON r.code=v.rate_code
		WHERE v.id = ANY($1::bigint[]) AND r.category_code<>'ood_rpd'`, pqArray(versionIDs)).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if foreign != 0 {
		t.Fatalf("в основании суммы %d версий чужих видов", foreign)
	}
}

func pqArray(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprint(id)
	}
	return "{" + strings.Join(parts, ",") + "}"
}
