package regulatory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind — вид регламентного процесса.
type Kind string

const (
	// KindAgreement — проект соглашения: 14 дней на рассмотрение, 10 на
	// доработку, 10 на повторное рассмотрение.
	KindAgreement Kind = "agreement"
	// KindPreliminary — предварительный перечень: срез на 1 ноября, отправка
	// не позднее 10 ноября, цепочка 10/10/5.
	KindPreliminary Kind = "preliminary"
	// KindFinal — итоговый перечень: срез на 31 декабря, отправка не позднее
	// 1 марта следующего года, цепочка 20/20/10.
	KindFinal Kind = "final"
)

// Chain — длительность трёх окон в календарных днях: рассмотрение, доработка
// автором и повторное рассмотрение.
type Chain struct {
	Review   int
	Rework   int
	ReReview int
}

// Chains — цепочки Приказа № 270 (ADR-02).
var Chains = map[Kind]Chain{
	KindAgreement:   {Review: 14, Rework: 10, ReReview: 10},
	KindPreliminary: {Review: 10, Rework: 10, ReReview: 5},
	KindFinal:       {Review: 20, Rework: 20, ReReview: 10},
}

// Kinds перечисляет виды в стабильном порядке.
func Kinds() []Kind { return []Kind{KindAgreement, KindPreliminary, KindFinal} }

// Status — состояние процесса (WF-01).
type Status string

const (
	StatusDraft           Status = "draft"            // подготовлен, не отправлен
	StatusSent            Status = "sent"             // отправлен, идёт срок рассмотрения
	StatusInReview        Status = "in_review"        // принят к рассмотрению
	StatusRework          Status = "rework"           // возвращён на доработку
	StatusResubmitted     Status = "resubmitted"      // отправлен повторно
	StatusApproved        Status = "approved"         // согласован явно
	StatusDefaultApproved Status = "default_approved" // согласован по молчанию
	StatusDisputed        Status = "disputed"         // оспорен после доработки
)

// Terminal — из этих состояний процесс не выходит.
func (s Status) Terminal() bool {
	return s == StatusApproved || s == StatusDefaultApproved || s == StatusDisputed
}

// Window — какое окно срока сейчас идёт.
type Window string

const (
	WindowNone     Window = ""
	WindowReview   Window = "review"
	WindowRework   Window = "rework"
	WindowReReview Window = "rereview"
)

// Action — действие над процессом.
type Action string

const (
	ActionSend          Action = "send"
	ActionStartReview   Action = "start_review"
	ActionApprove       Action = "approve"
	ActionRequestRework Action = "request_rework"
	ActionResubmit      Action = "resubmit"
	ActionDispute       Action = "dispute"
)

// Actions перечисляет действия в стабильном порядке.
func Actions() []Action {
	return []Action{ActionSend, ActionStartReview, ActionApprove, ActionRequestRework, ActionResubmit, ActionDispute}
}

// ExpireAction — служебное действие: согласование по молчанию.
const (
	ActionDefaultApprove Action = "default_approve"
	ActionReworkLapsed   Action = "rework_lapsed"
)

var (
	ErrUnknownKind       = errors.New("неизвестный вид процесса")
	ErrInvalidTransition = errors.New("действие недопустимо в текущем состоянии")
	ErrReasonRequired    = errors.New("для этого действия обязательна причина")
	ErrTooEarly          = errors.New("срез для этого перечня ещё не наступил")
	ErrWindowClosed      = errors.New("срок доработки истёк")
)

// State — состояние процесса. Все сроки — календарные даты по Москве.
type State struct {
	Kind       Kind
	ReportYear int
	Status     Status
	Window     Window
	// DueDate — последний день текущего окна; ExpiresAt — момент, с которого
	// окно считается истёкшим (начало следующего дня по Москве).
	DueDate   string
	ExpiresAt time.Time
	// Round: 0 — до отправки, 1 — первичное рассмотрение, 2 — после доработки.
	Round  int
	SentAt *time.Time
	// SentLate — перечень отправлен позже установленной даты.
	SentLate bool
	Remarks  string
	// ReworkLapsedAt фиксирует пропуск окна доработки (идемпотентно).
	ReworkLapsedAt *time.Time
}

