// Package activityprojection defines the stable read contract shared by
// domain modules, dashboards, reports, snapshots and package exchange.
package activityprojection

import (
	"context"
	"time"

	"cybercalc/internal/money"
)

type Assessment struct {
	State   string   `json:"state"`
	Passed  bool     `json:"passed"`
	Reasons []string `json:"reasons"`
}

// Contribution is the accounting projection of one activity. It deliberately
// contains no legacy payload: consumers depend on stable amounts, dimensions
// and independently evaluated compliance axes instead of JSON storage details.
type Contribution struct {
	ActivityID     string       `json:"activity_id"`
	TenantID       string       `json:"tenant_id"`
	PartnerID      string       `json:"partner_id,omitempty"`
	AgreementID    string       `json:"agreement_id,omitempty"`
	CategoryCode   string       `json:"category_code"`
	Audience       string       `json:"audience"`
	ReportYear     int          `json:"report_year"`
	Period         string       `json:"period"`
	Units          float64      `json:"units"`
	PlanAmount     money.Amount `json:"plan_amount_rub"`
	FactAmount     money.Amount `json:"fact_amount_rub"`
	CalculatedFact money.Amount `json:"calculated_fact_rub"`
	ConfirmedFact  money.Amount `json:"confirmed_fact_rub"`
	CountedAmount  money.Amount `json:"counted_amount_rub"`
	Readiness      Assessment   `json:"readiness"`
	Eligibility    Assessment   `json:"eligibility"`
	Approval       Assessment   `json:"approval"`
	LegalDispute   Assessment   `json:"legal_dispute"`
	Risk           Assessment   `json:"risk"`
	RulesetVersion string       `json:"ruleset_version"`
	EvaluatedAt    time.Time    `json:"evaluated_at"`
}

type Filter struct {
	ReportYear   int
	Period       string
	TenantID     string
	PartnerID    string
	CategoryCode string
	Audience     string
}

// Reader is the inter-module API. Typed activity modules can replace the
// current SQL compatibility adapter independently, without changing users of
// the projection.
type Reader interface {
	List(context.Context, Filter) ([]Contribution, error)
}
