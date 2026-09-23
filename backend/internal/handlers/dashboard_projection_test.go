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
		{FactAmount: money.Amount(10_000), Risk: activityprojection.Assessment{State: "green"}},
		{FactAmount: money.Amount(20_000), Risk: activityprojection.Assessment{State: "yellow"}},
		{FactAmount: money.Amount(30_000), Risk: activityprojection.Assessment{State: "unknown"}},
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
