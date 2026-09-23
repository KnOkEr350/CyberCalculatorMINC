// Package topit — составляющие программы ТОП-ИТ/ТОП-ИИ (Вид 4): неденежная
// поддержка (TOP-04), стипендиаты (TOP-06) и производственные кейсы (TOP-07).
//
// Пакет проверяет строки и хранит их; суммы здесь — сведения о программе, а не
// зачётная сумма: официальный объём Вида 4 определяется отчётом о
// софинансировании (ADR-19), а пороги и знаменатель прогресса ждут ADR-14 и
// здесь не вычисляются.
package topit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/money"
	"github.com/lib/pq"
)

type Kind string

const (
	KindSupport     Kind = "support"
	KindScholarship Kind = "scholarship"
	KindCase        Kind = "case"
	KindRID         Kind = "rid"
)

var (
	ErrNotFound     = errors.New("строка не найдена")
	ErrInvalid      = errors.New("некорректная строка")
	ErrNotTopEntry  = errors.New("составляющие программы относятся только к записям Вида 4 (ТОП-ИТ/ТОП-ИИ)")
	ErrForeignFiles = errors.New("документ не относится к этой записи")
)

// Input — строка так, как её присылает клиент. Поля чужого вида должны быть
// пустыми.
type Input struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Неденежная поддержка.
	SupportKind       string        `json:"support_kind,omitempty"`
	ActReference      string        `json:"act_reference,omitempty"`
	ActDate           string        `json:"act_date,omitempty"`
	BalanceValueRub   *money.Amount `json:"balance_value_rub,omitempty"`
	AppraisedValueRub *money.Amount `json:"appraised_value_rub,omitempty"`
	ConfirmedValueRub *money.Amount `json:"confirmed_value_rub,omitempty"`
	// Стипендиат.
	StudentName string        `json:"student_name,omitempty"`
	GroupName   string        `json:"group_name,omitempty"`
	Course      int           `json:"course,omitempty"`
	PeriodStart string        `json:"period_start,omitempty"`
	PeriodEnd   string        `json:"period_end,omitempty"`
	AmountRub   *money.Amount `json:"amount_rub,omitempty"`
	Criterion   string        `json:"criterion,omitempty"`
	DonorName   string        `json:"donor_name,omitempty"`
	// Производственный кейс.
	ImplementationOrg    string `json:"implementation_org,omitempty"`
	ImplementationStatus string `json:"implementation_status,omitempty"`
	ImplementedOn        string `json:"implemented_on,omitempty"`
	Description          string `json:"description,omitempty"`
	// РИД (TOP-08): тип, авторы и доли исключительных прав.
	RIDType            string   `json:"rid_type,omitempty"`
	Authors            string   `json:"authors,omitempty"`
	UniversitySharePct *float64 `json:"university_share_pct,omitempty"`
	CompanySharePct    *float64 `json:"company_share_pct,omitempty"`
	// DocumentIDs — вложения записи, подтверждающие строку.
	DocumentIDs []string `json:"document_ids,omitempty"`
}

func fail(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func text(value, label string, min, max int) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	if n := utf8.RuneCountInString(value); n < min || n > max {
		return "", fail("%s: от %d до %d символов", label, min, max)
	}
	return value, nil
}

func day(value, label string, today time.Time, allowFuture bool) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", fail("%s: ожидается дата ГГГГ-ММ-ДД", label)
	}
	if parsed.Year() < 2000 {
		return "", fail("%s: не раньше 2000 года", label)
	}
	if !allowFuture && parsed.After(today) {
		return "", fail("%s не может быть в будущем", label)
	}
	return value, nil
}

