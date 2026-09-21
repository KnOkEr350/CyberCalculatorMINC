// Package service implements health use cases and depends only on the module's
// domain and repository contracts plus shared application errors.
package service

import (
	"context"
	"time"

	"cybercalc/internal/modules/health/domain"
	"cybercalc/internal/modules/health/repository"
	"cybercalc/internal/platform/apperror"
)

const defaultReadinessTimeout = 2 * time.Second

type Service struct {
	probe   repository.Probe
	timeout time.Duration
}

func New(probe repository.Probe) *Service {
	return &Service{probe: probe, timeout: defaultReadinessTimeout}
}

// Liveness proves that the HTTP process can serve requests. It intentionally
// has no infrastructure dependency.
func (s *Service) Liveness() domain.Status {
	return domain.OK()
}

// Readiness additionally proves that PostgreSQL is reachable.
func (s *Service) Readiness(ctx context.Context) (domain.Status, error) {
	probeContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	if s.probe == nil {
		return domain.Status{}, apperror.New(apperror.KindUnavailable, "database_unavailable", "база данных недоступна", nil, repository.ErrDatabaseNotConfigured)
	}
	if err := s.probe.Ping(probeContext); err != nil {
		return domain.Status{}, apperror.New(apperror.KindUnavailable, "database_unavailable", "база данных недоступна", nil, err)
	}
	return domain.OK(), nil
}
