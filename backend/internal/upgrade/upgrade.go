// Package upgrade проверяет обновление и откат инстанса (OPS-11): подпись
// релиза, соответствие миграций, свежую и целую резервную копию, запас места.
//
// Схема БД меняется только вперёд: применённые миграции не правятся и не
// удаляются. Откат — это откат образов приложения, поэтому проверка отката
// говорит, сможет ли прежний релиз работать на текущей схеме.
package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"cybercalc/internal/backup"

	"github.com/lib/pq"
)

// Migration — миграция с контрольной суммой (SHA-256 содержимого файла, как в
// таблице schema_migrations).
type Migration struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Release — состав релиза, который подписывается.
type Release struct {
	Version   string            `json:"version"`
	Previous  string            `json:"previous,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Images    map[string]string `json:"images"` // сервис → digest образа
	// Migrations — полный перечень миграций релиза.
	Migrations []Migration `json:"migrations"`
	// Files — файлы автономного пакета (образы, конфигурация, сценарии) с
	// контрольными суммами; пусто у релиза, доставляемого через реестр.
	Files []Migration `json:"files,omitempty"`
}

// Gate — один пункт предполётной проверки.
type Gate struct {
	Name   string
	Passed bool
	Detail string
}

// Report — итог: обновление допустимо, когда пройдены все пункты.
type Report struct{ Gates []Gate }

// OK — все пункты пройдены.
func (r Report) OK() bool {
	for _, g := range r.Gates {
		if !g.Passed {
			return false
		}
	}
	return len(r.Gates) > 0
}

func (r Report) String() string {
	var b strings.Builder
	for _, g := range r.Gates {
		mark := "ОК  "
		if !g.Passed {
			mark = "СБОЙ"
		}
		fmt.Fprintf(&b, "%s %s — %s\n", mark, g.Name, g.Detail)
	}
	return b.String()
}

// ReadMigrations читает каталог миграций.
func ReadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		out = append(out, Migration{Name: e.Name(), SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadApplied читает применённые миграции из БД.
func ReadApplied(ctx context.Context, db *sql.DB) ([]Migration, error) {
	rows, err := db.QueryContext(ctx, `SELECT filename,COALESCE(checksum,'') FROM schema_migrations ORDER BY filename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Migration
	for rows.Next() {
		var m Migration
		if err := rows.Scan(&m.Name, &m.SHA256); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReadAppliedOrEmpty отличается от ReadApplied тем, что отсутствие таблицы
// миграций (первая установка, база ещё пуста) даёт пустой список, а не ошибку.
func ReadAppliedOrEmpty(ctx context.Context, db *sql.DB) ([]Migration, error) {
	applied, err := ReadApplied(ctx, db)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "42P01" {
		return nil, nil
	}
	return applied, err
}

// Plan — что произойдёт при обновлении.
type Plan struct {
	Pending  []string
	Problems []string
}

// PlanUpgrade сопоставляет миграции релиза с применёнными. Проблемы:
// применённая миграция отсутствует в релизе (база новее пакета), изменена или
// новая миграция стоит раньше уже применённой (порядок нарушен).
func PlanUpgrade(release, applied []Migration) Plan {
	var plan Plan
	inRelease := map[string]Migration{}
	for _, m := range release {
		inRelease[m.Name] = m
	}
	appliedSet := map[string]bool{}
	lastApplied := ""
	for _, m := range applied {
		appliedSet[m.Name] = true
		if m.Name > lastApplied {
			lastApplied = m.Name
		}
		want, ok := inRelease[m.Name]
		switch {
		case !ok:
			plan.Problems = append(plan.Problems, fmt.Sprintf("миграция %s применена, а в релизе её нет: база новее пакета, откат схемы невозможен", m.Name))
		case m.SHA256 != "" && want.SHA256 != m.SHA256:
			plan.Problems = append(plan.Problems, fmt.Sprintf("применённая миграция %s изменена в релизе", m.Name))
		}
	}
	for _, m := range release {
		if appliedSet[m.Name] {
			continue
		}
		if m.Name < lastApplied {
			plan.Problems = append(plan.Problems, fmt.Sprintf("новая миграция %s стоит раньше уже применённой %s: порядок нарушен", m.Name, lastApplied))
		}
		plan.Pending = append(plan.Pending, m.Name)
	}
	return plan
}

// RollbackReport — может ли прежний релиз работать на текущей схеме.
type RollbackReport struct {
	// Missing — миграции прежнего релиза, которых нет в базе: схема старее, чем
	// ждёт прежний релиз (такой «откат» — на самом деле обновление).
	Missing []string
	// Altered — миграции, применённые иначе, чем в прежнем релизе.
	Altered []string
	// NewerSchema — миграции, которых прежний релиз не знает. Откат допустим,
	// если они только добавляют (расширение схемы); иначе решает человек.
	NewerSchema []string
}

// Safe — откат не требует ничего, кроме подтверждения аддитивности.
func (r RollbackReport) Safe() bool { return len(r.Missing) == 0 && len(r.Altered) == 0 }

// CheckRollback сверяет прежний релиз с применёнными миграциями.
func CheckRollback(target, applied []Migration) RollbackReport {
	var r RollbackReport
	appliedBy := map[string]Migration{}
	for _, m := range applied {
		appliedBy[m.Name] = m
	}
	known := map[string]bool{}
	for _, m := range target {
		known[m.Name] = true
		got, ok := appliedBy[m.Name]
		switch {
		case !ok:
			r.Missing = append(r.Missing, m.Name)
		case got.SHA256 != "" && got.SHA256 != m.SHA256:
			r.Altered = append(r.Altered, m.Name)
		}
	}
	for _, m := range applied {
		if !known[m.Name] {
			r.NewerSchema = append(r.NewerSchema, m.Name)
		}
	}
	sort.Strings(r.NewerSchema)
	return r
}

// Signed — подписанный релиз: канонический JSON и отсоединённая подпись Ed25519.
type Signed struct {
	Release   json.RawMessage `json:"release"`
	Signature string          `json:"signature"`
}

// Sign подписывает релиз.
func Sign(release Release, key ed25519.PrivateKey) ([]byte, error) {
	if release.Version == "" || len(release.Images) == 0 || len(release.Migrations) == 0 {
		return nil, errors.New("upgrade: релиз должен называть версию, образы и миграции")
	}
	body, err := json.Marshal(release)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Signed{Release: body, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, body))})
}

