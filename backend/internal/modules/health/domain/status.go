// Package domain contains health module values and has no dependency on
// database or HTTP concerns.
package domain

// Status is the public health state returned by probes.
type Status struct {
	Status string `json:"status"`
}

func OK() Status {
	return Status{Status: "ok"}
}
