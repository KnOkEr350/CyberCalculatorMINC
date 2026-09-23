package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/compliance"
	"cybercalc/internal/money"
	"cybercalc/internal/platform/activityprojection"
	"github.com/lib/pq"
)

type ActivityProjection struct {
	db  *sql.DB
	now func() time.Time
}

func NewActivityProjection(db *sql.DB) *ActivityProjection {
	return &ActivityProjection{db: db, now: time.Now}
}

type legacyActivityRow struct {
	activityID      string
	tenantID        string
	partnerID       string
	agreementID     string
	categoryCode    string
	audience        string
	reportYear      int
	period          string
	amount          money.Amount
	formulaAmount   money.Amount
	payload         []byte
	accountEligible bool
	reportStatus    string
	documents       []string
}

func (r *ActivityProjection) List(ctx context.Context, filter activityprojection.Filter) ([]activityprojection.Contribution, error) {
	if r.db == nil {
		return nil, fmt.Errorf("activity projection: nil database")
	}
	if filter.ReportYear != 0 && (filter.ReportYear < 2000 || filter.ReportYear > 2100) {
		return nil, fmt.Errorf("activity projection: report year is outside 2000-2100")
	}
	if filter.Period != "" && filter.Period != "plan" && filter.Period != "fact" {
		return nil, fmt.Errorf("activity projection: unsupported period %q", filter.Period)
	}

	rows, err := r.db.QueryContext(ctx, `SELECT e.id::text,COALESCE(e.it_company_id::text,''),COALESCE(e.partner_id::text,''),
		COALESCE(e.agreement_id::text,''),e.category_code,e.audience,e.report_year,e.period_type,
		e.amount_rub,e.formula_amount_rub,e.payload,COALESCE(eligibility.eligible,false),
		COALESCE(eligibility.report_status,'draft'),
		ARRAY(SELECT DISTINCT a.document_type||':'||a.review_status FROM attachments a
			WHERE a.entry_id=e.id AND a.retention_expires_at>now())
		FROM entries e LEFT JOIN entry_eligibility eligibility ON eligibility.id=e.id
		WHERE ($1=0 OR e.report_year=$1) AND ($2='' OR e.period_type=$2)
		AND ($3='' OR e.it_company_id::text=$3) AND ($4='' OR e.partner_id::text=$4)
		AND ($5='' OR e.category_code=$5) AND ($6='' OR e.audience=$6)
		ORDER BY e.report_year,e.period_type,e.category_code,e.id`,
		filter.ReportYear, filter.Period, filter.TenantID, filter.PartnerID, filter.CategoryCode, filter.Audience)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]activityprojection.Contribution, 0)
	for rows.Next() {
		var row legacyActivityRow
		var documents pq.StringArray
		if err := rows.Scan(
			&row.activityID, &row.tenantID, &row.partnerID, &row.agreementID,
			&row.categoryCode, &row.audience, &row.reportYear, &row.period,
			&row.amount, &row.formulaAmount, &row.payload, &row.accountEligible,
			&row.reportStatus, &documents,
		); err != nil {
			return nil, err
		}
		row.documents = []string(documents)
		projection, err := projectLegacyActivity(row, r.now().UTC())
		if err != nil {
			return nil, fmt.Errorf("activity projection %s: %w", row.activityID, err)
		}
		result = append(result, projection)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func projectLegacyActivity(row legacyActivityRow, evaluatedAt time.Time) (activityprojection.Contribution, error) {
	payload := make(map[string]interface{})
	decoder := json.NewDecoder(bytes.NewReader(row.payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return activityprojection.Contribution{}, err
	}
	readiness := compliance.Evaluate(row.categoryCode, row.period, payload, row.documents)
	approved := row.reportStatus == "approved"
	eligible := readiness.Eligible
	riskState := readiness.State
	if riskState == "green" && !row.accountEligible {
		riskState = "yellow"
	}

	reasons := append([]string(nil), readiness.Blocking...)
	reasons = append(reasons, readiness.Warnings...)
	if !row.accountEligible {
		reasons = append(reasons, "Мероприятие не прошло нормативное согласование")
	}

	projection := activityprojection.Contribution{
		ActivityID:     row.activityID,
		TenantID:       row.tenantID,
		PartnerID:      row.partnerID,
		AgreementID:    row.agreementID,
		CategoryCode:   row.categoryCode,
		Audience:       row.audience,
		ReportYear:     row.reportYear,
		Period:         row.period,
		Units:          activityUnits(row.categoryCode, payload),
		Readiness:      activityprojection.Assessment{State: readiness.State, Passed: readiness.Ready, Reasons: append(append([]string(nil), readiness.Blocking...), readiness.Warnings...)},
		Eligibility:    activityprojection.Assessment{State: eligibilityState(eligible), Passed: eligible, Reasons: reasonsIfFalse(eligible, "Мероприятие не допущено к зачёту")},
		Approval:       activityprojection.Assessment{State: row.reportStatus, Passed: approved, Reasons: reasonsIfFalse(approved, "Отчётный комплект не утверждён")},
		LegalDispute:   activityprojection.Assessment{State: "clear", Passed: true, Reasons: []string{}},
		Risk:           activityprojection.Assessment{State: riskState, Passed: riskState == "green", Reasons: reasons},
		RulesetVersion: readiness.RulesetVersion,
		EvaluatedAt:    evaluatedAt,
	}
	if row.period == "plan" {
		projection.PlanAmount = row.amount
	} else {
		projection.FactAmount = row.amount
		projection.CalculatedFact = row.formulaAmount
		if readiness.Ready {
			projection.ConfirmedFact = row.amount
		}
		if readiness.Ready && row.accountEligible && approved {
			projection.CountedAmount = row.amount
		}
	}
	return projection, nil
}

func eligibilityState(eligible bool) string {
	if eligible {
		return "eligible"
	}
	return "ineligible"
}

func reasonsIfFalse(value bool, reason string) []string {
	if value {
		return []string{}
	}
	return []string{reason}
}

func activityUnits(category string, payload map[string]interface{}) float64 {
	switch category {
	case "teacher_training":
		return payloadNumber(payload["trained_teachers_count"])
	case "it_clubs":
		return payloadNumber(payload["developed_programs_count"])
	case "edu_content":
		return payloadNumber(payload["student_platform_months"]) + payloadNumber(payload["teacher_platform_months"])
	default:
		return 1
	}
}

func payloadNumber(value interface{}) float64 {
	switch typed := value.(type) {
	case json.Number:
		number, _ := typed.Float64()
		return number
	case float64:
		return typed
	case string:
		number, _ := strconv.ParseFloat(strings.ReplaceAll(typed, ",", "."), 64)
		return number
	default:
		return 0
	}
}
