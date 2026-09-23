package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/rbac"
	"cybercalc/internal/tasks"
)

const (
	taskReportVerify  = "report_verify"
	taskReportApprove = "report_approve"
	taskSubjectReport = "agreement_report"
)

// syncReportTasks (SEC-10) ставит и закрывает задачи по событиям отчёта, чтобы
// следующий шаг не зависел от того, что администратор вспомнит поставить
// задачу вручную:
//
//	ready    → задача «проверить» профильному специалисту (куратору);
//	verified → «проверить» выполнена, задача «утвердить» администратору;
//	approved → «утвердить» выполнена;
//	draft    → открытые задачи по отчёту отменены — отчёт вернули на доработку.
//
// Маршрут выбирает та же цепочка ADR-22, что и для задач, поставленных
// вручную; вызывается внутри транзакции смены статуса.
func syncReportTasks(ctx context.Context, tx *sql.Tx, u middleware.AuthUser, agreementID string, year int, period, status string) error {
	subject := fmt.Sprintf("%s:%d:%s", agreementID, year, period)
	closeTask := func(kind, result string) error {
		_, err := tasks.CloseBySubject(ctx, tx, kind, taskSubjectReport, subject, result, u.ID)
		return err
	}
	switch status {
	case "verified":
		if err := closeTask(taskReportVerify, "done"); err != nil {
			return err
		}
	case "approved":
		if err := closeTask(taskReportVerify, "done"); err != nil {
			return err
		}
		return closeTask(taskReportApprove, "done")
	case "draft":
		if err := closeTask(taskReportVerify, "cancelled"); err != nil {
			return err
		}
		return closeTask(taskReportApprove, "cancelled")
	}
	if status != "ready" && status != "verified" {
		return nil
	}

	var tenant, number string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(it_company_id::text,''),number FROM agreements WHERE id::text=$1`, agreementID).Scan(&tenant, &number); err != nil {
		return err
	}
	if tenant == "" {
		return nil // соглашение без арендатора (устаревшие данные): назначать некому
	}
	var partner string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT partner_id::text FROM agreement_partners WHERE agreement_id::text=$1
		ORDER BY is_primary DESC, partner_id LIMIT 1),'')`, agreementID).Scan(&partner); err != nil {
		return err
	}
	next := tasks.NewTask{TenantID: tenant, PartnerID: partner, SubjectType: taskSubjectReport, SubjectID: subject, CreatedBy: u.ID}
	label := fmt.Sprintf("по соглашению № %s (%s %d)", number, map[string]string{"plan": "план", "fact": "факт"}[period], year)
	if status == "ready" {
		next.Kind, next.Role, next.Permission = taskReportVerify, models.RoleCurator, rbac.PrepareReports
		next.Title = "Проверить отчёт " + label
	} else {
		next.Kind, next.Role, next.Permission = taskReportApprove, models.RoleOrgAdmin, rbac.ApproveReports
		next.Title = "Утвердить отчёт " + label
	}
	task, created, err := tasks.Dispatch(ctx, tx, next, time.Now())
	if err != nil {
		return err
	}
	if !created { // повторная постановка возвращает уже открытую задачу
		return nil
	}
	return logAudit(ctx, tx, "workflow_task", task.ID, "create", u.ID, task.FallbackReason, nil, task)
}
