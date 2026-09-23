// Package activityprojection defines the stable read contract shared by
// domain modules, dashboards, reports, snapshots and package exchange.
package activityprojection

import (
	"context"
	"fmt"
	"sort"
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
	AgreementID  string
	CategoryCode string
	Audience     string
}

// Grouping selects stable projection dimensions without exposing storage
// details to analytics consumers. A zero value produces one overall total.
type Grouping struct {
	Partner   bool
	Agreement bool
	Category  bool
	Audience  bool
}

// Aggregate is the shared plan/fact/delta read model used by dashboards and
// reports. Amount axes stay separate so a consumer cannot accidentally treat
// a calculated, confirmed or counted amount as the raw fact.
type Aggregate struct {
	PartnerID            string
	AgreementID          string
	CategoryCode         string
	Audience             string
	PlanEntries          int
	FactEntries          int
	PlanUnits            float64
	FactUnits            float64
	PlanAmount           money.Amount
	FactAmount           money.Amount
	CalculatedFactAmount money.Amount
	ConfirmedFactAmount  money.Amount
	CountedFactAmount    money.Amount
	EligiblePlanAmount   money.Amount
	EligibleFactAmount   money.Amount
	IncompleteEntries    int
}

// Reader is the inter-module API. Typed activity modules can replace the
// current SQL compatibility adapter independently, without changing users of
// the projection.
type Reader interface {
	List(context.Context, Filter) ([]Contribution, error)
}

// Summarize builds one overall aggregate. Empty input returns a zero value.
func Summarize(items []Contribution) (Aggregate, error) {
	groups, err := AggregateContributions(items, Grouping{})
	if err != nil || len(groups) == 0 {
		return Aggregate{}, err
	}
	return groups[0], nil
}

// AggregateContributions groups contributions deterministically and performs
// every monetary addition through money.Add so overflow is never silent.
func AggregateContributions(items []Contribution, grouping Grouping) ([]Aggregate, error) {
	groups := make(map[string]*Aggregate)
	for _, item := range items {
		key, aggregate := aggregateKey(item, grouping)
		current := groups[key]
		if current == nil {
			current = &aggregate
			groups[key] = current
		}
		var err error
		switch item.Period {
		case "plan":
			current.PlanEntries++
			current.PlanUnits += item.Units
			if current.PlanAmount, err = money.Add(current.PlanAmount, item.PlanAmount); err != nil {
				return nil, err
			}
			if item.Eligibility.Passed {
				if current.EligiblePlanAmount, err = money.Add(current.EligiblePlanAmount, item.PlanAmount); err != nil {
					return nil, err
				}
			} else {
				current.IncompleteEntries++
			}
		case "fact":
			current.FactEntries++
			current.FactUnits += item.Units
			if current.FactAmount, err = money.Add(current.FactAmount, item.FactAmount); err != nil {
				return nil, err
			}
			if current.CalculatedFactAmount, err = money.Add(current.CalculatedFactAmount, item.CalculatedFact); err != nil {
				return nil, err
			}
			if current.ConfirmedFactAmount, err = money.Add(current.ConfirmedFactAmount, item.ConfirmedFact); err != nil {
				return nil, err
			}
			if current.CountedFactAmount, err = money.Add(current.CountedFactAmount, item.CountedAmount); err != nil {
				return nil, err
			}
			if item.Eligibility.Passed {
				if current.EligibleFactAmount, err = money.Add(current.EligibleFactAmount, item.FactAmount); err != nil {
					return nil, err
				}
			} else {
				current.IncompleteEntries++
			}
		default:
			return nil, fmt.Errorf("activity projection: unsupported contribution period %q", item.Period)
		}
	}

	result := make([]Aggregate, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.PartnerID != right.PartnerID {
			return left.PartnerID < right.PartnerID
		}
		if left.AgreementID != right.AgreementID {
			return left.AgreementID < right.AgreementID
		}
		if left.CategoryCode != right.CategoryCode {
			return left.CategoryCode < right.CategoryCode
		}
		return left.Audience < right.Audience
	})
	return result, nil
}

func aggregateKey(item Contribution, grouping Grouping) (string, Aggregate) {
	parts := make([]string, 0, 4)
	aggregate := Aggregate{}
	if grouping.Partner {
		aggregate.PartnerID = item.PartnerID
		parts = append(parts, item.PartnerID)
	}
	if grouping.Agreement {
		aggregate.AgreementID = item.AgreementID
		parts = append(parts, item.AgreementID)
	}
	if grouping.Category {
		aggregate.CategoryCode = item.CategoryCode
		parts = append(parts, item.CategoryCode)
	}
	if grouping.Audience {
		aggregate.Audience = item.Audience
		parts = append(parts, item.Audience)
	}
	return stringJoin(parts), aggregate
}

func stringJoin(parts []string) string {
	key := ""
	for _, part := range parts {
		key += fmt.Sprintf("%d:%s", len(part), part)
	}
	return key
}
