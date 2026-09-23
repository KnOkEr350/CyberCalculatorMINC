package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/money"
	"cybercalc/internal/platform/activityprojection"
)

type projectionReaderStub struct {
	filter activityprojection.Filter
	items  []activityprojection.Contribution
}

func (s *projectionReaderStub) List(_ context.Context, filter activityprojection.Filter) ([]activityprojection.Contribution, error) {
	s.filter = filter
	return s.items, nil
}

func TestDashboardRiskBreakdownUsesActivityProjection(t *testing.T) {
	reader := &projectionReaderStub{items: []activityprojection.Contribution{
		{Period: "fact", FactAmount: money.Amount(10_000), Risk: activityprojection.Assessment{State: "green"}},
		{Period: "fact", FactAmount: money.Amount(20_000), Risk: activityprojection.Assessment{State: "yellow"}},
		{Period: "fact", FactAmount: money.Amount(30_000), Risk: activityprojection.Assessment{State: "unknown"}},
	}}
	handler := DashboardHandlers{Projection: reader}
	request := httptest.NewRequest("GET", "/api/dashboard", nil)

	buckets, err := handler.riskBreakdown(request, 2026, "partner-1", "tenant-1", "teachers", "vuz")
	if err != nil {
		t.Fatal(err)
	}
	wantFilter := (activityprojection.Filter{
		ReportYear: 2026, Period: "fact", TenantID: "tenant-1", PartnerID: "partner-1",
		CategoryCode: "teachers", Audience: "vuz",
	})
	if reader.filter != wantFilter {
		t.Fatalf("filter = %#v, want %#v", reader.filter, wantFilter)
	}
	if buckets["green"].EntryCount != 1 || buckets["green"].AmountRub != money.Amount(10_000) {
		t.Fatalf("green bucket = %#v", buckets["green"])
	}
	if buckets["yellow"].EntryCount != 1 || buckets["yellow"].AmountRub != money.Amount(20_000) {
		t.Fatalf("yellow bucket = %#v", buckets["yellow"])
	}
	if buckets["red"].EntryCount != 1 || buckets["red"].AmountRub != money.Amount(30_000) {
		t.Fatalf("red bucket = %#v", buckets["red"])
	}
}

func TestDashboardBreakdownUsesProjectionAmountsUnitsAndObligations(t *testing.T) {
	items := []activityprojection.Contribution{
		{CategoryCode: "teachers", Audience: "vuz", Period: "plan", Units: 2, PlanAmount: money.Amount(30_000)},
		{CategoryCode: "teachers", Audience: "vuz", Period: "fact", Units: 3, FactAmount: money.Amount(20_000)},
		{CategoryCode: "teachers", Audience: "kolledj", Period: "fact", Units: 1, FactAmount: money.Amount(10_000)},
	}
	rows, err := breakdownFromProjection(items, "fact", map[string]string{"teachers": "mandatory"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	byAudience := map[string]categoryBreakdown{}
	for _, row := range rows {
		byAudience[row.Audience] = row
	}
	if row := byAudience["vuz"]; row.EntryCount != 1 || row.UnitCount != 3 || row.AmountRub != 20_000 || row.Obligation != "mandatory" || row.SharePercent != 66.67 {
		t.Fatalf("unexpected university breakdown: %#v", row)
	}
	if row := byAudience["kolledj"]; row.EntryCount != 1 || row.AmountRub != 10_000 || row.Obligation != "variable" || row.SharePercent != 33.33 {
		t.Fatalf("unexpected college breakdown: %#v", row)
	}
}

func TestRiskBreakdownFromProjectionIgnoresPlanRows(t *testing.T) {
	items := []activityprojection.Contribution{
		{Period: "plan", PlanAmount: 90_000, Risk: activityprojection.Assessment{State: "green"}},
		{Period: "fact", FactAmount: 40_000, Risk: activityprojection.Assessment{State: "green"}},
	}
	buckets, err := riskBreakdownFromProjection(items)
	if err != nil {
		t.Fatal(err)
	}
	if buckets["green"].EntryCount != 1 || buckets["green"].AmountRub != 40_000 {
		t.Fatalf("plan row leaked into risk buckets: %#v", buckets)
	}
}