// ErrSignature — подпись не подтверждена.
var ErrSignature = errors.New("upgrade: подпись релиза не подтверждена")

// Verify проверяет подпись доверенным открытым ключом и возвращает релиз.
// Разбирается только проверенное содержимое.
func Verify(data []byte, key ed25519.PublicKey) (Release, error) {
	var signed Signed
	if err := json.Unmarshal(data, &signed); err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrSignature, err)
	}
	signature, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, signed.Release, signature) {
		return Release{}, ErrSignature
	}
	var release Release
	if err := json.Unmarshal(signed.Release, &release); err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrSignature, err)
	}
	return release, nil
}

// Options — параметры предполётной проверки.
type Options struct {
	Release Release
	// MigrationsDir — каталог миграций, поставляемый с релизом.
	MigrationsDir string
	// BackupPath и Passphrase — копия, сделанная перед обновлением.
	BackupPath string
	Passphrase string
	// MaxBackupAge — насколько свежей должна быть копия.
	MaxBackupAge time.Duration
	// DataDir и MinFreeBytes — запас места под обновление.
	DataDir      string
	MinFreeBytes uint64
	Now          func() time.Time
}

// Preflight проверяет, что обновление можно начинать. Ничего не меняет.
func Preflight(ctx context.Context, db *sql.DB, opts Options) Report {
	var report Report
	add := func(name string, ok bool, detail string) {
		report.Gates = append(report.Gates, Gate{Name: name, Passed: ok, Detail: detail})
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	onDisk, err := ReadMigrations(opts.MigrationsDir)
	if err != nil {
		add("миграции релиза читаются", false, err.Error())
	} else {
		plan := PlanUpgrade(opts.Release.Migrations, onDisk)
		// Подписанный перечень и файлы пакета должны совпасть без остатка.
		ok := len(plan.Problems) == 0 && len(plan.Pending) == 0
		detail := fmt.Sprintf("миграций в пакете: %d", len(onDisk))
		if !ok {
			detail = fmt.Sprintf("подписанный перечень и файлы пакета расходятся: %v %v", plan.Problems, plan.Pending)
		}
		add("файлы миграций совпадают с подписанным релизом", ok, detail)
	}

	applied, err := ReadApplied(ctx, db)
	if err != nil {
		add("применённые миграции читаются", false, err.Error())
	} else {
		plan := PlanUpgrade(opts.Release.Migrations, applied)
		add("схема базы согласуется с релизом", len(plan.Problems) == 0,
			fmt.Sprintf("применено %d, будет применено %d %s", len(applied), len(plan.Pending), strings.Join(plan.Problems, "; ")))
	}

	if opts.BackupPath == "" {
		add("резервная копия перед обновлением", false, "копия не указана")
	} else if in, err := os.Open(opts.BackupPath); err != nil {
		add("резервная копия перед обновлением", false, err.Error())
	} else {
		manifest, verifyErr := backup.Verify(in, opts.Passphrase)
		in.Close()
		switch {
		case verifyErr != nil:
			add("резервная копия перед обновлением", false, "копия не прошла проверку: "+verifyErr.Error())
		case opts.MaxBackupAge > 0 && now().Sub(manifest.CreatedAt) > opts.MaxBackupAge:
			add("резервная копия перед обновлением", false, fmt.Sprintf("копия от %s старше %s", manifest.CreatedAt.Format(time.RFC3339), opts.MaxBackupAge))
		default:
			add("резервная копия перед обновлением", true, fmt.Sprintf("цела, создана %s", manifest.CreatedAt.Format(time.RFC3339)))
		}
	}

	if opts.DataDir != "" && opts.MinFreeBytes > 0 {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(opts.DataDir, &stat); err != nil {
			add("запас места", false, err.Error())
		} else {
			free := stat.Bavail * uint64(stat.Bsize)
			add("запас места", free >= opts.MinFreeBytes, fmt.Sprintf("свободно %d МиБ, нужно %d МиБ", free>>20, opts.MinFreeBytes>>20))
		}
	}
	return report
}

// ReadFile читает файл целиком с ограничением размера: манифест релиза не
// бывает большим.
func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 4<<20))
}

