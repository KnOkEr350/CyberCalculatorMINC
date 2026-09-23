package regulatory

import (
	"errors"
	"testing"
	"time"
)

func msk(value string) time.Time {
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, BusinessLocation)
	if err != nil {
		panic(err)
	}
	return parsed
}

func must(t *testing.T, state State, action Action, now time.Time, reason string) State {
	t.Helper()
	next, event, err := Apply(state, action, Input{Now: now, Reason: reason})
	if err != nil {
		t.Fatalf("%s из %s: %v", action, state.Status, err)
	}
	if event.From != state.Status || event.To != next.Status || event.Action != action {
		t.Fatalf("событие описывает переход неверно: %+v (%s → %s)", event, state.Status, next.Status)
	}
	return next
}

// WF-03: проект соглашения — 14 дней на рассмотрение, 10 на доработку, 10 на
// повторное рассмотрение. Проверяются последние дни каждого окна.
func TestAgreementChainDurations(t *testing.T) {
	state, err := New(KindAgreement, 2026)
	if err != nil {
		t.Fatal(err)
	}
	sent := must(t, state, ActionSend, msk("2026-03-02 15:00"), "")
	if sent.Status != StatusSent || sent.Window != WindowReview || sent.Round != 1 {
		t.Fatalf("после отправки: %+v", sent)
	}
	// День получения не входит: 2 марта + 14 = 16 марта.
	if sent.DueDate != "2026-03-16" {
		t.Fatalf("срок рассмотрения %s, ожидалось 2026-03-16", sent.DueDate)
	}

	rework := must(t, sent, ActionRequestRework, msk("2026-03-10 11:00"), "нет реквизитов приложения")
	if rework.Status != StatusRework || rework.Window != WindowRework || rework.Remarks != "нет реквизитов приложения" {
		t.Fatalf("после возврата: %+v", rework)
	}
	if rework.DueDate != "2026-03-20" { // 10 марта + 10
		t.Fatalf("срок доработки %s, ожидалось 2026-03-20", rework.DueDate)
	}

	resubmitted := must(t, rework, ActionResubmit, msk("2026-03-18 09:00"), "")
	if resubmitted.Status != StatusResubmitted || resubmitted.Window != WindowReReview || resubmitted.Round != 2 {
		t.Fatalf("после повторной отправки: %+v", resubmitted)
	}
	if resubmitted.DueDate != "2026-03-28" { // 18 марта + 10
		t.Fatalf("срок повторного рассмотрения %s, ожидалось 2026-03-28", resubmitted.DueDate)
	}

	approved := must(t, resubmitted, ActionApprove, msk("2026-03-25 12:00"), "")
	if approved.Status != StatusApproved || approved.Window != WindowNone || approved.DueDate != "" {
		t.Fatalf("после согласования окно должно закрыться: %+v", approved)
	}
}

