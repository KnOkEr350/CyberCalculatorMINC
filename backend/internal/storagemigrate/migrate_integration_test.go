package storagemigrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/dbx"
	"cybercalc/internal/filestore"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"

	_ "github.com/lib/pq"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("isolated workspace_test database required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	return db
}

// STORE-06 на реальной БД: исторические файлы переносятся в CAS со сверкой
// байтов. Перенос идемпотентен, дубликаты схлопываются в один blob, а строки,
// требующие разбора человеком, не «чинятся» молча.
func TestLegacyUploadsMigrateIntoContentAddressedStorage(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	root := t.TempDir()

	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.AssignUserToCompany(ctx, admin.ID, company.ID); err != nil {
		t.Fatal(err)
	}
	university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	partner, err := f.CreatePartner(ctx, company, university)
	if err != nil {
		t.Fatal(err)
	}
	agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID,
		PartnerIDs: []string{partner.ID}, CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	var entryID string
	if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
		audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('internship',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5) RETURNING id::text`,
		partner.ID, agreement.ID, company.ID, time.Now().Year(), admin.ID).Scan(&entryID); err != nil {
		t.Fatal(err)
	}

	// Исторический файл в каталоге мероприятия: имя случайное, адрес не
	// связан с содержимым.
	legacy := func(name string, content []byte, recordedSHA string) (string, string) {
		t.Helper()
		file, path, err := filestore.Create(root, "legacy", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
		file.Close()
		var id string
		var sha interface{}
		if recordedSHA != "" {
			sha = recordedSHA
		}
		if err := db.QueryRowContext(ctx, `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,
			uploaded_by,retention_expires_at,document_type,content_sha256)
			VALUES($1,$2,$3,'application/octet-stream',$4,$5,$6,'labor_contract',$7) RETURNING id::text`,
			entryID, name, path, len(content), admin.ID, time.Now().Add(365*24*time.Hour), sha).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id, path
	}

	shared := []byte("один и тот же трудовой договор, загруженный дважды")
	sharedSum := sha256.Sum256(shared)
	firstID, firstPath := legacy("a.bin", shared, "")
	secondID, secondPath := legacy("b.bin", shared, hex.EncodeToString(sharedSum[:]))
	uniqueID, uniquePath := legacy("c.bin", []byte("отдельный акт сдачи-приёмки"), "")
	// Строка с чужим хешем: содержимое подменили, переносить нельзя.
	tamperedID, tamperedPath := legacy("d.bin", []byte("подменённое содержимое"), strings.Repeat("a", 64))
	// Строка без файла на диске.
	missingID, missingPath := legacy("e.bin", []byte("этот файл потеряли"), "")
	if err := filestore.Remove(root, missingPath); err != nil {
		t.Fatal(err)
	}

	report, err := Run(ctx, db, root, 20<<20)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total() != 5 {
		t.Fatalf("отчёт не сходится с числом вложений: %s", report)
	}
	if report.Migrated != 2 || report.Deduplicated != 1 {
		t.Fatalf("ожидались два новых blob и одна дедупликация: %s", report)
	}
	if report.Mismatched != 1 || report.Missing != 1 {
		t.Fatalf("строки для ручного разбора посчитаны неверно: %s", report)
	}

	pathOf := func(id string) string {
		t.Helper()
		var path string
		if err := db.QueryRowContext(ctx, `SELECT storage_path FROM attachments WHERE id::text=$1`, id).Scan(&path); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// Одинаковые байты сошлись в один blob, разные — в разные.
	if pathOf(firstID) != pathOf(secondID) {
		t.Fatal("одинаковое содержимое должно указывать на один blob")
	}
	if pathOf(uniqueID) == pathOf(firstID) {
		t.Fatal("разное содержимое не может делить blob")
	}
	for _, id := range []string{firstID, secondID, uniqueID} {
		path := pathOf(id)
		if !filestore.IsBlobPath(root, path) {
			t.Fatalf("вложение %s осталось вне CAS: %s", id, path)
		}
		if err := filestore.VerifyBlob(root, path); err != nil {
			t.Fatalf("перенесённый файл повреждён: %v", err)
		}
	}
	// Записанный ранее хеш сохранился: перенос не переписал сверку.
	var storedSHA string
	if err := db.QueryRowContext(ctx, `SELECT content_sha256 FROM attachments WHERE id::text=$1`, secondID).Scan(&storedSHA); err != nil {
		t.Fatal(err)
	}
	if storedSHA != hex.EncodeToString(sharedSum[:]) {
		t.Fatalf("хеш после переноса %s, ожидался прежний", storedSHA)
	}
	// Исторические файлы освобождены, кроме тех, что остались на разбор.
	for _, path := range []string{firstPath, secondPath, uniquePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("исторический файл %s должен быть убран: %v", path, err)
		}
	}
	if _, err := os.Stat(tamperedPath); err != nil {
		t.Fatalf("файл с несовпавшим хешем нельзя удалять до разбора: %v", err)
	}
	if pathOf(tamperedID) != tamperedPath || pathOf(missingID) != missingPath {
		t.Fatal("строки для ручного разбора не должны переписываться")
	}

	// Повторный прогон ничего не меняет.
	again, err := Run(ctx, db, root, 20<<20)
	if err != nil {
		t.Fatal(err)
	}
	if again.Migrated != 0 || again.Deduplicated != 0 {
		t.Fatalf("повторный перенос должен быть пустым: %s", again)
	}
	if again.AlreadyAddressed != 3 {
		t.Fatalf("повторный прогон должен признать три вложения перенесёнными: %s", again)
	}
}
