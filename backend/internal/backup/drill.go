package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cybercalc/internal/filestore"
)

// Check — один пункт проверки восстановления.
type Check struct {
	Name   string
	Passed bool
	Detail string
}

// DrillResult — итог учебного восстановления.
type DrillResult struct {
	Manifest Manifest
	Checks   []Check
}

// Passed — все пункты пройдены.
func (r DrillResult) Passed() bool {
	for _, c := range r.Checks {
		if !c.Passed {
			return false
		}
	}
	return len(r.Checks) > 0
}

// DrillOptions — параметры учебного восстановления (QA-11).
type DrillOptions struct {
	Bundle     io.Reader
	Passphrase string
	// AdminDSN — подключение с правом создавать и удалять базы; восстановление
	// идёт в отдельную временную базу, рабочая не затрагивается.
	AdminDSN string
	WorkDir  string
	// Scratch — имя временной базы; пусто — генерируется.
	Scratch string
}

var scratchName = regexp.MustCompile(`^[a-z][a-z0-9_]{2,40}$`)

// replaceDatabase подменяет имя базы в строке подключения.
func replaceDatabase(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		u.Path = "/" + name
		return u.String(), nil
	}
	parts := strings.Fields(dsn)
	replaced := false
	for i, part := range parts {
		if strings.HasPrefix(part, "dbname=") {
			parts[i], replaced = "dbname="+name, true
		}
	}
	if !replaced {
		parts = append(parts, "dbname="+name)
	}
	return strings.Join(parts, " "), nil
}

// Drill восстанавливает копию во временный каталог и временную базу и
// доказывает целостность: контрольные суммы всех файлов, факты о данных из
// манифеста и каждое вложение из восстановленной базы на диске.
// Временная база удаляется в любом случае.
func Drill(ctx context.Context, opts DrillOptions) (DrillResult, error) {
	var result DrillResult
	restored := filepath.Join(opts.WorkDir, "restored")
	sink, err := NewDirSink(restored)
	if err != nil {
		return result, err
	}
	manifest, err := Read(opts.Bundle, opts.Passphrase, sink)
	if err != nil {
		return result, err
	}
	result.Manifest = manifest
	add := func(name string, ok bool, detail string) {
		result.Checks = append(result.Checks, Check{Name: name, Passed: ok, Detail: detail})
	}
	add("копия расшифрована, контрольные суммы файлов сошлись", true, fmt.Sprintf("файлов: %d", len(manifest.Files)))

	scratch := opts.Scratch
	if scratch == "" {
		scratch = "restore_drill_" + strings.ToLower(strings.ReplaceAll(filepath.Base(opts.WorkDir), "-", "_"))
	}
	scratch = strings.ToLower(regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(scratch, "_"))
	if !scratchName.MatchString(scratch) {
		return result, fmt.Errorf("backup: недопустимое имя временной базы %q", scratch)
	}
	admin, err := sql.Open("postgres", opts.AdminDSN)
	if err != nil {
		return result, err
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+scratch); err != nil {
		return result, err
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+scratch); err != nil {
		return result, err
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+scratch+` WITH (FORCE)`)
	}()
	scratchDSN, err := replaceDatabase(opts.AdminDSN, scratch)
	if err != nil {
		return result, err
	}
	dump, err := os.Open(filepath.Join(restored, DatabaseEntry))
	if err != nil {
		return result, err
	}
	err = PGRestore(ctx, scratchDSN, dump)
	dump.Close()
	if err != nil {
		add("база восстановлена", false, err.Error())
		return result, fmt.Errorf("%w: %v", ErrDrillFailed, err)
	}
	add("база восстановлена", true, scratch)

	db, err := sql.Open("postgres", scratchDSN)
	if err != nil {
		return result, err
	}
	defer db.Close()
	facts, err := CollectFacts(ctx, db)
	if err != nil {
		add("факты о данных собраны", false, err.Error())
		return result, fmt.Errorf("%w: %v", ErrDrillFailed, err)
	}
	if diffs := CompareFacts(manifest.Facts, facts); len(diffs) > 0 {
		add("данные совпадают с манифестом", false, strings.Join(diffs, "; "))
	} else {
		add("данные совпадают с манифестом", true, fmt.Sprintf("сверено фактов: %d", len(manifest.Facts)))
	}

	// Каждое вложение восстановленной базы лежит в восстановленном хранилище и
	// совпадает со своим адресом.
	root := manifest.Facts["uploads_root"]
	uploads := filepath.Join(restored, "uploads")
	if root != "" {
		rows, err := db.QueryContext(ctx, `SELECT DISTINCT storage_path FROM attachments`)
		if err != nil {
			return result, err
		}
		var checked, broken int
		var first string
		for rows.Next() {
			var stored string
			if err := rows.Scan(&stored); err != nil {
				rows.Close()
				return result, err
			}
			rel, ok := strings.CutPrefix(stored, strings.TrimRight(root, "/")+"/")
			if !ok {
				continue
			}
			checked++
			target := filepath.Join(uploads, filepath.FromSlash(rel))
			if _, err := os.Stat(target); err != nil {
				broken++
				if first == "" {
					first = rel + ": файла нет"
				}
			} else if filestore.IsBlobPath(uploads, target) {
				if err := filestore.VerifyBlob(uploads, target); err != nil {
					broken++
					if first == "" {
						first = rel + ": " + err.Error()
					}
				}
			}
		}
		rows.Close()
		add("вложения на месте и целы", broken == 0, fmt.Sprintf("проверено %d, нарушений %d %s", checked, broken, first))
	}
	if !result.Passed() {
		return result, ErrDrillFailed
	}
	return result, nil
}
