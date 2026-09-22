// Package compliance evaluates document completeness without allowing a
// manual checkbox to hide an objective regulatory gap.
package compliance

import (
	"fmt"
	"strconv"
	"strings"
)

const RulesetVersion = "mincifry-270-2026.1"

type Check struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Blocking bool   `json:"blocking"`
}

type Result struct {
	State          string   `json:"state"` // green|yellow|red
	Ready          bool     `json:"ready"`
	Eligible       bool     `json:"eligible"`
	RulesetVersion string   `json:"ruleset_version"`
	Checks         []Check  `json:"checks"`
	Blocking       []string `json:"blocking_reasons"`
	Warnings       []string `json:"warnings"`
}

var documentLabels = map[string]string{
	"other":                     "Прочий документ",
	"employment_contract":       "Трудовой договор / ГПХ",
	"organization_agreement":    "Договор с образовательной организацией",
	"individual_plan":           "Индивидуальный план",
	"appointment_order":         "Приказ о назначении / допуске",
	"program_project":           "Проект программы",
	"expert_conclusion":         "Экспертное заключение",
	"academic_council_protocol": "Решение учёного совета",
	"internship_agreement":      "Договор о стажировке",
	"practice_agreement":        "Договор о практической подготовке",
	"labor_contract":            "Трудовой договор со студентом",
	"mentor_order":              "Приказ о наставнике",
	"individual_program":        "Индивидуальная программа / табель",
	"incoming_certificate":      "Входящая справка",
	"outgoing_certificate":      "Итоговая справка",
	"top_agreement":             "Договор по ТОП-ИТ / ТОП-ИИ",
	"payment_order":             "Платёжное поручение",
	"spending_act":              "Отчёт / акт о фактическом расходовании",
	"ano_letter":                "Письмо-согласование АНО АЦ",
	"school_agreement":          "Соглашение со школой / РОИВ",
	"participant_groups":        "Реестр групп участников",
	"acceptance_act":            "Акт приёмки",
	"digital_trace":             "Цифровой след ФГИС «Моя школа»",
	"ministry_decision":         "Решение Минцифры и поручение",
	"expense_evidence":          "Первичные документы расходов",
	"auditor_report":            "Аудиторское заключение",
}

func DocumentTypes() map[string]string {
	result := make(map[string]string, len(documentLabels))
	for code, label := range documentLabels {
		result[code] = label
	}
	return result
}

func ValidDocumentType(code string) bool { _, ok := documentLabels[code]; return ok }