// Normalize проверяет строку по её виду и приводит к каноническому виду.
// today — московская дата проверки: акт и внедрение из будущего невозможны.
func Normalize(in Input, today time.Time) (Input, error) {
	title, err := text(in.Title, "название", 2, 300)
	if err != nil {
		return in, err
	}
	in.Title = title
	kind := Kind(in.Kind)
	// Поле чужого вида — ошибка, а не молчаливое отбрасывание: клиент должен
	// узнать, что его данные не сохранены.
	foreign := func(name string, present bool) error {
		if present {
			return fail("поле %s не относится к виду %q", name, in.Kind)
		}
		return nil
	}
	isSupport := in.SupportKind != "" || in.ActReference != "" || in.ActDate != "" || in.BalanceValueRub != nil || in.AppraisedValueRub != nil || in.ConfirmedValueRub != nil
	isScholarship := in.StudentName != "" || in.GroupName != "" || in.Course != 0 || in.PeriodStart != "" || in.PeriodEnd != "" || in.AmountRub != nil || in.Criterion != "" || in.DonorName != ""
	isRID := in.RIDType != "" || in.Authors != "" || in.UniversitySharePct != nil || in.CompanySharePct != nil
	isCase := in.ImplementationOrg != "" || in.ImplementationStatus != "" || in.ImplementedOn != "" || in.Description != ""

	switch kind {
	case KindSupport:
		if err := foreign("стипендиата", isScholarship); err != nil {
			return in, err
		}
		if err := foreign("кейса", isCase); err != nil {
			return in, err
		}
		if err := foreign("РИД", isRID); err != nil {
			return in, err
		}
		if in.SupportKind != "equipment" && in.SupportKind != "software" {
			return in, fail("вид поддержки: equipment или software")
		}
		if in.ActReference, err = text(in.ActReference, "реквизиты акта", 1, 500); err != nil {
			return in, err
		}
		if in.ActDate, err = day(in.ActDate, "дата акта", today, false); err != nil {
			return in, err
		}
		if in.BalanceValueRub == nil && in.AppraisedValueRub == nil {
			return in, fail("укажите балансовую или оценочную стоимость")
		}
		limit := money.Amount(0)
		for _, value := range []*money.Amount{in.BalanceValueRub, in.AppraisedValueRub} {
			if value != nil && *value > limit {
				limit = *value
			}
		}
		if in.ConfirmedValueRub != nil && *in.ConfirmedValueRub > limit {
			return in, fail("подтверждённая стоимость не может превышать балансовую и оценочную")
		}
	case KindScholarship:
		if err := foreign("неденежной поддержки", isSupport); err != nil {
			return in, err
		}
		if err := foreign("кейса", isCase); err != nil {
			return in, err
		}
		if err := foreign("РИД", isRID); err != nil {
			return in, err
		}
		if in.StudentName, err = text(in.StudentName, "студент", 2, 300); err != nil {
			return in, err
		}
		if in.GroupName, err = text(in.GroupName, "группа", 1, 100); err != nil {
			return in, err
		}
		if in.Course < 1 || in.Course > 6 {
			return in, fail("курс: от 1 до 6")
		}
		if in.PeriodStart, err = day(in.PeriodStart, "начало периода", today, true); err != nil {
			return in, err
		}
		if in.PeriodEnd, err = day(in.PeriodEnd, "конец периода", today, true); err != nil {
			return in, err
		}
		if in.PeriodEnd < in.PeriodStart {
			return in, fail("конец периода раньше начала")
		}
		if in.AmountRub == nil || *in.AmountRub <= 0 {
			return in, fail("сумма стипендии должна быть положительной")
		}
		if in.Criterion, err = text(in.Criterion, "критерий", 1, 1000); err != nil {
			return in, err
		}
		if in.DonorName, err = text(in.DonorName, "донор", 1, 300); err != nil {
			return in, err
		}
	case KindCase:
		if err := foreign("неденежной поддержки", isSupport); err != nil {
			return in, err
		}
		if err := foreign("стипендиата", isScholarship); err != nil {
			return in, err
		}
		if err := foreign("РИД", isRID); err != nil {
			return in, err
		}
		if in.ImplementationOrg, err = text(in.ImplementationOrg, "организация внедрения", 1, 300); err != nil {
			return in, err
		}
		if in.Description, err = text(in.Description, "описание", 1, 4000); err != nil {
			return in, err
		}
		switch in.ImplementationStatus {
		case "proposed":
			if in.ImplementedOn != "" {
				return in, fail("дата внедрения указывается только у внедрённого кейса")
			}
		case "implemented":
			if in.ImplementedOn, err = day(in.ImplementedOn, "дата внедрения", today, false); err != nil {
				return in, err
			}
		default:
			return in, fail("статус внедрения: proposed или implemented")
		}
	case KindRID:
		if err := foreign("неденежной поддержки", isSupport); err != nil {
			return in, err
		}
		if err := foreign("стипендиата", isScholarship); err != nil {
			return in, err
		}
		if err := foreign("кейса", isCase); err != nil {
			return in, err
		}
		if in.RIDType != "software" && in.RIDType != "ai_model" && in.RIDType != "dataset" {
			return in, fail("тип РИД: software, ai_model или dataset")
		}
		if in.Authors, err = text(in.Authors, "авторы", 2, 2000); err != nil {
			return in, err
		}
		if in.UniversitySharePct == nil || in.CompanySharePct == nil {
			return in, fail("укажите доли вуза и компании")
		}
		// Доли считаются в сотых процента: 33,33 и 66,67 дают ровно 100.
		university, company := int64(math.Round(*in.UniversitySharePct*100)), int64(math.Round(*in.CompanySharePct*100))
		if university < 0 || company < 0 || university+company != 10000 {
			return in, fail("доли исключительных прав вуза и компании в сумме дают 100%%")
		}
	default:
		return in, fail("вид строки: support, scholarship, case или rid")
	}
	seen := map[string]bool{}
	for _, id := range in.DocumentIDs {
		if seen[id] {
			return in, fail("документ %s указан дважды", id)
		}
		seen[id] = true
	}
	return in, nil
}