// WF-04 и WF-05: цепочки перечней — 10/10/5 и 20/20/10.
func TestListChainsMatchTheOrder(t *testing.T) {
	want := map[Kind]Chain{
		KindAgreement:   {14, 10, 10},
		KindPreliminary: {10, 10, 5},
		KindFinal:       {20, 20, 10},
	}
	for kind, chain := range want {
		if Chains[kind] != chain {
			t.Errorf("%s: цепочка %+v, ожидалась %+v", kind, Chains[kind], chain)
		}
	}
	if len(Chains) != len(want) || len(Kinds()) != len(want) {
		t.Fatalf("видов процесса %d, ожидалось %d", len(Chains), len(want))
	}

	// Предварительный: отправлен 5 ноября → 10 дней = 15 ноября.
	preliminary, _ := New(KindPreliminary, 2026)
	sent := must(t, preliminary, ActionSend, msk("2026-11-05 10:00"), "")
	if sent.DueDate != "2026-11-15" || sent.SentLate {
		t.Fatalf("предварительный перечень: срок %s, поздно=%v", sent.DueDate, sent.SentLate)
	}
	rework := must(t, sent, ActionRequestRework, msk("2026-11-06 10:00"), "уточнить строки")
	if rework.DueDate != "2026-11-16" { // 6 ноября + 10
		t.Fatalf("доработка предварительного: %s", rework.DueDate)
	}
	resubmitted := must(t, rework, ActionResubmit, msk("2026-11-10 10:00"), "")
	if resubmitted.DueDate != "2026-11-15" { // 10 ноября + 5
		t.Fatalf("повторное рассмотрение предварительного: %s", resubmitted.DueDate)
	}

	// Итоговый: отправлен 15 января 2027 → 20 дней = 4 февраля.
	final, _ := New(KindFinal, 2026)
	sentFinal := must(t, final, ActionSend, msk("2027-01-15 10:00"), "")
	if sentFinal.DueDate != "2027-02-04" {
		t.Fatalf("итоговый перечень: %s", sentFinal.DueDate)
	}
	reworkFinal := must(t, sentFinal, ActionRequestRework, msk("2027-01-20 10:00"), "замечания")
	if reworkFinal.DueDate != "2027-02-09" { // 20 января + 20
		t.Fatalf("доработка итогового: %s", reworkFinal.DueDate)
	}
	again := must(t, reworkFinal, ActionResubmit, msk("2027-02-01 10:00"), "")
	if again.DueDate != "2027-02-11" { // 1 февраля + 10
		t.Fatalf("повторное рассмотрение итогового: %s", again.DueDate)
	}
}

// WF-04: срез на 1 ноября и отправка не позднее 10 ноября.
func TestPreliminaryListCutoffAndSendDeadline(t *testing.T) {
	state, _ := New(KindPreliminary, 2026)

	// До среза перечень формировать нельзя: данных на 1 ноября ещё нет.
	if _, _, err := Apply(state, ActionSend, Input{Now: msk("2026-10-31 23:59")}); !errors.Is(err, ErrTooEarly) {
		t.Fatalf("отправка до среза: %v", err)
	}
	// В день среза можно, и это не поздно.
	onCutoff := must(t, state, ActionSend, msk("2026-11-01 00:00"), "")
	if onCutoff.SentLate {
		t.Fatal("отправка в день среза — не позднее срока")
	}
	// 10 ноября — последний день без нарушения, вплоть до полуночи по Москве.
	lastDay := must(t, state, ActionSend, msk("2026-11-10 23:59"), "")
	if lastDay.SentLate {
		t.Fatal("отправка 10 ноября — ещё в срок")
	}
	// 11 ноября — нарушение, но отправка возможна и помечена.
	late := must(t, state, ActionSend, msk("2026-11-11 00:00"), "")
	if !late.SentLate {
		t.Fatal("отправка 11 ноября должна быть помечена как поздняя")
	}
	// Московская дата, а не UTC: 10 ноября 22:00 UTC — уже 11 ноября 01:00 по Москве.
	utc := time.Date(2026, 11, 10, 22, 0, 0, 0, time.UTC)
	if got := must(t, state, ActionSend, utc, ""); !got.SentLate {
		t.Fatal("счёт идёт по московской дате: 22:00 UTC 10 ноября — это уже 11 ноября")
	}

	m := MilestonesFor(KindPreliminary, 2026)
	if !m.Has || m.Cutoff.Format("2006-01-02") != "2026-11-01" || m.SendDue.Format("2006-01-02") != "2026-11-10" {
		t.Fatalf("контрольные даты предварительного перечня: %+v", m)
	}
	final := MilestonesFor(KindFinal, 2026)
	if final.Cutoff.Format("2006-01-02") != "2026-12-31" || final.SendDue.Format("2006-01-02") != "2027-03-01" {
		t.Fatalf("контрольные даты итогового перечня: %+v", final)
	}
	if MilestonesFor(KindAgreement, 2026).Has {
		t.Fatal("у проекта соглашения нет среза и крайней даты отправки")
	}
}

