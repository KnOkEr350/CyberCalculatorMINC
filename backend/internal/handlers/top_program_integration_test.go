package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
	"cybercalc/internal/topit"
)

// TOP-01, TOP-04, TOP-06, TOP-07 на реальной БД: паспорт программы выводится
// из payload, а составляющие — поддержка, стипендиаты, кейсы — проверяются
// правилами вида, правами роли и границей арендатора.
func TestTopProgramModelAndItems(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own, foreign := newTenant(ctx, t, f), newTenant(ctx, t, f)
	handlers := TopItemHandlers{DB: db}

	insert := func(tn tenantScenario, category, audience, payload string) (string, error) {
		var id string
		err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES($1,$2,$3,$4,'fact',$5,$6,$7::jsonb,100000,100000,'average',$8) RETURNING id::text`,
			category, tn.partner, tn.agreement, tn.company, year, audience, payload, tn.admin).Scan(&id)
		return id, err
	}
	entry, err := insert(own, "top_it", "vuz", `{"project_name":" Цифровая кафедра ","program_name":"ТОП-ИТ","program_wave":"2","partner_role":"Anchor","grant_amount_rub":"1500000,50","planned_cofinancing_amount_rub":300000}`)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("паспорт выводится из payload", func(t *testing.T) {
		var project, role, wave, grant, planned string
		var incomplete bool
		if err := db.QueryRowContext(ctx, `SELECT top_project_name,top_partner_role,top_wave,top_grant_rub::text,top_planned_cofinancing_rub::text,top_incomplete
			FROM entries WHERE id::text=$1`, entry).Scan(&project, &role, &wave, &grant, &planned, &incomplete); err != nil {
			t.Fatal(err)
		}
		if project != "Цифровая кафедра" || role != "anchor" || wave != "2" || grant != "1500000.50" || planned != "300000.00" || incomplete {
			t.Fatalf("паспорт: %q %q %q %q %q %v", project, role, wave, grant, planned, incomplete)
		}
		if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"program_name":""}' WHERE id::text=$1`, entry); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT top_incomplete FROM entries WHERE id::text=$1`, entry).Scan(&incomplete); err != nil || !incomplete {
			t.Fatalf("без программы паспорт неполон: %v %v", incomplete, err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"program_name":"ТОП-ИТ"}' WHERE id::text=$1`, entry); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Вид 4 только для ВО, у других видов паспорта нет", func(t *testing.T) {
		if _, err := insert(own, "top_it", "kolledj", `{"project_name":"П","program_name":"П"}`); err == nil {
			t.Fatal("новая запись Вида 4 по колледжу должна отвергаться БД")
		}
		other, err := insert(own, "teachers", "vuz", `{"project_name":"не топ","grant_amount_rub":"5"}`)
		if err != nil {
			t.Fatal(err)
		}
		var project sqlNull
		if err := db.QueryRowContext(ctx, `SELECT top_project_name FROM entries WHERE id::text=$1`, other).Scan(&project); err != nil || project.Valid {
			t.Fatalf("колонки паспорта заполняются только у Вида 4: %+v %v", project, err)
		}
	})

	author := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
	hr := middleware.AuthUser{ID: own.admin, Role: models.RoleHRSpecialist, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
	foreignAdmin := middleware.AuthUser{ID: foreign.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &foreign.company}
	post := func(u middleware.AuthUser, entryID, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/entries/"+entryID+"/top-items", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handlers.Create(rec, req, u, entryID)
		return rec
	}
	const support = `{"kind":"support","title":"Сервер","support_kind":"equipment","act_reference":"Акт 12","act_date":"2026-05-01","balance_value_rub":500000,"confirmed_value_rub":"450000.50"}`
	const scholarship = `{"kind":"scholarship","title":"Стипендия","student_name":"Иванов Иван","group_name":"ИВТ-31","course":3,"period_start":"2026-09-01","period_end":"2027-01-31","amount_rub":120000,"criterion":"Средний балл","donor_name":"ООО Пример"}`
	const caseBody = `{"kind":"case","title":"Кейс","implementation_org":"ООО Пример","implementation_status":"implemented","implemented_on":"2026-06-15","description":"Внедрено"}`

	var supportID string
	t.Run("права и граница арендатора", func(t *testing.T) {
		if rec := post(hr, entry, support); rec.Code != 403 {
			t.Fatalf("кадровая служба не правит составляющие Вида 4: %d", rec.Code)
		}
		if rec := post(foreignAdmin, entry, support); rec.Code != 403 && rec.Code != 404 {
			t.Fatalf("чужой арендатор: %d", rec.Code)
		}
		rec := post(author, entry, support)
		if rec.Code != 201 {
			t.Fatalf("автор добавляет поддержку: %d %s", rec.Code, rec.Body.String())
		}
		var item topit.Item
		if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil || item.ConfirmedValueRub == nil || item.ConfirmedValueRub.String() != "450000.50" {
			t.Fatalf("строка поддержки: %+v %v", item, err)
		}
		supportID = item.ID
	})

	t.Run("строки других видов и сводка", func(t *testing.T) {
		for _, body := range []string{scholarship, scholarship, caseBody} {
			if rec := post(author, entry, body); rec.Code != 201 {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		}
		rec := httptest.NewRecorder()
		handlers.List(rec, httptest.NewRequest("GET", "/api/entries/"+entry+"/top-items", nil), author, entry)
		var body struct {
			Items   []topit.Item  `json:"items"`
			Summary topit.Summary `json:"summary"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Items) != 4 {
			t.Fatalf("список: %d %s", rec.Code, rec.Body.String())
		}
		if s := body.Summary; s.Supports != 1 || s.ConfirmedSupportRub.String() != "450000.50" || s.Scholarships != 2 ||
			s.ScholarshipsTotalRub.String() != "240000.00" || s.Cases != 1 || s.ImplementedCases != 1 {
			t.Fatalf("сводка: %+v", s)
		}
	})

	t.Run("проверки вида и записи", func(t *testing.T) {
		for name, body := range map[string]string{
			"стипендия без донора":       strings.Replace(scholarship, `,"donor_name":"ООО Пример"`, "", 1),
			"подтверждённая выше оценки": strings.Replace(support, `"confirmed_value_rub":"450000.50"`, `"confirmed_value_rub":900000`, 1),
			"чужое поле":                 strings.Replace(caseBody, `"kind":"case"`, `"kind":"case","student_name":"Иванов"`, 1),
			"неизвестный вид":            strings.Replace(caseBody, `"case"`, `"prize"`, 1),
			"неизвестное поле":           strings.Replace(caseBody, `"kind"`, `"unknown":1,"kind"`, 1),
		} {
			if rec := post(author, entry, body); rec.Code != 400 {
				t.Errorf("%s: ожидался 400, получено %d %s", name, rec.Code, rec.Body.String())
			}
		}
		notTop, _ := insert(own, "teachers", "vuz", `{}`)
		if rec := post(author, notTop, support); rec.Code != 400 {
			t.Fatalf("составляющие только у Вида 4: %d", rec.Code)
		}
	})

	t.Run("документы только своей записи", func(t *testing.T) {
		var foreignEntry, attachment string
		foreignEntry, _ = insert(foreign, "top_it", "vuz", `{"project_name":"П","program_name":"П"}`)
		if err := db.QueryRowContext(ctx, `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at,document_type,content_sha256)
			VALUES($1,'a.pdf','x/a.bin','application/pdf',10,$2,now()+interval '1 day','other',repeat('b',64)) RETURNING id::text`, foreignEntry, foreign.admin).Scan(&attachment); err != nil {
			t.Fatal(err)
		}
		if rec := post(author, entry, strings.Replace(caseBody, `"description"`, `"document_ids":["`+attachment+`"],"description"`, 1)); rec.Code != 400 {
			t.Fatalf("документ чужой записи: %d %s", rec.Code, rec.Body.String())
		}
		var own2 string
		if err := db.QueryRowContext(ctx, `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at,document_type,content_sha256)
			VALUES($1,'b.pdf','x/b.bin','application/pdf',10,$2,now()+interval '1 day','other',repeat('c',64)) RETURNING id::text`, entry, own.admin).Scan(&own2); err != nil {
			t.Fatal(err)
		}
		rec := post(author, entry, strings.Replace(caseBody, `"description"`, `"document_ids":["`+own2+`"],"description"`, 1))
		var item topit.Item
		if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &item) != nil || len(item.DocumentIDs) != 1 || item.DocumentIDs[0] != own2 {
			t.Fatalf("документ своей записи: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("изменение и удаление", func(t *testing.T) {
		put := func(u middleware.AuthUser, id, body string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("PUT", "/api/top-items/"+id, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			handlers.Update(rec, req, u, id)
			return rec
		}
		if rec := put(author, supportID, strings.Replace(support, `"kind":"support"`, `"kind":"case"`, 1)); rec.Code != 400 {
			t.Fatalf("вид не меняется: %d", rec.Code)
		}
		if rec := put(foreignAdmin, supportID, support); rec.Code != 403 && rec.Code != 404 {
			t.Fatalf("чужой арендатор: %d", rec.Code)
		}
		if rec := put(author, supportID, strings.Replace(support, "450000.50", "460000", 1)); rec.Code != 200 || !strings.Contains(rec.Body.String(), "460000.00") {
			t.Fatalf("изменение: %d %s", rec.Code, rec.Body.String())
		}
		del := func(u middleware.AuthUser) int {
			rec := httptest.NewRecorder()
			handlers.Delete(rec, httptest.NewRequest("DELETE", "/api/top-items/"+supportID, nil), u, supportID)
			return rec.Code
		}
		if del(hr) != 403 || del(author) != 200 || del(author) != 404 {
			t.Fatal("удаляет автор, повторное удаление — 404, кадровая служба — 403")
		}
		var audited int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='top_program_item' AND entity_id=$1`, supportID).Scan(&audited); err != nil || audited != 3 {
			t.Fatalf("создание, изменение и удаление должны быть в аудите: %d %v", audited, err)
		}
	})

	t.Run("БД сама держит правила вида", func(t *testing.T) {
		for name, query := range map[string]string{
			"стипендия без периода":  `INSERT INTO top_program_items(entry_id,kind,title,student_name,group_name,course,amount_rub,criterion,donor_name) VALUES($1,'scholarship','С','Иванов','Г',1,5,'к','д')`,
			"поддержка без акта":     `INSERT INTO top_program_items(entry_id,kind,title,support_kind,balance_value_rub) VALUES($1,'support','П','equipment',5)`,
			"внедрён без даты":       `INSERT INTO top_program_items(entry_id,kind,title,implementation_org,implementation_status,description) VALUES($1,'case','К','о','implemented','д')`,
			"поле стипендии у кейса": `INSERT INTO top_program_items(entry_id,kind,title,implementation_org,implementation_status,description,course) VALUES($1,'case','К','о','proposed','д',2)`,
		} {
			if _, err := db.ExecContext(ctx, query, entry); err == nil {
				t.Errorf("%s: БД должна отвергнуть строку", name)
			}
		}
	})
}

type sqlNull struct {
	String string
	Valid  bool
}

func (n *sqlNull) Scan(value interface{}) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	n.Valid = true
	n.String = string(value.([]byte))
	return nil
}
