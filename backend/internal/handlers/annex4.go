package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/xlsx"
)

type annex4Aggregate struct {
	PartnerName, AgreementNo, Audience, CategoryCode, CategoryName string
	PlanVolume, FactVolume, PlanReach, FactReach                   float64
	PlanAmount                                                     money.Amount
}

func numericPayload(raw json.RawMessage) map[string]interface{} {
	result := map[string]interface{}{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&result)
	return result
}

func firstNumber(payload map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if number, err := typed.Float64(); err == nil {
				return number
			}
		case float64:
			return typed
		case string:
			if number, err := strconv.ParseFloat(strings.ReplaceAll(typed, ",", "."), 64); err == nil {
				return number
			}
		}
	}
	return 0
}

func activityMetrics(category string, payload map[string]interface{}) (volume, reach float64, unit string) {
	switch category {
	case "teachers":
		return firstNumber(payload, "academic_hours", "hours"), firstNumber(payload, "students_reach", "students_count", "student_reach"), "академический час"
	case "internship", "employment_practice":
		// INT-06: часы нормализуются единственным хелпером, чтобы объём в
		// отчётности не удваивался при заполненных итогах и помесячной нагрузке.
		return studentHours(payload), 1, "человеко-час"
	case "it_clubs":
		return firstNumber(payload, "academic_hours", "hours"), firstNumber(payload, "participants", "students_count", "teachers_count"), "академический час"
	case "teacher_training":
		reach = firstNumber(payload, "trained_teachers_count")
		return firstNumber(payload, "academic_hours_per_teacher") * reach, reach, "академический час"
	case "edu_content":
		volume = firstNumber(payload, "student_platform_months") + firstNumber(payload, "teacher_platform_months")
		return volume, firstNumber(payload, "participants", "students_count"), "человеко-месяц"
	default:
		volume = firstNumber(payload, "volume", "actual_volume", "programs", "program_count")
		if volume == 0 && payload != nil {
			volume = 1
		}
		return volume, firstNumber(payload, "participants", "students_count", "student_reach"), "мероприятие"
	}
}

func annex4Key(partnerID, category string) string { return partnerID + "\x00" + category }

// annex4Headers — 13 граф годового плана мероприятий (Приложение № 4 к
// Приказу № 270): план на 31 декабря и факт на 1 мая в одной форме.
var annex4Headers = []string{
	"№ п/п", "Наименование ОО / РОИВ", "Реквизиты соглашения", "Уровень образования",
	"Вид мероприятия", "Наименование мероприятия", "Срок исполнения", "Единица измерения",
	"Объём: план на 31 декабря", "Объём: факт на 1 мая",
	"Охват: план на 31 декабря, чел.", "Охват: факт на 1 мая, чел.",
	"Объём средств (план), тыс. руб.",
}