func TestFinalListCutoffAndSendDeadline(t *testing.T) {
	state, _ := New(KindFinal, 2026)
	if _, _, err := Apply(state, ActionSend, Input{Now: msk("2026-12-30 12:00")}); !errors.Is(err, ErrTooEarly) {
		t.Fatalf("итоговый перечень до среза 31 декабря: %v", err)
	}
	if got := must(t, state, ActionSend, msk("2027-03-01 23:59"), ""); got.SentLate {
		t.Fatal("1 марта — ещё в срок")
	}
	if got := must(t, state, ActionSend, msk("2027-03-02 00:00"), ""); !got.SentLate {
		t.Fatal("2 марта — поздно")
	}
}

// ADR-02: согласование по молчанию наступает только после окончания последнего
// календарного дня, а не в его начале и не в его конце по чужому часовому поясу.
func TestDefaultApprovalHappensOnlyAfterTheLastDay(t *testing.T) {
	state, _ := New(KindPreliminary, 2026)
	sent := must(t, state, ActionSend, msk("2026-11-05 10:00"), "") // срок до 15 ноября

	for _, moment := range []string{"2026-11-05 10:00", "2026-11-14 12:00", "2026-11-15 00:00", "2026-11-15 23:59"} {
		if next, event := ExpireIfDue(sent, msk(moment)); event != nil || next.Status != StatusSent {
			t.Fatalf("в момент %s срок ещё идёт", moment)
		}
	}
	next, event := ExpireIfDue(sent, msk("2026-11-16 00:00"))
	if event == nil || next.Status != StatusDefaultApproved {
		t.Fatalf("с началом 16 ноября перечень согласован по молчанию: %+v %v", next, event)
	}
	if !event.System || event.Action != ActionDefaultApprove || event.From != StatusSent || event.To != StatusDefaultApproved {
		t.Fatalf("событие: %+v", event)
	}
	if next.Window != WindowNone || next.DueDate != "" {
		t.Fatal("после согласования окно закрыто")
	}
	// Идемпотентно: повторный вызов ничего не меняет и не создаёт события.
	again, secondEvent := ExpireIfDue(next, msk("2027-01-01 00:00"))
	if secondEvent != nil || again.Status != StatusDefaultApproved {
		t.Fatal("повторное согласование по молчанию должно быть пустым")
	}
}

// Молчание рецензента срабатывает в обоих окнах рассмотрения — первичном и
// повторном, — а рассмотрение «в работе» срок не останавливает.
func TestSilenceApprovesInBothReviewWindows(t *testing.T) {
	state, _ := New(KindAgreement, 2026)
	sent := must(t, state, ActionSend, msk("2026-03-02 10:00"), "")
	reviewing := must(t, sent, ActionStartReview, msk("2026-03-03 10:00"), "")
	if reviewing.DueDate != sent.DueDate {
		t.Fatal("начало рассмотрения не должно продлевать срок")
	}
	if next, event := ExpireIfDue(reviewing, msk("2026-03-17 00:00")); event == nil || next.Status != StatusDefaultApproved {
		t.Fatal("рассмотрение «в работе» не должно останавливать срок")
	}

	rework := must(t, sent, ActionRequestRework, msk("2026-03-05 10:00"), "замечание")
	resubmitted := must(t, rework, ActionResubmit, msk("2026-03-06 10:00"), "")
	if next, event := ExpireIfDue(resubmitted, msk("2026-03-17 00:00")); event == nil || next.Status != StatusDefaultApproved {
		t.Fatalf("молчание при повторном рассмотрении: %+v", next)
	}
}