// Milestones — контрольные даты вида: срез и крайний срок отправки. У проекта
// соглашения их нет.
type Milestones struct {
	Cutoff  time.Time // с этой даты перечень может быть отправлен
	SendDue time.Time // не позднее этой даты
	Has     bool
}

// MilestonesFor возвращает контрольные даты вида для отчётного года.
func MilestonesFor(kind Kind, reportYear int) Milestones {
	date := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 0, 0, 0, 0, BusinessLocation)
	}
	switch kind {
	case KindPreliminary:
		return Milestones{Cutoff: date(reportYear, time.November, 1), SendDue: date(reportYear, time.November, 10), Has: true}
	case KindFinal:
		return Milestones{Cutoff: date(reportYear, time.December, 31), SendDue: date(reportYear+1, time.March, 1), Has: true}
	}
	return Milestones{}
}

// New создаёт процесс в состоянии «черновик».
func New(kind Kind, reportYear int) (State, error) {
	if _, ok := Chains[kind]; !ok {
		return State{}, ErrUnknownKind
	}
	if reportYear < 2000 || reportYear > 2100 {
		return State{}, fmt.Errorf("некорректный отчётный год %d", reportYear)
	}
	return State{Kind: kind, ReportYear: reportYear, Status: StatusDraft}, nil
}

// Event — запись о переходе. System отмечает действие, выполненное самой
// системой (согласование по молчанию), а не человеком.
type Event struct {
	Action Action
	From   Status
	To     Status
	Reason string
	// DueDate — срок окна, открытого этим переходом.
	DueDate string
	At      time.Time
	System  bool
}

// Input — контекст действия.
type Input struct {
	Now    time.Time
	Reason string
}

// Apply выполняет действие. Перед ним вызывающий обязан применить
// ExpireIfDue: действие над процессом, у которого срок уже истёк молчанием,
// должно видеть его согласованным, а не рассматриваемым.
func Apply(state State, action Action, in Input) (State, Event, error) {
	reason := strings.TrimSpace(in.Reason)
	fail := func(format string, args ...interface{}) (State, Event, error) {
		return state, Event{}, fmt.Errorf("%w: %s", ErrInvalidTransition, fmt.Sprintf(format, args...))
	}
	if state.Status.Terminal() {
		return fail("процесс уже завершён (%s)", state.Status)
	}
	chain, ok := Chains[state.Kind]
	if !ok {
		return state, Event{}, ErrUnknownKind
	}
	next := state
	event := Event{Action: action, From: state.Status, Reason: reason, At: in.Now}

	open := func(window Window, days int) {
		next.Window = window
		next.DueDate, next.ExpiresAt = CalendarDeadline(in.Now, days)
		event.DueDate = next.DueDate
	}
	closeWindow := func() {
		next.Window, next.DueDate, next.ExpiresAt = WindowNone, "", time.Time{}
	}

	switch action {
	case ActionSend:
		if state.Status != StatusDraft {
			return fail("отправить можно только черновик, а не %s", state.Status)
		}
		if milestones := MilestonesFor(state.Kind, state.ReportYear); milestones.Has {
			today := Today(in.Now)
			if today.Before(milestones.Cutoff) {
				return state, Event{}, fmt.Errorf("%w: перечень формируется на %s", ErrTooEarly, milestones.Cutoff.Format("02.01.2006"))
			}
			next.SentLate = today.After(milestones.SendDue)
		}
		sentAt := in.Now.UTC()
		next.SentAt = &sentAt
		next.Status, next.Round = StatusSent, 1
		open(WindowReview, chain.Review)

	case ActionStartReview:
		if state.Status != StatusSent && state.Status != StatusResubmitted {
			return fail("принять к рассмотрению можно отправленный или повторно отправленный процесс, а не %s", state.Status)
		}
		// Срок не пересчитывается: рассмотрение началось внутри уже идущего
		// окна, и начало работы не должно его продлевать.
		next.Status = StatusInReview

	case ActionApprove:
		if state.Status != StatusSent && state.Status != StatusInReview && state.Status != StatusResubmitted {
			return fail("согласовать можно процесс, находящийся на рассмотрении, а не %s", state.Status)
		}
		next.Status = StatusApproved
		closeWindow()

	case ActionRequestRework:
		// Возврат на доработку — один раз: после повторной отправки
		// рассмотрение заканчивается согласованием или оспариванием.
		if state.Round != 1 || (state.Status != StatusSent && state.Status != StatusInReview) {
			return fail("вернуть на доработку можно при первичном рассмотрении, а не %s (круг %d)", state.Status, state.Round)
		}
		if reason == "" {
			return state, Event{}, fmt.Errorf("%w: укажите замечания", ErrReasonRequired)
		}
		next.Status, next.Remarks = StatusRework, reason
		open(WindowRework, chain.Rework)

	case ActionResubmit:
		if state.Status != StatusRework {
			return fail("повторно отправить можно только процесс, возвращённый на доработку, а не %s", state.Status)
		}
		if DeadlineExpired(state.ExpiresAt, in.Now) {
			return state, Event{}, ErrWindowClosed
		}
		next.Status, next.Round = StatusResubmitted, 2
		open(WindowReReview, chain.ReReview)

	case ActionDispute:
		if state.Status != StatusResubmitted && !(state.Status == StatusInReview && state.Round == 2) {
			return fail("оспорить можно процесс после доработки, а не %s (круг %d)", state.Status, state.Round)
		}
		if reason == "" {
			return state, Event{}, fmt.Errorf("%w: укажите причину", ErrReasonRequired)
		}
		next.Status = StatusDisputed
		closeWindow()

	default:
		return fail("неизвестное действие %q", action)
	}
	event.To = next.Status
	return next, event, nil
}