func (h *ReportHandlers) exportAnnex4(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int) {
	companyID := itCompanyScope(u)
	if companyID == "" && (u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin) {
		companyID = strings.TrimSpace(r.URL.Query().Get("it_company_id"))
	}
	if companyID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "для Приложения № 4 укажите ИТ-компанию")
		return
	}
	var frozen []byte
	var snapshotID, snapshotHash string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT id::text,payload_bytes,payload_sha256 FROM report_snapshots WHERE it_company_id::text=$1 AND report_year=$2`, companyID, year).Scan(&snapshotID, &frozen, &snapshotHash); err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusConflict, "Приложение № 4 недоступно: сначала сформируйте неизменяемый снимок факта на 1 мая")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать снимок для Приложения № 4")
		return
	}
	digest := sha256.Sum256(frozen)
	if hex.EncodeToString(digest[:]) != snapshotHash {
		middleware.WriteError(w, 500, "контрольная сумма снимка для Приложения № 4 нарушена")
		return
	}
	var snapshot snapshotPayload
	if err := json.Unmarshal(frozen, &snapshot); err != nil {
		middleware.WriteError(w, 500, "снимок имеет некорректный формат")
		return
	}

	categories := map[string]string{}
	categoryRows, err := h.DB.QueryContext(r.Context(), `SELECT code,name FROM activity_categories ORDER BY code`)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать виды мероприятий")
		return
	}
	for categoryRows.Next() {
		var code, name string
		if categoryRows.Scan(&code, &name) != nil {
			categoryRows.Close()
			middleware.WriteError(w, 500, "не удалось прочитать виды мероприятий")
			return
		}
		categories[code] = name
	}
	categoryRows.Close()

	aggregates := map[string]*annex4Aggregate{}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT COALESCE(e.partner_id::text,''),COALESCE(p.name,''),COALESCE(a.number,''),e.category_code,e.audience,e.payload,e.amount_rub
		FROM entries e LEFT JOIN partners p ON p.id=e.partner_id LEFT JOIN agreements a ON a.id=e.agreement_id
		JOIN entry_eligibility eligibility ON eligibility.id=e.id AND eligibility.eligible
		WHERE e.it_company_id::text=$1 AND e.report_year=$2 AND e.period_type='plan' ORDER BY e.partner_id,e.category_code,e.id`, companyID, year)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать утверждённый план")
		return
	}
	for rows.Next() {
		var partnerID string
		var payload json.RawMessage
		var amount money.Amount
		var partnerName, agreementNo, category, audience string
		if rows.Scan(&partnerID, &partnerName, &agreementNo, &category, &audience, &payload, &amount) != nil {
			rows.Close()
			middleware.WriteError(w, 500, "не удалось прочитать утверждённый план")
			return
		}
		key := annex4Key(partnerID, category)
		item := aggregates[key]
		if item == nil {
			item = &annex4Aggregate{PartnerName: partnerName, AgreementNo: agreementNo, Audience: audience, CategoryCode: category, CategoryName: categories[category]}
			aggregates[key] = item
		}
		volume, reach, _ := activityMetrics(category, numericPayload(payload))
		item.PlanVolume += volume
		item.PlanReach += reach
		if item.PlanAmount, err = money.Add(item.PlanAmount, amount); err != nil {
			rows.Close()
			middleware.WriteError(w, 422, err.Error())
			return
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		middleware.WriteError(w, 500, "не удалось прочитать утверждённый план")
		return
	}
	rows.Close()
	partnerFilter := strings.TrimSpace(r.URL.Query().Get("partner_id"))
	if u.EntityType == models.EntityOrganization && u.Role == models.RoleCurator {
		if u.PartnerID == nil || *u.PartnerID == "" {
			middleware.WriteError(w, http.StatusForbidden, "куратору не назначена образовательная организация")
			return
		}
		partnerFilter = *u.PartnerID
	}
	for _, fact := range snapshot.Entries {
		if !fact.Eligible || fact.Readiness != "green" {
			continue
		}
		partnerID := ""
		if fact.PartnerID != nil {
			partnerID = *fact.PartnerID
		}
		if partnerFilter != "" && partnerID != partnerFilter {
			continue
		}
		key := annex4Key(partnerID, fact.CategoryCode)
		item := aggregates[key]
		if item == nil {
			item = &annex4Aggregate{PartnerName: fact.PartnerName, AgreementNo: fact.AgreementNo, Audience: fact.Audience, CategoryCode: fact.CategoryCode, CategoryName: categories[fact.CategoryCode]}
			aggregates[key] = item
		}
		volume, reach, _ := activityMetrics(fact.CategoryCode, numericPayload(fact.Payload))
		item.FactVolume += volume
		item.FactReach += reach
	}

	keys := make([]string, 0, len(aggregates))
	for key := range aggregates {
		if partnerFilter == "" || strings.HasPrefix(key, partnerFilter+"\x00") {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := aggregates[keys[i]], aggregates[keys[j]]
		if left.PartnerName == right.PartnerName {
			return left.CategoryName < right.CategoryName
		}
		return left.PartnerName < right.PartnerName
	})
	headers := annex4Headers
	data := make([][]interface{}, 0, len(keys)+1)
	for index, key := range keys {
		item := aggregates[key]
		_, _, unit := activityMetrics(item.CategoryCode, nil)
		data = append(data, []interface{}{index + 1, item.PartnerName, item.AgreementNo, officeValue(item.Audience), item.CategoryName, item.CategoryName, "до 31 декабря", unit, item.PlanVolume, item.FactVolume, item.PlanReach, item.FactReach, fmt.Sprintf("%.2f", float64(item.PlanAmount)/1000)})
	}
	if len(data) == 0 {
		middleware.WriteError(w, http.StatusConflict, "для Приложения № 4 нет плановых или зафиксированных фактических данных")
		return
	}
	data = append(data, []interface{}{"Источник факта", "Snapshot " + snapshotID, "SHA-256 " + snapshotHash, "", "", "", "", "", "", "", "", "", ""})
	wb := xlsx.New()
	wb.AddSheet("Приложение № 4", headers, data)
	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать Приложение № 4")
		return
	}
	// Приложение № 4 читает факт исключительно из подписанного снимка на
	// 1 мая (WF-08) — источник фиксируется в реестре (REPORT-11), а не
	// только внутри самого файла, чтобы его можно было найти без скачивания.
	filters := reportFilters("partner_id", partnerFilter, "agreement_id", strings.TrimSpace(r.URL.Query().Get("agreement_id")),
		"snapshot_id", snapshotID, "snapshot_sha256", snapshotHash)
	h.writeGenerated(w, r, u, "annex4", "xlsx", fmt.Sprintf("приложение_4_%d.xlsx", year), companyID, filters, year, body)
}
