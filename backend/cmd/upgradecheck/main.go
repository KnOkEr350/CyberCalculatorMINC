// Command upgradecheck готовит и проверяет обновление и откат (OPS-11).
//
//	upgradecheck keygen <каталог>                       # пара ключей Ed25519 для подписи релизов
//	upgradecheck release <миграции> <версия> <файл> ... # образы: сервис=digest, печатает неподписанный релиз
//	upgradecheck files <каталог пакета> <исключение>...   # перечень файлов автономного пакета (JSON)
//	upgradecheck verify-tree <каталог пакета> <подписанный файл> <открытый ключ>
//	upgradecheck sign <release.json> <секретный ключ> <подписанный файл>
//	upgradecheck verify <подписанный файл> <открытый ключ>
//	upgradecheck preflight <подписанный файл> <открытый ключ> <миграции>
//	    # DATABASE_DSN, BACKUP_FILE, BACKUP_PASSPHRASE_FILE, DATA_DIR; проверка ничего не меняет
//	upgradecheck schema <миграции>
//	    # DATABASE_DSN или DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME: подходит ли схема базы этому дереву миграций
//	upgradecheck rollback <подписанный прежний релиз> <открытый ключ>
//	    # DATABASE_DSN: сможет ли прежний релиз работать на текущей схеме
//
// Код возврата 3 — проверка не пройдена, обновлять или откатывать нельзя.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cybercalc/internal/upgrade"

	_ "github.com/lib/pq"
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "upgradecheck: "+format+"\n", args...)
	os.Exit(1)
}

func readKey(path string, size int) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != size {
		fail("ключ %s: ожидается base64 длиной %d байт", path, size)
	}
	return key
}

