package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
	"cybercalc/internal/xlsx"
)

// TCH-07 на реальной БД: шаблон импорта нагрузки, атомарность и протокол
// ошибок. Ошибка в одной строке не должна оставлять в базе «половину» файла.
func TestTeachingLoadImportIsAtomicAndReportsErrors(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())

	adminUser, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	itCompany, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: adminUser.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.AssignUserToCompany(ctx, adminUser.ID, itCompany.ID); err != nil {
		t.Fatal(err)
	}
	university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	partnerFixture, err := f.CreatePartner(ctx, itCompany, university)
	if err != nil {
		t.Fatal(err)
	}
	agreementFixture, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: itCompany.ID,
		PartnerIDs: []string{partnerFixture.ID}, CreatedBy: adminUser.ID, ActivityCodes: []string{"teachers"}})
	if err != nil {
		t.Fatal(err)
	}
	admin, company, partner, agreement := adminUser.ID, itCompany.ID, partnerFixture.ID, agreementFixture.ID
	handlers := EntryHandlers{DB: db}
	user := middleware.AuthUser{ID: admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company}

	// Шаблон отдаёт подписи полей вида, чтобы файл можно было заполнить.
	recorder := httptest.NewRecorder()
	handlers.ImportTemplate(recorder, httptest.NewRequest("GET", "/api/entries/import-template?category_code=teachers", nil), user)
	if recorder.Code != 200 {
		t.Fatalf("шаблон вернул %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("шаблон импорта пуст")
	}

	headers := []string{
		"Наименование курса", "ФИО преподавателя", "Количество ак.ч.",
		"Уровень образовательной программы", "Семестр", "Как оформлен",
		"Код ОКЗ", "Подтверждённый ИТ-стаж за последние 5 лет, дней",
	}
	rowFor := func(hours interface{}, semester interface{}) []interface{} {
		return []interface{}{"Архитектура ИС", "Сидоров П.В.", hours, "bachelor", semester, "ГПХ", "2512", 400}
	}
	workbook := func(rows [][]interface{}) []byte {
		wb := xlsx.New()
		wb.AddSheet("Данные", headers, rows)
		body, err := wb.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	importPath := fmt.Sprintf("/api/entries/import?partner_id=%s&agreement_id=%s&category_code=teachers&period_type=plan&report_year=%d&commit=1",
		partner, agreement, year)

	countEntries := func() int {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE agreement_id::text=$1 AND category_code='teachers'`, agreement).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	// Файл с одной негодной строкой: семестр вне матрицы бакалавриата.
	broken := object(t, uploadWorkbook(t, handlers, user, importPath, workbook([][]interface{}{
		rowFor(64, 5), rowFor(32, 99),
	})))
	if len(broken["errors"].([]interface{})) == 0 {
		t.Fatalf("негодная строка должна попасть в протокол ошибок: %v", broken)
	}
	if broken["committed"] == true {
		t.Fatal("файл с ошибками не должен коммититься")
	}
	if got := countEntries(); got != 0 {
		t.Fatalf("после отклонённого импорта в базе %d записей, ожидалось 0 — импорт не атомарен", got)
	}

	// Корректный файл проходит целиком.
	good := object(t, uploadWorkbook(t, handlers, user, importPath, workbook([][]interface{}{
		rowFor(64, 5), rowFor(32, 6),
	})))
	if len(good["errors"].([]interface{})) != 0 {
		t.Fatalf("корректный файл отклонён: %v", good["errors"])
	}
	if good["committed"] != true {
		t.Fatalf("корректный файл должен быть зафиксирован: %v", good)
	}
	if got := countEntries(); got != 2 {
		t.Fatalf("после импорта в базе %d записей, ожидалось 2", got)
	}

	// Повторная загрузка того же файла отклоняется, а не задваивает нагрузку.
	code, repeat := uploadWorkbookStatus(t, handlers, user, importPath, workbook([][]interface{}{
		rowFor(64, 5), rowFor(32, 6),
	}))
	if code != 409 {
		t.Fatalf("повторный импорт вернул %d: %s", code, repeat)
	}
	if got := countEntries(); got != 2 {
		t.Fatalf("повторный импорт задвоил записи: в базе %d, ожидалось 2", got)
	}
}

func uploadWorkbook(t *testing.T, handlers EntryHandlers, user middleware.AuthUser, path string, book []byte) []byte {
	t.Helper()
	code, body := uploadWorkbookStatus(t, handlers, user, path, book)
	// 200 — предпросмотр или отклонённый файл, 201 — зафиксированный импорт.
	if code != 200 && code != 201 {
		t.Fatalf("импорт вернул %d: %s", code, body)
	}
	return body
}

func uploadWorkbookStatus(t *testing.T, handlers EntryHandlers, user middleware.AuthUser, path string, book []byte) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "load.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(book); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	handlers.Import(recorder, request, user)
	return recorder.Code, recorder.Body.Bytes()
}

func object(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()
	var value map[string]interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return value
}
