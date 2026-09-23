package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

// OPS-12: показатели эксплуатации. Инстанс ставится в закрытый контур без
// внешнего мониторинга, поэтому состояние подсистем должно читаться из самой
// системы: иначе о просроченном сроке, несформированном снимке или растущей
// очереди удаления узнают по последствиям.
type operationsMetrics struct {
	CollectedAt time.Time `json:"collected_at"`

	// Регламентные сроки: ближайшая контрольная дата и число просроченных.
	Timers struct {
		ReportYear   int    `json:"report_year"`
		NextDeadline string `json:"next_deadline,omitempty"`
		DaysLeft     int    `json:"days_left"`
		Overdue      int    `json:"overdue"`
	} `json:"timers"`

	// Снимок на 1 мая: без него Приложение № 4 не формируется.
	Snapshot struct {
		Sealed     bool   `json:"sealed"`
		SealedAt   string `json:"sealed_at,omitempty"`
		ReportYear int    `json:"report_year"`
	} `json:"snapshot"`

	// Хранилище: сколько документов и байт учтено, сколько ждёт удаления.
	Storage struct {
		Documents      int   `json:"documents"`
		DistinctBlobs  int   `json:"distinct_blobs"`
		Bytes          int64 `json:"bytes"`
		Quarantined    int   `json:"quarantined"`
		Unscanned      int   `json:"unscanned"`
		DeletionQueue  int   `json:"deletion_queue"`
		ExpiringInWeek int   `json:"expiring_in_week"`
	} `json:"storage"`

	// Журнал: объём и подтверждение целостности цепочки.
	Audit struct {
		Records       int    `json:"records"`
		Archived      int    `json:"archived"`
		ChainVerified bool   `json:"chain_verified"`
		ChainProblem  string `json:"chain_problem,omitempty"`
	} `json:"audit"`

	// База: доступность и число активных сессий.
	Database struct {
		Reachable      bool  `json:"reachable"`
		ActiveSessions int   `json:"active_sessions"`
		LatencyMillis  int64 `json:"latency_ms"`
	} `json:"database"`
}

// Metrics отдаёт срез состояния подсистем. Доступ администраторский: срез
// показывает объёмы и целостность по всей организации.
func (h *AdminHandlers) Metrics(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	if admin.Role != models.RoleSuperAdmin && admin.Role != models.RoleHoldingAdmin && admin.Role != models.RoleOrgAdmin {
		middleware.WriteError(w, http.StatusForbidden, "показатели эксплуатации доступны администратору")
		return
	}
	metrics := operationsMetrics{CollectedAt: time.Now().UTC()}
	year := time.Now().In(businessLocation).Year()
	metrics.Timers.ReportYear = year
	metrics.Snapshot.ReportYear = year

	started := time.Now()
	if err := h.DB.PingContext(r.Context()); err == nil {
		metrics.Database.Reachable = true
	}
	metrics.Database.LatencyMillis = time.Since(started).Milliseconds()
	if !metrics.Database.Reachable {
		// Без базы остальные показатели собрать нечем; отдаём то, что есть.
		middleware.WriteJSON(w, http.StatusOK, metrics)
		return
	}

	// Ближайший регламентный срок считает тот же календарь, что и интерфейс.
	now := time.Now().In(businessLocation)
	for _, milestone := range regulatoryMilestones(year, now) {
		if milestone.Overdue {
			metrics.Timers.Overdue++
			continue
		}
		if metrics.Timers.NextDeadline == "" || milestone.Date < metrics.Timers.NextDeadline {
			metrics.Timers.NextDeadline = milestone.Date
			metrics.Timers.DaysLeft = milestone.DaysLeft
		}
	}

	company := itCompanyScope(admin)
	var sealedAt sql.NullString
	if company != "" && company != "unassigned" {
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT captured_at::text FROM report_snapshots WHERE it_company_id::text=$1 AND report_year=$2`,
			company, year).Scan(&sealedAt)
	}
	if sealedAt.Valid {
		metrics.Snapshot.Sealed = true
		metrics.Snapshot.SealedAt = sealedAt.String
	}

	_ = h.DB.QueryRowContext(r.Context(), `SELECT count(*),count(DISTINCT storage_path),COALESCE(sum(size_bytes),0),
		count(*) FILTER(WHERE scan_status='quarantined'),count(*) FILTER(WHERE scan_status='unscanned'),
		count(*) FILTER(WHERE retention_expires_at < now()+interval '7 days')
		FROM attachments`).Scan(&metrics.Storage.Documents, &metrics.Storage.DistinctBlobs,
		&metrics.Storage.Bytes, &metrics.Storage.Quarantined, &metrics.Storage.Unscanned,
		&metrics.Storage.ExpiringInWeek)
	_ = h.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM file_deletion_queue`).Scan(&metrics.Storage.DeletionQueue)

	_ = h.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM audit_log`).Scan(&metrics.Audit.Records)
	_ = h.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM audit_log_archive`).Scan(&metrics.Audit.Archived)
	var problem sql.NullString
	err := h.DB.QueryRowContext(r.Context(), `SELECT problem FROM verify_audit_chain()`).Scan(&problem)
	metrics.Audit.ChainVerified = err == sql.ErrNoRows
	if problem.Valid {
		metrics.Audit.ChainProblem = problem.String
	}

	_ = h.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM sessions WHERE expires_at>now()`).
		Scan(&metrics.Database.ActiveSessions)

	middleware.WriteJSON(w, http.StatusOK, metrics)
}
