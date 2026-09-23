// Package bootstrap создаёт и восстанавливает именованного системного
// администратора (SEC-03). Это единственная учётная запись, которая может
// появиться без участия другого администратора, поэтому её жизненный цикл
// вынесен отдельно и покрыт тестами.
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Outcome — что сделал запуск. Явный исход нужен, чтобы оператор видел в
// журнале разницу между «ничего не потребовалось» и «доступ восстановлен».
type Outcome string

const (
	// OutcomeUnchanged — действующий системный администратор уже есть.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeCreated — учётная запись создана на пустой системе.
	OutcomeCreated Outcome = "created"
	// OutcomeRestored — существующая учётная запись включена заново, пароль
	// заменён на заданный оператором.
	OutcomeRestored Outcome = "restored"
)

// Params — данные именованной учётной записи. Пароль приходит уже
// захешированным: сам хеш считает вызывающий, пакет его не хранит.
type Params struct {
	Email        string
	FullName     string
	PasswordHash string
}

type execQuerier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// EnsureAdmin приводит систему в состояние, где есть ровно один способ войти
// как системный администратор.
//
// Восстановление — главное отличие от простого «создать, если нет»: если
// единственная учётная запись отключена или её пароль утрачен, без этого шага
// систему нельзя вернуть в работу, не трогая базу руками. Права на такое
// восстановление даёт не роль в системе, а доступ к её конфигурации и
// перезапуску, то есть локальный root-оператор.
func EnsureAdmin(ctx context.Context, db execQuerier, params Params) (Outcome, error) {
	email := strings.TrimSpace(strings.ToLower(params.Email))
	if email == "" || params.PasswordHash == "" {
		return "", fmt.Errorf("bootstrap: требуются почта и хеш пароля")
	}
	name := strings.TrimSpace(params.FullName)
	if name == "" {
		name = "Системный администратор"
	}

	// Учитываются только действующие записи: отключённый администратор
	// системой не управляет.
	var active int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM users WHERE role='super_admin' AND is_active`).Scan(&active); err != nil {
		return "", err
	}
	if active > 0 {
		// Исторические записи без типа профиля приводятся к операторскому.
		if _, err := db.ExecContext(ctx, `UPDATE users SET entity_type='organization',updated_at=now()
			WHERE role='super_admin' AND entity_type IS NULL`); err != nil {
			return "", err
		}
		return OutcomeUnchanged, nil
	}

	result, err := db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, full_name, role, entity_type, is_active)
		 VALUES ($1,$2,$3,'super_admin','organization',true)
		 ON CONFLICT (email) DO NOTHING`, email, params.PasswordHash, name)
	if err != nil {
		return "", err
	}
	if created, rowsErr := result.RowsAffected(); rowsErr == nil && created == 1 {
		return OutcomeCreated, nil
	}

	// Запись с такой почтой уже есть: возвращаем ей роль, доступ и пароль,
	// заданный оператором. Все прежние сессии закрываются — восстановление не
	// должно оставлять чужой открытый вход.
	if _, err := db.ExecContext(ctx,
		`UPDATE users SET password_hash=$2, full_name=$3, role='super_admin',
			entity_type='organization', is_active=true, updated_at=now()
		 WHERE lower(email)=$1`, email, params.PasswordHash, name); err != nil {
		return "", err
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE lower(email)=$1)`, email); err != nil {
		return "", err
	}
	return OutcomeRestored, nil
}
