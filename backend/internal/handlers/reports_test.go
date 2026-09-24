package handlers

import "testing"

func TestReportEntryStatusPreservesDraftAndEligibility(t *testing.T) {
	tests := []struct {
		name string
		row  reportEntryRow
		want string
	}{
		{name: "черновик", row: reportEntryRow{ReportStatus: "draft"}, want: "Черновик (не допущено к зачёту)"},
		{name: "готово", row: reportEntryRow{ReportStatus: "ready"}, want: "Готово (не допущено к зачёту)"},
		{name: "утверждено и допущено", row: reportEntryRow{ReportStatus: "approved", Eligible: true}, want: "Утверждено"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportEntryStatus(tc.row); got != tc.want {
				t.Fatalf("reportEntryStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}
