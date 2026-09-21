package featureflags

import (
	"strings"
	"testing"
)

func TestParseIsExplicitAndFailClosed(t *testing.T) {
	set, err := Parse("teachers, reporting_v44,teachers")
	if err != nil {
		t.Fatal(err)
	}
	if !set.Enabled(Teachers) || !set.Enabled(ReportingV44) || set.Enabled(Schools) {
		t.Fatalf("unexpected set: %#v", set.Snapshot())
	}

	if _, err := Parse("teachers,typo"); err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("unknown flag must fail validation, got %v", err)
	}
}

func TestParseAllAndSnapshotIsolation(t *testing.T) {
	set, err := Parse("all")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	if len(snapshot) != len(Names()) {
		t.Fatalf("got %d flags, want %d", len(snapshot), len(Names()))
	}
	for name, enabled := range snapshot {
		if !enabled {
			t.Fatalf("%s is disabled after all", name)
		}
		snapshot[name] = false
		if !set.Enabled(Name(name)) {
			t.Fatalf("snapshot mutated the set for %s", name)
		}
	}
}
