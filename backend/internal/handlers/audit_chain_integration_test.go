package handlers

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	platformaudit "cybercalc/internal/platform/audit"
	"cybercalc/internal/testfixtures"
)

func writeAuditEvent(t *testing.T, ctx context.Context, db *sql.DB, action, entity string) {
	t.Helper()
	if err := platformaudit.Write(ctx, db, platformaudit.Event{
		Actor:  platformaudit.Actor{Type: platformaudit.ActorSystem},
		Action: action,
		Entity: platformaudit.Entity{Type: entity},
		After:  map[string]string{"marker": t.Name()},
	}); err != nil {
		t.Fatal(err)
	}
}

func chainProblem(t *testing.T, ctx context.Context, db *sql.DB) (int64, string) {
	t.Helper()
	var seq sql.NullInt64
	var problem sql.NullString
	err := db.QueryRowContext(ctx, `SELECT chain_seq,problem FROM verify_audit_chain()`).Scan(&seq, &problem)
	if err == sql.ErrNoRows {
		return 0, ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return seq.Int64, problem.String
}

// AUDIT-02 на реальной БД: каждая запись закрывает предыдущую. Запрет из 0901
// не видит пропажу записи целиком — например, при восстановлении таблицы из
// подменённой копии; цепочка это обнаруживает.
func TestAuditChainDetectsMissingAndAlteredRecords(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		writeAuditEvent(t, ctx, db, "create", "entry")
	}
	if _, problem := chainProblem(t, ctx, db); problem != "" {
		t.Fatalf("свежая цепочка должна быть целой: %s", problem)
	}

	// Звенья связаны: prev_hash каждой записи — это row_hash предыдущей.
	// Цепь идёт сквозь архив, поэтому обе части читаются одной последовательностью.
	rows, err := db.QueryContext(ctx, `SELECT chain_seq,prev_hash,row_hash FROM audit_log_archive
		UNION ALL SELECT chain_seq,prev_hash,row_hash FROM audit_log ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	// В общей тестовой базе журнал уже не пуст, поэтому сравниваются соседние
	// звенья, а не связь с началом цепи.
	previous := ""
	links := 0
	for rows.Next() {
		var seq int64
		var prev sql.NullString
		var row string
		if err := rows.Scan(&seq, &prev, &row); err != nil {
			t.Fatal(err)
		}
		if links > 0 && prev.String != previous {
			t.Fatalf("звено %d не ссылается на предыдущую запись", links)
		}
		previous = row
		links++
	}
	if links < 5 {
		t.Fatalf("в цепочке %d звеньев, ожидалось не меньше пяти", links)
	}

	// Пропажа записи в обход триггеров: так выглядит восстановление из
	// подменённой резервной копии. Ломаем цепь внутри транзакции и
	// откатываем её — иначе испорченный журнал достался бы соседним тестам.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL cybercalc.audit_purge='on'`); err != nil {
		t.Fatal(err)
	}
	var victim int64
	if err := tx.QueryRowContext(ctx, `SELECT chain_seq FROM audit_log ORDER BY chain_seq DESC OFFSET 2 LIMIT 1`).Scan(&victim); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM audit_log WHERE chain_seq=$1`, victim); err != nil {
		t.Fatal(err)
	}
	var brokenSeq sql.NullInt64
	var problem sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT chain_seq,problem FROM verify_audit_chain()`).Scan(&brokenSeq, &problem); err != nil {
		t.Fatalf("пропажа записи должна ломать цепочку: %v", err)
	}
	if problem.String != "пропущено звено" {
		t.Fatalf("ожидался пропуск звена, получено %q", problem.String)
	}
	if brokenSeq.Int64 != victim+1 {
		t.Fatalf("нарушение найдено на звене %d, ожидалось %d", brokenSeq.Int64, victim+1)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// После отката журнал снова цел: тест не оставляет следов.
	if _, problem := chainProblem(t, ctx, db); problem != "" {
		t.Fatalf("после отката цепочка должна быть целой: %s", problem)
	}
}

// AUDIT-03 на реальной БД: очистка по сроку хранения переносит записи в архив,
// и цепочка остаётся сплошной — архив не разрушает неизменяемость, а активная
// часть журнала не теряет связь с прошлым.
func TestAuditArchiveKeepsChainIntact(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		writeAuditEvent(t, ctx, db, "update", "settings")
	}
	// Состарим часть записей, чтобы очистка их забрала.
	if _, err := db.ExecContext(ctx, `INSERT INTO audit_log(actor_type,action,entity_type,request_id,created_at)
		SELECT 'system','delete','entry',$1,now()-interval '400 days'`, "old:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	writeAuditEvent(t, ctx, db, "create", "partner")

	var purged int64
	if err := db.QueryRowContext(ctx, `SELECT purge_expired_audit()`).Scan(&purged); err != nil {
		t.Fatal(err)
	}
	if purged < 1 {
		t.Fatal("просроченная запись должна уходить в архив")
	}
	var archived int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log_archive`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived < 1 {
		t.Fatal("архив должен хранить вынесенные записи")
	}
	if _, problem := chainProblem(t, ctx, db); problem != "" {
		t.Fatalf("после архивирования цепочка должна оставаться целой: %s", problem)
	}

	// Архив тоже только дополняется.
	if _, err := db.ExecContext(ctx, `UPDATE audit_log_archive SET action='подмена'`); err == nil {
		t.Fatal("архив не должен принимать UPDATE")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM audit_log_archive`); err == nil {
		t.Fatal("архив не должен принимать DELETE")
	}
	// Новая запись после очистки продолжает ту же цепь.
	writeAuditEvent(t, ctx, db, "update", "entry")
	if _, problem := chainProblem(t, ctx, db); problem != "" {
		t.Fatalf("запись после очистки должна продолжать цепь: %s", problem)
	}
}

