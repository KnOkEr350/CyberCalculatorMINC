package regulatory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"cybercalc/internal/dbx"
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

type storeScenario struct {
	admin, agreement string
}

// newAgreement заводит соглашение и пользователя. Каждый тест берёт своё:
// процесс уникален по (вид, соглашение, год), а база общая.
func newAgreement(t *testing.T, ctx context.Context, db *sql.DB) storeScenario {
	t.Helper()
	f := testfixtures.New(db, t.Name())
	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: admin.ID})
	if err != nil {
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
	return storeScenario{admin: admin.ID, agreement: agreement.ID}
}

// Процесс переживает перезапуск: состояние и срок лежат в БД, повторное
// создание идемпотентно, а вся цепочка оставляет историю с автором.
func TestProcessChainIsPersistedWithHistory(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)

	created, err := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)
	if err != nil {
		t.Fatal(err)
	}
	if created.State.Status != StatusDraft || created.State.Round != 0 {
		t.Fatalf("новый процесс: %+v", created.State)
	}
	again, err := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)
	if !errors.Is(err, ErrExists) || again.ID != created.ID {
		t.Fatalf("повторное создание должно вернуть тот же процесс: %v %s/%s", err, again.ID, created.ID)
	}
	// Другой вид и другой год — отдельные процессы.
	other, err := Create(ctx, db, KindFinal, s.agreement, 2026, s.admin)
	if err != nil || other.ID == created.ID {
		t.Fatalf("другой вид — другой процесс: %v", err)
	}

	process, events, err := Act(ctx, db, created.ID, ActionSend, s.admin, "", msk("2026-03-02 15:00"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if process.State.Status != StatusSent || process.State.DueDate != "2026-03-16" || len(events) != 1 {
		t.Fatalf("после отправки: %+v", process.State)
	}
	if _, _, err := Act(ctx, db, created.ID, ActionRequestRework, s.admin, "замечание к разделу 3", msk("2026-03-05 10:00"), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Act(ctx, db, created.ID, ActionResubmit, s.admin, "", msk("2026-03-08 10:00"), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Act(ctx, db, created.ID, ActionApprove, s.admin, "", msk("2026-03-10 10:00"), nil); err != nil {
		t.Fatal(err)
	}

	// Состояние читается из БД так, как записано.
	loaded, err := Get(ctx, db, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Status != StatusApproved || loaded.State.Window != WindowNone || loaded.State.Round != 2 {
		t.Fatalf("итоговое состояние: %+v", loaded.State)
	}
	history, err := Events(ctx, db, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		action   Action
		from, to Status
	}{
		{ActionSend, StatusDraft, StatusSent},
		{ActionRequestRework, StatusSent, StatusRework},
		{ActionResubmit, StatusRework, StatusResubmitted},
		{ActionApprove, StatusResubmitted, StatusApproved},
	}
	if len(history) != len(want) {
		t.Fatalf("в истории %d переходов, ожидалось %d", len(history), len(want))
	}
	for i, step := range want {
		got := history[i]
		if got.Action != step.action || got.From != step.from || got.To != step.to || got.ActorID != s.admin || got.System {
			t.Errorf("переход %d: %+v", i, got)
		}
	}
	if history[1].Reason != "замечание к разделу 3" || history[1].DueDate != "2026-03-15" {
		t.Errorf("замечание и срок доработки должны сохраняться: %+v", history[1])
	}

	// История только дополняется.
	if _, err := db.ExecContext(ctx, `UPDATE regulatory_process_events SET reason='подмена' WHERE process_id::text=$1`, created.ID); err == nil {
		t.Fatal("историю переходов нельзя переписывать")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM regulatory_process_events WHERE process_id::text=$1`, created.ID); err == nil {
		t.Fatal("историю переходов нельзя удалять")
	}
}

// WF-04 / WF-05: согласование по молчанию выполняет фоновое задание — только
// после срока, без человека, один раз, и переход виден как действие системы.
func TestSilenceApprovalIsAutomaticAndIdempotent(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)

	silent, _ := Create(ctx, db, KindPreliminary, s.agreement, 2026, s.admin)
	if _, _, err := Act(ctx, db, silent.ID, ActionSend, s.admin, "", msk("2026-11-05 10:00"), nil); err != nil {
		t.Fatal(err)
	}
	// Для другого процесса срок ещё идёт: задание не должно его трогать.
	running, _ := Create(ctx, db, KindFinal, s.agreement, 2026, s.admin)
	if _, _, err := Act(ctx, db, running.ID, ActionSend, s.admin, "", msk("2027-01-20 10:00"), nil); err != nil {
		t.Fatal(err)
	}

	// В последний день срока (15 ноября) задание ничего не меняет.
	if approved, _, err := ExpireDue(ctx, db, msk("2026-11-15 23:59"), s.agreement); err != nil || approved != 0 {
		t.Fatalf("в последний день срока перевода быть не должно: approved=%d err=%v", approved, err)
	}
	// С началом 16 ноября процесс согласован по молчанию.
	approved, _, err := ExpireDue(ctx, db, msk("2026-11-16 00:00"), s.agreement)
	if err != nil {
		t.Fatal(err)
	}
	if approved != 1 {
		t.Fatalf("задание должно перевести ровно один процесс (предварительный), перевело %d", approved)
	}
	loaded, _ := Get(ctx, db, silent.ID)
	if loaded.State.Status != StatusDefaultApproved || loaded.State.Window != WindowNone {
		t.Fatalf("после молчания: %+v", loaded.State)
	}
	history, _ := Events(ctx, db, silent.ID)
	last := history[len(history)-1]
	if !last.System || last.ActorID != "" || last.Action != ActionDefaultApprove || last.To != StatusDefaultApproved {
		t.Fatalf("согласование по молчанию должно быть действием системы: %+v", last)
	}
	if untouched, _ := Get(ctx, db, running.ID); untouched.State.Status != StatusSent {
		t.Fatalf("процесс с идущим сроком не должен затрагиваться: %s", untouched.State.Status)
	}

	// Повторный проход ничего не меняет и не добавляет событий.
	before, _ := Events(ctx, db, silent.ID)
	if _, _, err := ExpireDue(ctx, db, msk("2026-12-31 00:00"), s.agreement); err != nil {
		t.Fatal(err)
	}
	after, _ := Events(ctx, db, silent.ID)
	if len(before) != len(after) {
		t.Fatalf("повторный проход добавил события: %d → %d", len(before), len(after))
	}
}

// Пропуск срока доработки фиксируется один раз и не забивает выборку
// просроченных процессов: иначе постоянно просроченная доработка вытесняла бы
// остальные.
func TestReworkLapseIsRecordedOnceByTheWorker(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)

	p, _ := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)
	for _, step := range []struct {
		action Action
		at     string
		reason string
	}{{ActionSend, "2026-03-02 10:00", ""}, {ActionRequestRework, "2026-03-05 10:00", "замечание"}} {
		if _, _, err := Act(ctx, db, p.ID, step.action, s.admin, step.reason, msk(step.at), nil); err != nil {
			t.Fatal(err)
		}
	}
	// Срок доработки — до 15 марта; 16-го задание фиксирует пропуск.
	if _, lapsed, err := ExpireDue(ctx, db, msk("2026-03-16 00:00"), s.agreement); err != nil || lapsed != 1 {
		t.Fatalf("пропуск доработки должен фиксироваться: lapsed=%d err=%v", lapsed, err)
	}
	if _, lapsed, err := ExpireDue(ctx, db, msk("2026-03-20 00:00"), s.agreement); err != nil || lapsed != 0 {
		t.Fatalf("повторно пропуск не фиксируется: lapsed=%d err=%v", lapsed, err)
	}
	history, _ := Events(ctx, db, p.ID)
	lapses := 0
	for _, event := range history {
		if event.Action == ActionReworkLapsed {
			lapses++
			if !event.System || event.From != StatusRework || event.To != StatusRework {
				t.Errorf("событие пропуска: %+v", event)
			}
		}
	}
	if lapses != 1 {
		t.Fatalf("событий пропуска %d, ожидалось одно", lapses)
	}
	// Процесс остаётся в доработке; повторная отправка после срока не проходит.
	if _, _, err := Act(ctx, db, p.ID, ActionResubmit, s.admin, "", msk("2026-03-17 10:00"), nil); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("повторная отправка после срока: %v", err)
	}
	if loaded, _ := Get(ctx, db, p.ID); loaded.State.Status != StatusRework {
		t.Fatalf("состояние: %s", loaded.State.Status)
	}
}

// Действие, пришедшее после истёкшего срока, застаёт уже согласованный по
// молчанию процесс: перевод сохраняется, а само действие отклоняется.
func TestActionAfterSilenceSeesTheDefaultApproval(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)

	p, _ := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)
	if _, _, err := Act(ctx, db, p.ID, ActionSend, s.admin, "", msk("2026-03-02 10:00"), nil); err != nil {
		t.Fatal(err)
	}
	// Рецензент отвечает 17 марта — на следующий день после срока (до 16-го).
	_, applied, err := Act(ctx, db, p.ID, ActionRequestRework, s.admin, "поздние замечания", msk("2026-03-17 09:00"), nil)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ответ после срока: %v", err)
	}
	if len(applied) != 1 || applied[0].Action != ActionDefaultApprove {
		t.Fatalf("молчание, случившееся до ответа, должно сохраниться: %+v", applied)
	}
	if loaded, _ := Get(ctx, db, p.ID); loaded.State.Status != StatusDefaultApproved {
		t.Fatalf("состояние: %s", loaded.State.Status)
	}
	if _, _, err := Act(ctx, db, "00000000-0000-0000-0000-000000000000", ActionSend, s.admin, "", msk("2026-03-02 10:00"), nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("несуществующий процесс: %v", err)
	}
}

// Два ответа рецензента одновременно: выигрывает один, второй видит уже
// завершённый процесс. История не содержит двух согласований.
func TestConcurrentAnswersAreSerialized(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)

	p, _ := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)
	if _, _, err := Act(ctx, db, p.ID, ActionSend, s.admin, "", msk("2026-03-02 10:00"), nil); err != nil {
		t.Fatal(err)
	}
	const attempts = 8
	var wg sync.WaitGroup
	results := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, results[i] = Act(ctx, db, p.ID, ActionApprove, s.admin, "", msk("2026-03-03 10:00"), nil)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrInvalidTransition):
		default:
			t.Fatalf("неожиданная ошибка: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("согласование выиграло %d раз, должно быть ровно один", wins)
	}
	history, _ := Events(ctx, db, p.ID)
	approvals := 0
	for _, event := range history {
		if event.Action == ActionApprove {
			approvals++
		}
	}
	if approvals != 1 {
		t.Fatalf("в истории %d согласований", approvals)
	}
}

// Хук выполняется в той же транзакции: его сбой откатывает и сам переход.
func TestHookFailureRollsTheTransitionBack(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)

	p, _ := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)
	boom := errors.New("сбой аудита")
	_, _, err := Act(ctx, db, p.ID, ActionSend, s.admin, "", msk("2026-03-02 10:00"),
		func(context.Context, *sql.Tx, []StoredEvent) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("ошибка хука должна возвращаться: %v", err)
	}
	loaded, _ := Get(ctx, db, p.ID)
	history, _ := Events(ctx, db, p.ID)
	if loaded.State.Status != StatusDraft || len(history) != 0 {
		t.Fatalf("сбой аудита должен откатывать переход: %s, событий %d", loaded.State.Status, len(history))
	}
}

// Ограничения БД страхуют автомат: окно без срока и завершённый процесс с
// открытым окном невозможны даже при прямой записи.
func TestDatabaseConstraintsGuardTheStates(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	s := newAgreement(t, ctx, db)
	p, _ := Create(ctx, db, KindAgreement, s.agreement, 2026, s.admin)

	for name, statement := range map[string]string{
		"окно без срока":         `UPDATE regulatory_processes SET window_kind='review' WHERE id::text=$1`,
		"срок без окна":          `UPDATE regulatory_processes SET due_date=DATE '2026-03-16',expires_at=now() WHERE id::text=$1`,
		"неизвестный статус":     `UPDATE regulatory_processes SET status='почти' WHERE id::text=$1`,
		"черновик с кругом":      `UPDATE regulatory_processes SET round=1 WHERE id::text=$1`,
		"отправленный без круга": `UPDATE regulatory_processes SET status='sent' WHERE id::text=$1`,
		"неизвестный вид":        `UPDATE regulatory_processes SET kind='другой' WHERE id::text=$1`,
	} {
		if _, err := db.ExecContext(ctx, statement, p.ID); err == nil {
			t.Errorf("%s: должно отклоняться", name)
		}
	}
	// Событие человека без автора и событие системы с автором невозможны.
	if _, err := db.ExecContext(ctx, `INSERT INTO regulatory_process_events(process_id,action,from_status,to_status,system_action)
		VALUES($1::uuid,'approve','sent','approved',false)`, p.ID); err == nil {
		t.Error("событие человека без автора должно отклоняться")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO regulatory_process_events(process_id,action,from_status,to_status,actor_id,system_action)
		VALUES($1::uuid,'default_approve','sent','default_approved',$2::uuid,true)`, p.ID, s.admin); err == nil {
		t.Error("событие системы с автором должно отклоняться")
	}
}
