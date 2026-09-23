package repository

import (
	"testing"
	"time"

	"cybercalc/internal/money"
)

func TestProjectLegacyActivitySeparatesComplianceAxesAndAmounts(t *testing.T) {
	evaluatedAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	row := legacyActivityRow{
		activityID:      "activity-1",
		tenantID:        "tenant-1",
		partnerID:       "partner-1",
		agreementID:     "agreement-1",
		categoryCode:    "edu_content",
		audience:        "school",
		reportYear:      2026,
		period:          "fact",
		amount:          money.Amount(125_000),
		formulaAmount:   money.Amount(120_000),
		payload:         []byte(`{"student_platform_months":"10","teacher_platform_months":2,"digital_trace_period_start":"2026-01-01","digital_trace_period_end":"2026-04-30","digital_trace_participants":12,"digital_trace_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
		accountEligible: true,
		reportStatus:    "approved",
		documents:       []string{"school_agreement:approved", "digital_trace:approved", "acceptance_act:approved"},
	}

	projection, err := projectLegacyActivity(row, evaluatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Units != 12 {
		t.Fatalf("units = %v, want 12", projection.Units)
	}
	if projection.PlanAmount != 0 || projection.FactAmount != row.amount || projection.CalculatedFact != row.formulaAmount {
		t.Fatalf("amount separation is wrong: %#v", projection)
	}
	if projection.ConfirmedFact != row.amount || projection.CountedAmount != row.amount {
		t.Fatalf("confirmed/countable amounts are wrong: %#v", projection)
	}
	if !projection.Readiness.Passed || !projection.Eligibility.Passed || !projection.Approval.Passed || !projection.Risk.Passed {
		t.Fatalf("independent compliance axes are wrong: %#v", projection)
	}
	if projection.EvaluatedAt != evaluatedAt {
		t.Fatalf("evaluated_at = %v, want %v", projection.EvaluatedAt, evaluatedAt)
	}
}

func TestProjectLegacyActivityKeepsUnapprovedFactOutOfCountedAmount(t *testing.T) {
	row := legacyActivityRow{
		activityID:    "activity-2",
		categoryCode:  "minc_decision",
		audience:      "vuz",
		reportYear:    2026,
		period:        "fact",
		amount:        money.Amount(50_000),
		formulaAmount: money.Amount(50_000),
		payload:       []byte(`{"actual_value":1}`),
		reportStatus:  "draft",
		documents:     []string{"ministry_decision:approved", "expense_evidence:approved"},
	}

	projection, err := projectLegacyActivity(row, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if projection.CountedAmount != 0 || projection.ConfirmedFact != row.amount {
		t.Fatalf("confirmation and accounting approval were conflated: %#v", projection)
	}
	if projection.Risk.State != "yellow" || projection.LegalDispute.State != "clear" {
		t.Fatalf("unexpected risk/dispute projection: %#v", projection)
	}
	if projection.Eligibility.Passed {
		t.Fatalf("an entry outside the accounting boundary must stay ineligible: %#v", projection)
	}
}