// AUDIT-04 на реальной БД: выгрузка отдаёт журнал построчно, включает архив,
// несёт звенья цепи для самостоятельной проверки и завершается итогом.
func TestAuditExportStreamsVerifiedRecords(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 4; i++ {
		writeAuditEvent(t, ctx, db, "create", "export_probe")
	}
	handlers := AdminHandlers{DB: db}
	user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization}

	recorder := httptest.NewRecorder()
	handlers.ExportAuditLog(recorder, httptest.NewRequest("GET", "/api/admin/logs/export?entity_type=export_probe", nil), user)
	if recorder.Code != 200 {
		t.Fatalf("выгрузка вернула %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/x-ndjson") {
		t.Fatalf("выгрузка должна быть построчной, получено %q", contentType)
	}

	scanner := bufio.NewScanner(strings.NewReader(recorder.Body.String()))
	records := 0
	var summary map[string]interface{}
	previous := ""
	for scanner.Scan() {
		var item map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("строка выгрузки не является JSON: %v", err)
		}
		if _, isSummary := item["summary"]; isSummary {
			summary = item
			continue
		}
		if summary != nil {
			t.Fatal("итог должен быть последней строкой выгрузки")
		}
		if item["entity_type"] != "export_probe" {
			t.Fatalf("фильтр по разделу не сработал: %v", item["entity_type"])
		}
		hash, _ := item["row_hash"].(string)
		if len(hash) != 64 {
			t.Fatalf("в выгрузке должно быть звено цепи, получено %q", hash)
		}
		if previous != "" {
			if prev, _ := item["prev_hash"].(string); prev != previous {
				t.Fatal("выгруженные записи должны сохранять связь звеньев")
			}
		}
		previous = hash
		records++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if records != 4 {
		t.Fatalf("выгружено %d записей, ожидалось 4", records)
	}
	if summary == nil {
		t.Fatal("без завершающей строки нельзя отличить полную выгрузку от оборванной")
	}
	if summary["chain_verified"] != true || summary["last_row_hash"] != previous {
		t.Fatalf("итог выгрузки не сходится с потоком: %v", summary)
	}

	// Фильтр по разделу, которого нет, даёт пустую, но завершённую выгрузку.
	empty := httptest.NewRecorder()
	handlers.ExportAuditLog(empty, httptest.NewRequest("GET", "/api/admin/logs/export?entity_type=нет-такого", nil), user)
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), `"summary":true`) {
		t.Fatalf("пустая выгрузка должна завершаться итогом: %d %s", empty.Code, empty.Body.String())
	}
}

// Выгрузка не выдаётся, пока целостность журнала не подтверждена: смысл
// выгрузки в том, что её можно предъявить.
func TestAuditExportRefusesBrokenChain(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		writeAuditEvent(t, ctx, db, "create", "broken_probe")
	}

	// Обработчик работает с *sql.DB, поэтому нарушение создаётся в отдельном
	// пуле из одного соединения: незавершённая транзакция видна всем запросам
	// этого пула и исчезает при откате, не задевая остальные тесты.
	probe, err := sql.Open("postgres", os.Getenv("TEST_DATABASE_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	probe.SetMaxOpenConns(1)
	if _, err := probe.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatal(err)
	}
	defer probe.ExecContext(context.Background(), `ROLLBACK`)
	if _, err := probe.ExecContext(ctx, `SET LOCAL cybercalc.audit_purge='on'`); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.ExecContext(ctx,
		`DELETE FROM audit_log WHERE chain_seq=(SELECT chain_seq FROM audit_log ORDER BY chain_seq DESC OFFSET 1 LIMIT 1)`); err != nil {
		t.Fatal(err)
	}

	handlers := AdminHandlers{DB: probe}
	user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization}
	recorder := httptest.NewRecorder()
	handlers.ExportAuditLog(recorder, httptest.NewRequest("GET", "/api/admin/logs/export", nil), user)
	if recorder.Code != 409 {
		t.Fatalf("при нарушенной цепочке выгрузка должна отказывать, получено %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "целостность журнала нарушена") {
		t.Fatalf("отказ должен называть причину: %s", recorder.Body.String())
	}

	if _, err := probe.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	// После отката выгрузка снова доступна.
	restored := httptest.NewRecorder()
	handlers.ExportAuditLog(restored, httptest.NewRequest("GET", "/api/admin/logs/export?entity_type=broken_probe", nil), user)
	if restored.Code != 200 {
		t.Fatalf("после отката выгрузка должна работать, получено %d: %s", restored.Code, restored.Body.String())
	}
}
