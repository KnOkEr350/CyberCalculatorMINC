// Package workflow defines the regulatory process state machine from WF-01.
package workflow

import "fmt"

type Status string

const (
	Draft           Status = "draft"
	Sent            Status = "sent"
	InReview        Status = "in_review"
	Rework          Status = "rework"
	Resubmitted     Status = "resubmitted"
	Approved        Status = "approved"
	DefaultApproved Status = "default_approved"
	Disputed        Status = "disputed"
)

func ValidProcess(value string) bool {
	return value == "agreement" || value == "preliminary" || value == "final"
}

func ValidStatus(value Status) bool {
	switch value {
	case Draft, Sent, InReview, Rework, Resubmitted, Approved, DefaultApproved, Disputed:
		return true
	default:
		return false
	}
}

func ValidateTransition(from, to Status) error {
	allowed := map[Status]map[Status]bool{
		Draft:           {Sent: true},
		Sent:            {InReview: true, Rework: true, DefaultApproved: true, Disputed: true},
		InReview:        {Rework: true, Approved: true, DefaultApproved: true, Disputed: true},
		Rework:          {Resubmitted: true},
		Resubmitted:     {InReview: true, Rework: true, Approved: true, DefaultApproved: true, Disputed: true},
		Approved:        {Disputed: true},
		DefaultApproved: {Disputed: true},
		Disputed:        {InReview: true, Rework: true},
	}
	if !ValidStatus(from) || !ValidStatus(to) || !allowed[from][to] {
		return fmt.Errorf("переход %s → %s запрещён", from, to)
	}
	return nil
}