// Пропуск срока доработки — просрочка, а не автоматический исход: Приказ говорит
// об этом на уровне позиций, поэтому процесс остаётся в доработке, событие
// записывается один раз, а повторная отправка после срока не проходит.
func TestReworkLapseIsRecordedOnceAndClosesResubmission(t *testing.T) {
	state, _ := New(KindPreliminary, 2026)
	sent := must(t, state, ActionSend, msk("2026-11-05 10:00"), "")
	rework := must(t, sent, ActionRequestRework, msk("2026-11-06 10:00"), "замечание") // срок до 16 ноября

	if _, event := ExpireIfDue(rework, msk("2026-11-16 23:59")); event != nil {
		t.Fatal("в последний день срока доработки просрочки ещё нет")
	}
	lapsed, event := ExpireIfDue(rework, msk("2026-11-17 00:00"))
	if event == nil || event.Action != ActionReworkLapsed || !event.System {
		t.Fatalf("пропуск доработки должен фиксироваться: %+v", event)
	}
	if lapsed.Status != StatusRework || lapsed.ReworkLapsedAt == nil {
		t.Fatalf("состояние после пропуска: %+v", lapsed)
	}
	if _, again := ExpireIfDue(lapsed, msk("2026-11-20 00:00")); again != nil {
		t.Fatal("пропуск фиксируется один раз")
	}
	if _, _, err := Apply(lapsed, ActionResubmit, Input{Now: msk("2026-11-17 09:00")}); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("повторная отправка после срока: %v", err)
	}
	// В последний день срока отправить ещё можно.
	if _, _, err := Apply(rework, ActionResubmit, Input{Now: msk("2026-11-16 23:59")}); err != nil {
		t.Fatalf("в последний день срока: %v", err)
	}
}

// Допустимые переходы заданы таблицей: всё, чего в ней нет, отклоняется, а
// завершённый процесс не принимает никаких действий.
func TestTransitionTable(t *testing.T) {
	now := msk("2026-03-02 10:00")
	base, _ := New(KindAgreement, 2026)
	sent := must(t, base, ActionSend, now, "")
	inReview := must(t, sent, ActionStartReview, now, "")
	rework := must(t, sent, ActionRequestRework, now, "замечание")
	resubmitted := must(t, rework, ActionResubmit, now, "")
	inReview2 := must(t, resubmitted, ActionStartReview, now, "")
	approved := must(t, sent, ActionApprove, now, "")
	defaulted, _ := ExpireIfDue(sent, msk("2027-01-01 00:00"))
	disputed := must(t, resubmitted, ActionDispute, now, "не согласны")

	allowed := map[string]struct {
		state State
		want  []Action
	}{
		"draft":             {base, []Action{ActionSend}},
		"sent":              {sent, []Action{ActionStartReview, ActionApprove, ActionRequestRework}},
		"in_review":         {inReview, []Action{ActionApprove, ActionRequestRework}},
		"rework":            {rework, []Action{ActionResubmit}},
		"resubmitted":       {resubmitted, []Action{ActionStartReview, ActionApprove, ActionDispute}},
		"in_review, круг 2": {inReview2, []Action{ActionApprove, ActionDispute}},
		"approved":          {approved, nil},
		"default_approved":  {defaulted, nil},
		"disputed":          {disputed, nil},
	}
	for name, tc := range allowed {
		got := Allowed(tc.state, now)
		if len(got) != len(tc.want) {
			t.Errorf("%s: допустимы %v, ожидалось %v", name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: допустимы %v, ожидалось %v", name, got, tc.want)
				break
			}
		}
	}

	// Завершённый процесс не принимает ничего.
	for _, terminal := range []State{approved, defaulted, disputed} {
		for _, action := range Actions() {
			if _, _, err := Apply(terminal, action, Input{Now: now, Reason: "причина"}); !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s: действие %s над завершённым процессом: %v", terminal.Status, action, err)
			}
		}
	}
	if _, _, err := Apply(sent, Action("что-то"), Input{Now: now}); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("неизвестное действие: %v", err)
	}
}

