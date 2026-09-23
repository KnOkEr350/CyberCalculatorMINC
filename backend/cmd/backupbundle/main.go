// Command backupbundle создаёт, проверяет и восстанавливает шифрованную
// резервную копию инстанса (OPS-10) и выполняет учебное восстановление (QA-11).
//
//	backupbundle create  <файл>            # DATABASE_DSN, UPLOAD_DIR, BACKUP_CONFIG=имя=путь,...
//	backupbundle verify  <файл>            # расшифровка и контрольные суммы, ничего не сохраняется
//	backupbundle restore <файл> <каталог>  # каталога назначения быть не должно; базу вернёт pg_restore
//	backupbundle drill   <файл> <каталог>  # ADMIN_DSN: временная база, сверка данных и вложений
//
// Парольная фраза берётся из файла BACKUP_PASSPHRASE_FILE (предпочтительно) или
// из переменной BACKUP_PASSPHRASE и должна быть не короче 12 символов.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"cybercalc/internal/backup"

	_ "github.com/lib/pq"
)

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "backupbundle: "+format+"\n", args...)
	os.Exit(1)
}

func passphrase() string {
	if path := os.Getenv("BACKUP_PASSPHRASE_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			fail("файл парольной фразы: %v", err)
		}
		return strings.TrimRight(string(data), "\r\n")
	}
	if value := os.Getenv("BACKUP_PASSPHRASE"); value != "" {
		return value
	}
	fail("задайте BACKUP_PASSPHRASE_FILE или BACKUP_PASSPHRASE")
	return ""
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(os.Args) < 3 {
		fail("использование: backupbundle create|verify|restore|drill <файл> [каталог]")
	}
	command, file := os.Args[1], os.Args[2]
	switch command {
	case "create":
		create(ctx, file)
	case "verify":
		in, err := os.Open(file)
		if err != nil {
			fail("%v", err)
		}
		defer in.Close()
		manifest, err := backup.Verify(in, passphrase())
		if err != nil {
			fail("копия не прошла проверку: %v", err)
		}
		fmt.Printf("копия цела: %d файлов, создана %s, версия %s\n", len(manifest.Files), manifest.CreatedAt.Format(time.RFC3339), manifest.AppVersion)
	case "restore":
		if len(os.Args) != 4 {
			fail("использование: backupbundle restore <файл> <каталог>")
		}
		in, err := os.Open(file)
		if err != nil {
			fail("%v", err)
		}
		defer in.Close()
		sink, err := backup.NewDirSink(os.Args[3])
		if err != nil {
			fail("%v", err)
		}
		manifest, err := backup.Read(in, passphrase(), sink)
		if err != nil {
			fail("восстановление отменено, каталог назначения не создан: %v", err)
		}
		fmt.Printf("восстановлено %d файлов в %s; базу верните командой pg_restore из %s\n",
			len(manifest.Files), os.Args[3], filepath.Join(os.Args[3], backup.DatabaseEntry))
	case "drill":
		if len(os.Args) != 4 {
			fail("использование: backupbundle drill <файл> <рабочий каталог>")
		}
		admin := os.Getenv("ADMIN_DSN")
		if admin == "" {
			fail("задайте ADMIN_DSN: подключение с правом создавать базы")
		}
		in, err := os.Open(file)
		if err != nil {
			fail("%v", err)
		}
		defer in.Close()
		result, err := backup.Drill(ctx, backup.DrillOptions{Bundle: in, Passphrase: passphrase(), AdminDSN: admin, WorkDir: os.Args[3]})
		for _, check := range result.Checks {
			mark := "ОК "
			if !check.Passed {
				mark = "СБОЙ"
			}
			fmt.Printf("%s %s — %s\n", mark, check.Name, check.Detail)
		}
		if err != nil {
			fail("учебное восстановление не пройдено: %v", err)
		}
		fmt.Println("учебное восстановление пройдено")
	default:
		fail("неизвестная команда %q", command)
	}
}

func create(ctx context.Context, file string) {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		fail("задайте DATABASE_DSN")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fail("%v", err)
	}
	defer db.Close()
	facts, err := backup.CollectFacts(ctx, db)
	if err != nil {
		fail("%v", err)
	}
	uploads := os.Getenv("UPLOAD_DIR")
	if uploads != "" {
		facts["uploads_root"] = filepath.Clean(uploads)
	}
	config := map[string]string{}
	for _, pair := range strings.Split(os.Getenv("BACKUP_CONFIG"), ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		name, path, ok := strings.Cut(pair, "=")
		if !ok || name == "" || path == "" {
			fail("BACKUP_CONFIG: ожидается имя=путь, получено %q", pair)
		}
		config[name] = path
	}
	// Копия пишется во временный файл рядом и переименовывается после
	// успешного завершения: обрывок не принимают за копию.
	tmp := file + ".partial"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fail("%v", err)
	}
	manifest, err := backup.Create(ctx, out, passphrase(), backup.Source{Dump: backup.PGDump(dsn), UploadsDir: uploads,
		ConfigFiles: config, AppVersion: os.Getenv("APP_VERSION"), Facts: facts})
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		fail("копия не создана: %v", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		fail("%v", err)
	}
	fmt.Printf("копия создана: %s, файлов %d, записей %s\n", file, len(manifest.Files), facts["entries"])
}
