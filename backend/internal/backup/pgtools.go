package backup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// pgEnv переводит строку подключения (URL или key=value) в переменные libpq.
// Пароль так не попадает в список процессов, как попал бы в аргументах.
func pgEnv(dsn string) ([]string, string, error) {
	values := map[string]string{}
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, "", fmt.Errorf("backup: строка подключения: %w", err)
		}
		values["host"], values["port"] = u.Hostname(), u.Port()
		values["dbname"] = strings.TrimPrefix(u.Path, "/")
		if u.User != nil {
			values["user"] = u.User.Username()
			values["password"], _ = u.User.Password()
		}
		for k, v := range u.Query() {
			values[k] = v[0]
		}
	default:
		for _, part := range strings.Fields(dsn) {
			k, v, ok := strings.Cut(part, "=")
			if ok {
				values[k] = strings.Trim(v, "'")
			}
		}
	}
	mapping := map[string]string{"host": "PGHOST", "port": "PGPORT", "user": "PGUSER", "password": "PGPASSWORD", "sslmode": "PGSSLMODE"}
	env := os.Environ()
	for key, name := range mapping {
		if v := values[key]; v != "" {
			env = append(env, name+"="+v)
		}
	}
	return env, values["dbname"], nil
}

func toolPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("backup: не найден %s (нужен клиент PostgreSQL): %w", name, err)
	}
	return path, nil
}

// PGDump возвращает источник дампа: pg_dump в сжатом пользовательском формате,
// поток идёт прямо в копию.
func PGDump(dsn string) func(context.Context, io.Writer) error {
	return func(ctx context.Context, w io.Writer) error {
		env, dbname, err := pgEnv(dsn)
		if err != nil {
			return err
		}
		tool, err := toolPath("pg_dump")
		if err != nil {
			return err
		}
		var stderr bytes.Buffer
		// tool — путь, найденный по фиксированному имени; пользовательский ввод только в аргументе --dbname.
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.CommandContext(ctx, tool, "--format=custom", "--no-password", "--dbname="+dbname)
		cmd.Env, cmd.Stdout, cmd.Stderr = env, w, &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
}

// PGRestore восстанавливает дамп в указанную базу (без назначения владельцев:
// восстановление идёт под учётной записью проверяющего).
func PGRestore(ctx context.Context, dsn string, dump io.Reader) error {
	env, dbname, err := pgEnv(dsn)
	if err != nil {
		return err
	}
	tool, err := toolPath("pg_restore")
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, tool, "--no-owner", "--no-privileges", "--no-password", "--exit-on-error", "--dbname="+dbname)
	cmd.Env, cmd.Stdin, cmd.Stderr = env, dump, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// CollectFacts снимает сведения о данных, по которым проверяется восстановление:
// число записей, вложений, строк аудита, применённых миграций и голова цепочки
// аудита. Значения сравниваются как строки.
func CollectFacts(ctx context.Context, db *sql.DB) (map[string]string, error) {
	queries := map[string]string{
		"entries":     `SELECT count(*)::text FROM entries`,
		"attachments": `SELECT count(*)::text FROM attachments`,
		"audit_rows":  `SELECT ((SELECT count(*) FROM audit_log)+(SELECT count(*) FROM audit_log_archive))::text`,
		"audit_head":  `SELECT COALESCE((SELECT row_hash FROM audit_log ORDER BY id DESC LIMIT 1),'')`,
		"migrations":  `SELECT count(*)::text FROM schema_migrations`,
	}
	facts := make(map[string]string, len(queries))
	for name, query := range queries {
		var value string
		if err := db.QueryRowContext(ctx, query).Scan(&value); err != nil {
			return nil, fmt.Errorf("факт %s: %w", name, err)
		}
		facts[name] = value
	}
	return facts, nil
}

// CompareFacts возвращает расхождения между ожидаемыми и фактическими значениями.
func CompareFacts(want, got map[string]string) []string {
	var diffs []string
	for name, expected := range want {
		if name == "uploads_root" {
			continue
		}
		if actual, ok := got[name]; !ok {
			diffs = append(diffs, name+": нет в восстановленной базе")
		} else if actual != expected {
			diffs = append(diffs, fmt.Sprintf("%s: в копии %q, после восстановления %q", name, expected, actual))
		}
	}
	sort.Strings(diffs)
	return diffs
}

// ErrDrillFailed — восстановление выполнено, но проверка целостности не пройдена.
var ErrDrillFailed = errors.New("backup: проверка восстановления не пройдена")
