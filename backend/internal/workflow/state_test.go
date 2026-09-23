package workflow

import "testing"

func TestRegulatoryTransitions(t *testing.T) {
	allowed := [][2]Status{{Draft, Sent}, {Sent, InReview}, {InReview, Rework}, {Rework, Resubmitted},
		{Resubmitted, Approved}, {Approved, Disputed}, {Disputed, InReview}, {Sent, DefaultApproved}}
	for _, transition := range allowed {
		if err := ValidateTransition(transition[0], transition[1]); err != nil {
			t.Errorf("%s -> %s: %v", transition[0], transition[1], err)
		}
	}
	for _, transition := range [][2]Status{{Draft, Approved}, {Rework, Approved}, {Approved, Draft}, {Sent, Resubmitted}} {
		if err := ValidateTransition(transition[0], transition[1]); err == nil {
			t.Errorf("%s -> %s must fail", transition[0], transition[1])
		}
	}
}