// Item — сохранённая строка.
type Item struct {
	ID        string `json:"id"`
	EntryID   string `json:"entry_id"`
	CreatedAt string `json:"created_at"`
	Input
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func nullable(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullableAmount(value *money.Amount) interface{} {
	if value == nil {
		return nil
	}
	return value.String()
}

func nullableShare(value *float64) interface{} {
	if value == nil {
		return nil
	}
	return fmt.Sprintf("%.2f", *value)
}

func nullableInt(value int) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

func classify(err error) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23514", "22007", "22008":
			return fmt.Errorf("%w: %s", ErrInvalid, pgErr.Message)
		case "P0001":
			if strings.Contains(pgErr.Message, "top_it entries only") {
				return ErrNotTopEntry
			}
		}
	}
	return err
}

// checkDocuments — документы принадлежат записи и не просрочены.
func checkDocuments(ctx context.Context, db queryer, entryID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var found int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE entry_id::text=$1 AND id::text=ANY($2) AND retention_expires_at>now()`,
		entryID, pq.Array(ids)).Scan(&found); err != nil {
		return err
	}
	if found != len(ids) {
		return ErrForeignFiles
	}
	return nil
}

func writeDocuments(ctx context.Context, db queryer, itemID string, ids []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM top_item_documents WHERE item_id::text=$1`, itemID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO top_item_documents(item_id,attachment_id) VALUES($1::uuid,$2::uuid)`, itemID, id); err != nil {
			return err
		}
	}
	return nil
}

const itemColumns = `id::text,entry_id::text,created_at::text,kind,title,COALESCE(support_kind,''),COALESCE(act_reference,''),COALESCE(act_date::text,''),
	balance_value_rub::text,appraised_value_rub::text,confirmed_value_rub::text,COALESCE(student_name,''),COALESCE(group_name,''),COALESCE(course,0),
	COALESCE(period_start::text,''),COALESCE(period_end::text,''),amount_rub::text,COALESCE(criterion,''),COALESCE(donor_name,''),
	COALESCE(implementation_org,''),COALESCE(implementation_status,''),COALESCE(implemented_on::text,''),COALESCE(description,''),COALESCE(rid_type,''),COALESCE(authors,''),university_share_pct::float8,company_share_pct::float8,
	ARRAY(SELECT d.attachment_id::text FROM top_item_documents d WHERE d.item_id=top_program_items.id ORDER BY 1)`

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var item Item
	var balance, appraised, confirmed, amount sql.NullString
	var university, company sql.NullFloat64
	var documents pq.StringArray
	if err := row.Scan(&item.ID, &item.EntryID, &item.CreatedAt, &item.Kind, &item.Title, &item.SupportKind, &item.ActReference, &item.ActDate,
		&balance, &appraised, &confirmed, &item.StudentName, &item.GroupName, &item.Course, &item.PeriodStart, &item.PeriodEnd,
		&amount, &item.Criterion, &item.DonorName, &item.ImplementationOrg, &item.ImplementationStatus, &item.ImplementedOn,
		&item.Description, &item.RIDType, &item.Authors, &university, &company, &documents); err != nil {
		return item, err
	}
	if university.Valid {
		item.UniversitySharePct = &university.Float64
	}
	if company.Valid {
		item.CompanySharePct = &company.Float64
	}
	for _, pair := range []struct {
		from sql.NullString
		to   **money.Amount
	}{{balance, &item.BalanceValueRub}, {appraised, &item.AppraisedValueRub}, {confirmed, &item.ConfirmedValueRub}, {amount, &item.AmountRub}} {
		if pair.from.Valid {
			value, err := money.Parse(pair.from.String)
			if err != nil {
				return item, err
			}
			*pair.to = &value
		}
	}
	item.DocumentIDs = []string(documents)
	return item, nil
}

// Create добавляет строку к записи Вида 4.
func Create(ctx context.Context, db queryer, entryID string, in Input, by string) (Item, error) {
	if err := checkDocuments(ctx, db, entryID, in.DocumentIDs); err != nil {
		return Item{}, err
	}
	row := db.QueryRowContext(ctx, `INSERT INTO top_program_items(entry_id,kind,title,support_kind,act_reference,act_date,balance_value_rub,
			appraised_value_rub,confirmed_value_rub,student_name,group_name,course,period_start,period_end,amount_rub,criterion,donor_name,
			implementation_org,implementation_status,implemented_on,description,rid_type,authors,university_share_pct,company_share_pct,created_by)
		VALUES($1::uuid,$2,$3,$4,$5,$6::date,$7::numeric,$8::numeric,$9::numeric,$10,$11,$12,$13::date,$14::date,$15::numeric,$16,$17,$18,$19,$20::date,$21,$22,$23,$24::numeric,$25::numeric,$26::uuid)
		RETURNING `+itemColumns, entryID, in.Kind, in.Title, nullable(in.SupportKind), nullable(in.ActReference), nullable(in.ActDate),
		nullableAmount(in.BalanceValueRub), nullableAmount(in.AppraisedValueRub), nullableAmount(in.ConfirmedValueRub),
		nullable(in.StudentName), nullable(in.GroupName), nullableInt(in.Course), nullable(in.PeriodStart), nullable(in.PeriodEnd),
		nullableAmount(in.AmountRub), nullable(in.Criterion), nullable(in.DonorName), nullable(in.ImplementationOrg),
		nullable(in.ImplementationStatus), nullable(in.ImplementedOn), nullable(in.Description), nullable(in.RIDType), nullable(in.Authors),
		nullableShare(in.UniversitySharePct), nullableShare(in.CompanySharePct), nullable(by))
	item, err := scanItem(row)
	if err != nil {
		return Item{}, classify(err)
	}
	if err := writeDocuments(ctx, db, item.ID, in.DocumentIDs); err != nil {
		return Item{}, err
	}
	item.DocumentIDs = append([]string{}, in.DocumentIDs...)
	return item, nil
}

// Get читает строку.
func Get(ctx context.Context, db queryer, id string) (Item, error) {
	item, err := scanItem(db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM top_program_items WHERE id::text=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}

// Update заменяет содержимое строки; вид и запись не меняются.
func Update(ctx context.Context, db queryer, id string, in Input) (Item, error) {
	current, err := Get(ctx, db, id)
	if err != nil {
		return Item{}, err
	}
	if in.Kind != current.Kind {
		return Item{}, fail("вид строки изменить нельзя")
	}
	if err := checkDocuments(ctx, db, current.EntryID, in.DocumentIDs); err != nil {
		return Item{}, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE top_program_items SET title=$2,support_kind=$3,act_reference=$4,act_date=$5::date,balance_value_rub=$6::numeric,
			appraised_value_rub=$7::numeric,confirmed_value_rub=$8::numeric,student_name=$9,group_name=$10,course=$11,period_start=$12::date,
			period_end=$13::date,amount_rub=$14::numeric,criterion=$15,donor_name=$16,implementation_org=$17,implementation_status=$18,
			implemented_on=$19::date,description=$20,rid_type=$21,authors=$22,university_share_pct=$23::numeric,company_share_pct=$24::numeric,updated_at=now() WHERE id::text=$1`, id, in.Title, nullable(in.SupportKind),
		nullable(in.ActReference), nullable(in.ActDate), nullableAmount(in.BalanceValueRub), nullableAmount(in.AppraisedValueRub),
		nullableAmount(in.ConfirmedValueRub), nullable(in.StudentName), nullable(in.GroupName), nullableInt(in.Course),
		nullable(in.PeriodStart), nullable(in.PeriodEnd), nullableAmount(in.AmountRub), nullable(in.Criterion), nullable(in.DonorName),
		nullable(in.ImplementationOrg), nullable(in.ImplementationStatus), nullable(in.ImplementedOn), nullable(in.Description),
		nullable(in.RIDType), nullable(in.Authors), nullableShare(in.UniversitySharePct), nullableShare(in.CompanySharePct)); err != nil {
		return Item{}, classify(err)
	}
	if err := writeDocuments(ctx, db, id, in.DocumentIDs); err != nil {
		return Item{}, err
	}
	return Get(ctx, db, id)
}

