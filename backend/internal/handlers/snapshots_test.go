package handlers

import (
	"testing"
	"time"
)

func TestMaySnapshotWindowUsesMoscowCalendarDate(t *testing.T) {
	if !maySnapshotAllowed(time.Date(2026, 4, 30, 21, 0, 0, 0, time.UTC), 2026) {
		t.Fatal("00:00 on 1 May Moscow must be inside the snapshot window")
	}
	if maySnapshotAllowed(time.Date(2026, 5, 1, 21, 0, 0, 0, time.UTC), 2026) {
		t.Fatal("00:00 on 2 May Moscow must be outside the snapshot window")
	}
	if maySnapshotAllowed(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), 2025) {
		t.Fatal("a snapshot cannot be sealed for another reporting year")
	}
}