// Возврат на доработку — один раз: цепочка заканчивается согласованием или
// оспариванием, а не бесконечным круговоротом.
func TestOnlyOneReworkCycle(t *testing.T) {
	now := msk("2026-03-02 10:00")
	state, _ := New(KindAgreement, 2026)
	sent := must(t, state, ActionSend, now, "")
	rework := must(t, sent, ActionRequestRework, now, "замечание")
	resubmitted := must(t, rework, ActionResubmit, now, "")
	if _, _, err := Apply(resubmitted, ActionRequestRework, Input{Now: now, Reason: "ещё замечание"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("второй возврат на доработку: %v", err)
	}
	reviewing := must(t, resubmitted, ActionStartReview, now, "")
	if _, _, err := Apply(reviewing, ActionRequestRework, Input{Now: now, Reason: "ещё замечание"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("второй возврат на доработку из рассмотрения: %v", err)
	}
}

// Замечания и причина оспаривания обязательны: решение без причины нечем
// проверить.
func TestReasonsAreRequired(t *testing.T) {
	now := msk("2026-03-02 10:00")
	state, _ := New(KindAgreement, 2026)
	sent := must(t, state, ActionSend, now, "")
	if _, _, err := Apply(sent, ActionRequestRework, Input{Now: now, Reason: "   "}); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("возврат без замечаний: %v", err)
	}
	rework := must(t, sent, ActionRequestRework, now, "замечание")
	resubmitted := must(t, rework, ActionResubmit, now, "")
	if _, _, err := Apply(resubmitted, ActionDispute, Input{Now: now, Reason: ""}); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("оспаривание без причины: %v", err)
	}
	// Отклонённое действие состояние не меняет.
	if sent.Status != StatusSent || resubmitted.Status != StatusResubmitted {
		t.Fatal("отклонённое действие изменило состояние")
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	if _, err := New(Kind("нет-такого"), 2026); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("неизвестный вид: %v", err)
	}
	for _, year := range []int{0, 1999, 2101} {
		if _, err := New(KindFinal, year); err == nil {
			t.Errorf("год %d должен отклоняться", year)
		}
	}
	if _, _, err := Apply(State{Kind: "нет-такого", Status: StatusDraft}, ActionSend, Input{Now: time.Now()}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("процесс неизвестного вида: %v", err)
	}
}

// ADR-02: правило счёта дней — те же граничные случаи, что и у обработчиков.
func TestCalendarDeadlineBoundaries(t *testing.T) {
	cases := []struct {
		received string
		days     int
		lastDay  string
	}{
		{"2026-12-22 10:00", 10, "2027-01-01"}, // праздник не переносит срок
		{"2028-02-24 10:00", 5, "2028-02-29"},  // високосный год
		{"2027-02-24 10:00", 5, "2027-03-01"},
		{"2026-11-10 23:59", 10, "2026-11-20"}, // час получения не важен
		{"2026-11-10 00:00", 10, "2026-11-20"},
	}
	for _, tc := range cases {
		if last, _ := CalendarDeadline(msk(tc.received), tc.days); last != tc.lastDay {
			t.Errorf("%s + %d: последний день %s, ожидался %s", tc.received, tc.days, last, tc.lastDay)
		}
	}
	// 22:00 UTC — уже следующая московская дата.
	utc := time.Date(2026, 11, 10, 22, 0, 0, 0, time.UTC)
	if last, _ := CalendarDeadline(utc, 10); last != "2026-11-21" {
		t.Errorf("22:00 UTC 10 ноября = 11 ноября по Москве: последний день %s", last)
	}
	_, expires := CalendarDeadline(msk("2026-11-10 12:00"), 10)
	if DeadlineExpired(expires, msk("2026-11-20 23:59")) || !DeadlineExpired(expires, msk("2026-11-21 00:00")) {
		t.Error("срок истекает ровно в полночь по Москве")
	}
	if got := Today(msk("2026-11-10 23:59")); got.Format("2006-01-02") != "2026-11-10" {
		t.Errorf("Today: %s", got)
	}
}
