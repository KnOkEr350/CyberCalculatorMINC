package activityprojection

import (
	"testing"

	"cybercalc/internal/money"
)

func TestAggregateContributionsKeepsAmountAxesAndDimensionsSeparate(t *testing.T) {
	items := []Contribution{
		{
			PartnerID: "partner-b", CategoryCode: "teachers", Audience: "vuz", Period: "plan", Units: 2,
			PlanAmount: money.Amount(10_000), Eligibility: Assessment{Passed: true},
		},
		{
			PartnerID: "partner-b", CategoryCode: "teachers", Audience: "vuz", Period: "fact", Units: 3,
			FactAmount: money.Amount(9_000), CalculatedFact: money.Amount(8_500), ConfirmedFact: money.Amount(8_000),
			CountedAmount: money.Amount(7_500), Eligibility: Assessment{Passed: true},
		},
		{
			PartnerID: "partner-a", CategoryCode: "ood_rpd", Audience: "kolledj", Period: "fact", Units: 1,
			FactAmount: money.Amount(4_000), CalculatedFact: money.Amount(4_000), Eligibility: Assessment{Passed: false},
		},
	}

	groups, err := AggregateContributions(items, Grouping{Partner: true, Category: true, Audience: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].PartnerID != "partner-a" || groups[1].PartnerID != "partner-b" {
		t.Fatalf("groups are not deterministic: %#v", groups)
	}
	eligible := groups[1]
	if eligible.PlanAmount != 10_000 || eligible.FactAmount != 9_000 || eligible.CalculatedFactAmount != 8_500 ||
		eligible.ConfirmedFactAmount != 8_000 || eligible.CountedFactAmount != 7_500 {
		t.Fatalf("amount axes were conflated: %#v", eligible)
	}
	if eligible.PlanEntries != 1 || eligible.FactEntries != 1 || eligible.PlanUnits != 2 || eligible.FactUnits != 3 {
		t.Fatalf("entry/unit aggregation is wrong: %#v", eligible)
	}
	if groups[0].EligibleFactAmount != 0 || groups[0].IncompleteEntries != 1 {
		t.Fatalf("eligibility aggregation is wrong: %#v", groups[0])
	}
}

func TestSummarizeEmptyAndRejectsUnknownPeriod(t *testing.T) {
	if summary, err := Summarize(nil); err != nil || summary != (Aggregate{}) {
		t.Fatalf("empty summary = %#v, %v", summary, err)
	}
	if _, err := Summarize([]Contribution{{Period: "future"}}); err == nil {
		t.Fatal("unknown period must be rejected")
	}
}