// BuildFiles перечисляет файлы автономного пакета с контрольными суммами. Файлы
// из skip (например, сам подписанный релиз) в перечень не входят. Символические
// ссылки и файлы особых типов недопустимы: пакет должен состоять из обычных файлов.
func BuildFiles(root string, skip ...string) ([]Migration, error) {
	skipSet := map[string]bool{}
	for _, name := range skip {
		skipSet[filepath.ToSlash(name)] = true
	}
	var out []Migration
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skipSet[rel] {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("upgrade: %s — не обычный файл (ссылка или особый тип)", rel)
		}
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		out = append(out, Migration{Name: rel, SHA256: sum})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// VerifyTree сверяет каталог автономного пакета с подписанным перечнем файлов:
// не должно быть ни изменённых, ни отсутствующих, ни лишних файлов. Лишний
// файл — тоже нарушение: в проверенный пакет нельзя подложить сценарий.
func VerifyTree(root string, release Release, skip ...string) []string {
	actual, err := BuildFiles(root, skip...)
	if err != nil {
		return []string{err.Error()}
	}
	want := map[string]string{}
	for _, f := range release.Files {
		want[f.Name] = f.SHA256
	}
	var problems []string
	seen := map[string]bool{}
	for _, f := range actual {
		seen[f.Name] = true
		expected, ok := want[f.Name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("лишний файл %s", f.Name))
		case expected != f.SHA256:
			problems = append(problems, fmt.Sprintf("файл %s изменён", f.Name))
		}
	}
	for name := range want {
		if !seen[name] {
			problems = append(problems, fmt.Sprintf("нет файла %s", name))
		}
	}
	sort.Strings(problems)
	return problems
}