// ExpireIfDue переводит процесс в «согласован по молчанию», если окно
// рассмотрения истекло без ответа (ADR-02: после окончания последнего
// календарного дня срока). Повторный вызов ничего не меняет: у завершённого
// процесса окна нет.
//
// Окно доработки по истечении состояния не меняет: пропуск срока автором —
// это просрочка, о которой Приказ говорит на уровне позиций перечня
// («позиция считается отсутствующей»), а не всего процесса. Пропуск
// фиксируется отдельным событием.
func ExpireIfDue(state State, now time.Time) (State, *Event) {
	if state.Status.Terminal() || state.Window == WindowNone || !DeadlineExpired(state.ExpiresAt, now) {
		return state, nil
	}
	switch state.Window {
	case WindowReview, WindowReReview:
		event := Event{
			Action: ActionDefaultApprove, From: state.Status, To: StatusDefaultApproved, At: now,
			DueDate: state.DueDate, System: true,
			Reason: fmt.Sprintf("срок рассмотрения истёк %s без ответа", state.DueDate),
		}
		state.Status, state.Window, state.DueDate, state.ExpiresAt = StatusDefaultApproved, WindowNone, "", time.Time{}
		return state, &event
	case WindowRework:
		if state.ReworkLapsedAt != nil {
			return state, nil
		}
		at := now.UTC()
		state.ReworkLapsedAt = &at
		event := Event{
			Action: ActionReworkLapsed, From: state.Status, To: state.Status, At: now, DueDate: state.DueDate, System: true,
			Reason: fmt.Sprintf("срок доработки истёк %s", state.DueDate),
		}
		return state, &event
	}
	return state, nil
}

// Allowed перечисляет действия, допустимые сейчас, — для интерфейса и для
// проверки прав: пользователю не предлагается то, что процесс не примет.
func Allowed(state State, now time.Time) []Action {
	if state.Status.Terminal() {
		return nil
	}
	var out []Action
	for _, action := range Actions() {
		if _, _, err := Apply(state, action, Input{Now: now, Reason: "проверка"}); err == nil {
			out = append(out, action)
		}
	}
	return out
}