func Evaluate(category, period string, payload map[string]interface{}, documentTypes []string) Result {
	result := Result{State: "green", Ready: true, Eligible: true, RulesetVersion: RulesetVersion, Checks: []Check{}, Blocking: []string{}, Warnings: []string{}}
	docs := map[string]bool{}
	rejectedDocs := map[string]bool{}
	pendingReview := false
	for _, value := range documentTypes {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) == 2 && parts[1] == "rejected" {
			rejectedDocs[parts[0]] = true
			continue
		}
		docs[parts[0]] = true
		if len(parts) == 2 && parts[1] == "pending" {
			pendingReview = true
		}
	}
	require := func(code, label string, complete, blocking bool) {
		result.Checks = append(result.Checks, Check{Code: code, Label: label, Complete: complete, Blocking: blocking})
		if complete {
			return
		}
		if blocking {
			result.Blocking = append(result.Blocking, label)
		} else {
			result.Warnings = append(result.Warnings, label)
		}
	}
	has := func(key string) bool {
		return strings.TrimSpace(fmt.Sprint(payload[key])) != "" && fmt.Sprint(payload[key]) != "<nil>"
	}
	hasDoc := func(code string, refs ...string) bool {
		if docs[code] {
			return true
		}
		// An explicit legal rejection wins over a legacy free-text reference;
		// otherwise the same rejected file could still make a row appear ready.
		if rejectedDocs[code] {
			return false
		}
		for _, ref := range refs {
			if has(ref) {
				return true
			}
		}
		return false
	}
	num := func(key string) float64 {
		value, _ := strconv.ParseFloat(strings.ReplaceAll(fmt.Sprint(payload[key]), ",", "."), 64)
		return value
	}

	// Plan rows may legitimately precede final evidence. Their data quality is
	// still checked, while completion documents become warnings rather than a
	// false assertion that the event has already happened.
	blockDocuments := period == "fact"
	switch category {
	case "teachers":
		// Legacy rows keep their former inline checks. New rows linked to the
		// typed directory additionally require administrator verification.
		if has("staff_member_id") {
			require("staff_profile", "Профиль сотрудника подтверждён администратором", fmt.Sprint(payload["staff_member_verified"]) == "true", true)
		}
		// Матрица рисков ТЗ (§5.1, Вид 1): «Дисциплина не определена» —
		// красная зона наравне с отсутствием договора и трудоустройства.
		require("discipline", "Дисциплина согласована с образовательной организацией", has("course_name"), true)
		require("it_experience", "ИТ-стаж не менее 365 дней за последние 5 лет", num("it_experience_days") >= 365, true)
		require("okz", "Проверенный код ОКЗ сотрудника", has("okz_code"), true)
		require("employment_contract", "Трудовой договор или ГПХ", hasDoc("employment_contract", "employment_contract_reference"), blockDocuments)
		require("appointment_order", "Приказ о допуске к преподаванию", hasDoc("appointment_order", "appointment_order_reference"), false)
		require("individual_plan", "Индивидуальный план и расписание", hasDoc("individual_plan", "individual_plan_reference") && has("class_schedule"), false)
	case "ood_rpd":
		require("program_project", "Проект ООП/РПД", hasDoc("program_project", "project_document_reference"), blockDocuments)
		if fmt.Sprint(payload["activity_type"]) == "expertise" {
			require("expert", "Назначенный эксперт", has("expert_full_name"), true)
		}
		require("expert_conclusion", "Подписанное заключение", hasDoc("expert_conclusion", "expert_conclusion_reference"), false)
		require("academic_council_protocol", "Утверждение образовательной организацией", hasDoc("academic_council_protocol", "approval_reference"), false)
	case "internship":
		require("internship_basis", "Договор о стажировке или трудовой договор", hasDoc("internship_agreement", "internship_agreement_reference") || hasDoc("labor_contract", "labor_contract_number"), blockDocuments)
		require("mentor", "Наставник из справочника", has("mentor_id"), true)
		require("mentor_order", "Приказ о наставнике", hasDoc("mentor_order", "mentor_order_reference"), false)
		require("individual_program", "Индивидуальная программа и фактические часы", hasDoc("individual_program", "individual_program_reference"), false)
		require("certificates", "Входящая и итоговая справки", hasDoc("incoming_certificate", "incoming_certificate_reference") && hasDoc("outgoing_certificate", "outgoing_certificate_reference"), false)
	case "employment_practice":
		require("labor_contract", "Срочный трудовой договор", fmt.Sprint(payload["labor_contract_type"]) == "fixed_term" && has("labor_contract_number") && hasDoc("labor_contract", "labor_contract_reference"), blockDocuments)
		require("practice_agreement", "Договор о практической подготовке", hasDoc("practice_agreement", "practice_agreement_reference"), blockDocuments)
		require("mentor", "Наставник из справочника", has("mentor_id"), true)
		require("mentor_order", "Приказ о наставнике", hasDoc("mentor_order", "mentor_order_reference"), false)
		require("certificates", "Направление, программа и итоговые документы", hasDoc("individual_program", "individual_program_reference") && hasDoc("outgoing_certificate", "outgoing_certificate_reference"), false)
	case "top_it":
		require("top_agreement", "Договор софинансирования", hasDoc("top_agreement", "top_agreement_reference"), blockDocuments)
		require("transfer", "Подтверждение перечисления средств", num("transferred_amount_rub") > 0 && hasDoc("payment_order", "payment_order_reference"), blockDocuments)
		spent := num("actual_spent_amount_rub")
		plan := num("planned_cofinancing_amount_rub")
		require("spending", "Фактически израсходованные средства и акт", spent > 0 && hasDoc("spending_act", "spending_act_reference"), blockDocuments)
		if plan > 0 {
			// Шкала софинансирования ТЗ (§7.5, §5.1): ≥100% — зелёная зона,
			// 70–99.9% — зона внимания, <70% — красная. Без второй ступени
			// недоосвоенное софинансирование ошибочно считалось зелёным.
			// Знаменатель (план X) остаётся предметом открытого ADR-14.
			ratio := spent / plan
			require("progress_70", "Освоено не менее 70% планового софинансирования", ratio >= .7, true)
			require("progress_full", "Софинансирование освоено полностью", ratio >= 1, false)
		}
		require("ano_letter", "Письмо-согласование АНО АЦ", hasDoc("ano_letter", "ano_letter_reference"), false)
	case "it_clubs", "teacher_training":
		require("school_agreement", "Соглашение со школой или РОИВ", hasDoc("school_agreement", "school_agreement_reference"), blockDocuments)
		require("participant_groups", "Реестр участников и групп", hasDoc("participant_groups", "participant_groups_reference"), blockDocuments)
		require("acceptance_act", "Акт приёмки", hasDoc("acceptance_act", "acceptance_act_reference"), false)
	case "edu_content":
		require("school_agreement", "Соглашение со школой или РОИВ", hasDoc("school_agreement", "school_agreement_reference"), blockDocuments)
		require("digital_trace", "Цифровой след ФГИС «Моя школа»", hasDoc("digital_trace", "digital_trace_reference"), blockDocuments)
		// SCH-05: выгрузка подтверждается периодом, числом участников и
		// контрольной суммой файла — иначе «цифровой след» нечем сверить.
		require("digital_trace_manifest", "Период, участники и SHA-256 выгрузки цифрового следа",
			has("digital_trace_period_start") && has("digital_trace_period_end") &&
				num("digital_trace_participants") > 0 && has("digital_trace_sha256"), blockDocuments)
		require("acceptance_act", "Акт приёмки доступа", hasDoc("acceptance_act", "acceptance_act_reference"), false)
	case "minc_decision":
		require("ministry_decision", "Решение Минцифры и исходное поручение", hasDoc("ministry_decision", "decision_reference"), blockDocuments)
		require("expense_evidence", "Акты, платежи и первичные документы", hasDoc("expense_evidence", "expense_evidence_reference"), false)
	}
	if pendingReview {
		require("document_review", "Документы ожидают юридической проверки", false, false)
	}
	if len(result.Blocking) > 0 {
		result.State, result.Ready, result.Eligible = "red", false, false
	} else if len(result.Warnings) > 0 {
		result.State, result.Ready, result.Eligible = "yellow", false, false
	}
	return result
}