// Delete удаляет строку.
func Delete(ctx context.Context, db queryer, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM top_program_items WHERE id::text=$1`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// List возвращает строки записи в порядке добавления.
func List(ctx context.Context, db queryer, entryID string) ([]Item, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+itemColumns+` FROM top_program_items WHERE entry_id::text=$1 ORDER BY kind,created_at,id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Summary — сведения по строкам записи. Это не зачётная сумма.
type Summary struct {
	Supports             int          `json:"supports"`
	ConfirmedSupportRub  money.Amount `json:"confirmed_support_rub"`
	UnconfirmedSupports  int          `json:"unconfirmed_supports"`
	Scholarships         int          `json:"scholarships"`
	ScholarshipsTotalRub money.Amount `json:"scholarships_total_rub"`
	Cases                int          `json:"cases"`
	ImplementedCases     int          `json:"implemented_cases"`
	Rids                 int          `json:"rids"`
}

// Summarize считает сводку. Неподтверждённая поддержка (без confirmed) в
// сумму не входит и считается отдельно.
func Summarize(items []Item) (Summary, error) {
	var s Summary
	var err error
	for _, item := range items {
		switch Kind(item.Kind) {
		case KindSupport:
			s.Supports++
			if item.ConfirmedValueRub == nil {
				s.UnconfirmedSupports++
				continue
			}
			if s.ConfirmedSupportRub, err = money.Add(s.ConfirmedSupportRub, *item.ConfirmedValueRub); err != nil {
				return s, err
			}
		case KindScholarship:
			s.Scholarships++
			if item.AmountRub != nil {
				if s.ScholarshipsTotalRub, err = money.Add(s.ScholarshipsTotalRub, *item.AmountRub); err != nil {
					return s, err
				}
			}
		case KindRID:
			s.Rids++
		case KindCase:
			s.Cases++
			if item.ImplementationStatus == "implemented" {
				s.ImplementedCases++
			}
		}
	}
	return s, nil
}