func main() {
	if len(os.Args) < 2 {
		fail("использование: upgradecheck keygen|release|sign|verify|preflight|rollback ...")
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "keygen":
		if len(args) != 1 {
			fail("использование: upgradecheck keygen <каталог>")
		}
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail("%v", err)
		}
		if err := os.MkdirAll(args[0], 0o700); err != nil {
			fail("%v", err)
		}
		if err := os.WriteFile(filepath.Join(args[0], "release.key"), []byte(base64.StdEncoding.EncodeToString(private)+"\n"), 0o600); err != nil {
			fail("%v", err)
		}
		if err := os.WriteFile(filepath.Join(args[0], "release.pub"), []byte(base64.StdEncoding.EncodeToString(public)+"\n"), 0o644); err != nil {
			fail("%v", err)
		}
		fmt.Println("ключи созданы: release.key храните вне репозитория, release.pub распространяйте вместе с инстансом")
	case "release":
		if len(args) < 3 {
			fail("использование: upgradecheck release <миграции> <версия> <сервис=digest>...")
		}
		migrations, err := upgrade.ReadMigrations(args[0])
		if err != nil {
			fail("%v", err)
		}
		images := map[string]string{}
		for _, pair := range args[2:] {
			name, digest, ok := strings.Cut(pair, "=")
			if !ok || name == "" || !strings.HasPrefix(digest, "sha256:") {
				fail("образ %q: ожидается сервис=sha256:...", pair)
			}
			images[name] = digest
		}
		out, _ := json.MarshalIndent(upgrade.Release{Version: args[1], Previous: os.Getenv("PREVIOUS_VERSION"), CreatedAt: time.Now().UTC(),
			Images: images, Migrations: migrations}, "", "  ")
		fmt.Println(string(out))
	case "files":
		if len(args) < 1 {
			fail("использование: upgradecheck files <каталог пакета> <исключение>...")
		}
		files, err := upgrade.BuildFiles(args[0], args[1:]...)
		if err != nil {
			fail("%v", err)
		}
		out, _ := json.MarshalIndent(files, "", "  ")
		fmt.Println(string(out))
	case "verify-tree":
		if len(args) != 3 {
			fail("использование: upgradecheck verify-tree <каталог пакета> <подписанный файл> <открытый ключ>")
		}
		release := loadRelease(args[1], args[2])
		if len(release.Files) == 0 {
			fail("подписанный релиз не описывает файлы пакета")
		}
		problems := upgrade.VerifyTree(args[0], release, filepath.Base(args[1]))
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "upgradecheck:", problem)
		}
		if len(problems) > 0 {
			os.Exit(3)
		}
		fmt.Printf("пакет цел: %d файлов совпадают с подписанным перечнем\n", len(release.Files))
	case "sign":
		if len(args) != 3 {
			fail("использование: upgradecheck sign <release.json> <секретный ключ> <выходной файл>")
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			fail("%v", err)
		}
		var release upgrade.Release
		if err := json.Unmarshal(data, &release); err != nil {
			fail("релиз: %v", err)
		}
		signed, err := upgrade.Sign(release, readKey(args[1], ed25519.PrivateKeySize))
		if err != nil {
			fail("%v", err)
		}
		if err := os.WriteFile(args[2], signed, 0o644); err != nil {
			fail("%v", err)
		}
		fmt.Println("релиз подписан:", args[2])
	case "verify":
		if len(args) != 2 {
			fail("использование: upgradecheck verify <подписанный файл> <открытый ключ>")
		}
		release := loadRelease(args[0], args[1])
		fmt.Printf("подпись подтверждена: версия %s, образов %d, миграций %d\n", release.Version, len(release.Images), len(release.Migrations))
	case "preflight":
		if len(args) != 3 {
			fail("использование: upgradecheck preflight <подписанный файл> <открытый ключ> <миграции>")
		}
		release := loadRelease(args[0], args[1])
		db := openDB()
		defer db.Close()
		passphrase := ""
		if path := os.Getenv("BACKUP_PASSPHRASE_FILE"); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				fail("%v", err)
			}
			passphrase = strings.TrimRight(string(data), "\r\n")
		}
		report := upgrade.Preflight(context.Background(), db, upgrade.Options{Release: release, MigrationsDir: args[2],
			BackupPath: os.Getenv("BACKUP_FILE"), Passphrase: passphrase, MaxBackupAge: 6 * time.Hour,
			DataDir: os.Getenv("DATA_DIR"), MinFreeBytes: 2 << 30})
		fmt.Print(report.String())
		if !report.OK() {
			os.Exit(3)
		}
		fmt.Println("обновление можно начинать")
	case "schema":
		if len(args) != 1 {
			fail("использование: upgradecheck schema <миграции>")
		}
		tree, err := upgrade.ReadMigrations(args[0])
		if err != nil {
			fail("%v", err)
		}
		db := openDB()
		defer db.Close()
		applied, err := upgrade.ReadAppliedOrEmpty(context.Background(), db)
		if err != nil {
			fail("%v", err)
		}
		plan := upgrade.PlanUpgrade(tree, applied)
		fmt.Printf("применено миграций %d, в дереве %d, будет применено %d\n", len(applied), len(tree), len(plan.Pending))
		for _, problem := range plan.Problems {
			fmt.Fprintln(os.Stderr, "upgradecheck:", problem)
		}
		if len(plan.Problems) > 0 {
			os.Exit(3)
		}
	case "rollback":
		if len(args) != 2 {
			fail("использование: upgradecheck rollback <подписанный прежний релиз> <открытый ключ>")
		}
		previous := loadRelease(args[0], args[1])
		db := openDB()
		defer db.Close()
		applied, err := upgrade.ReadApplied(context.Background(), db)
		if err != nil {
			fail("%v", err)
		}
		report := upgrade.CheckRollback(previous.Migrations, applied)
		fmt.Printf("прежний релиз %s: не хватает миграций %d, применено иначе %d, схема новее релиза на %d\n",
			previous.Version, len(report.Missing), len(report.Altered), len(report.NewerSchema))
		if !report.Safe() {
			os.Exit(3)
		}
		if len(report.NewerSchema) > 0 {
			fmt.Println("откат приложения возможен, если новые миграции только добавляют (расширение схемы): " + strings.Join(report.NewerSchema, ", "))
		} else {
			fmt.Println("откат приложения возможен")
		}
	default:
		fail("неизвестная команда %q", os.Args[1])
	}
}

func loadRelease(signedPath, keyPath string) upgrade.Release {
	data, err := upgrade.ReadFile(signedPath)
	if err != nil {
		fail("%v", err)
	}
	release, err := upgrade.Verify(data, readKey(keyPath, ed25519.PublicKeySize))
	if err != nil {
		fmt.Fprintln(os.Stderr, "upgradecheck:", err)
		os.Exit(3)
	}
	return release
}

func openDB() *sql.DB {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" && os.Getenv("DB_HOST") != "" {
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			os.Getenv("DB_HOST"), envOr("DB_PORT", "5432"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), envOr("DB_SSLMODE", "disable"))
	}
	if dsn == "" {
		fail("задайте DATABASE_DSN или DB_HOST/DB_USER/DB_PASSWORD/DB_NAME")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fail("%v", err)
	}
	return db
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
