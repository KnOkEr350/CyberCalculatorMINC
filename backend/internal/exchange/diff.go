package exchange

import (
	"encoding/json"
	"math/big"
	"sort"
	"strings"
)

// Status — исход сопоставления одной записи.
type Status string

const (
	// StatusIdentical — запись есть с обеих сторон и совпадает.
	StatusIdentical Status = "identical"
	// StatusCollision — запись есть с обеих сторон, но значения различаются.
	StatusCollision Status = "collision"
	// StatusMissingLocal — запись пришла в пакете, а у нас её нет.
	StatusMissingLocal Status = "missing_local"
	// StatusMissingIncoming — запись есть у нас, а в пакете её нет.
	StatusMissingIncoming Status = "missing_incoming"
	// StatusAmbiguous — ключ повторяется внутри одной стороны: сравнить
	// «ту самую» запись нельзя, выбор за человеком.
	StatusAmbiguous Status = "ambiguous"
)

// FieldDiff — одно расхождение с локальным и входящим значением.
type FieldDiff struct {
	Field    string      `json:"field"`
	Local    interface{} `json:"local"`
	Incoming interface{} `json:"incoming"`
}

// Item — результат по одному ключу.
type Item struct {
	Key         Key         `json:"key"`
	Status      Status      `json:"status"`
	Local       *Record     `json:"local,omitempty"`
	Incoming    *Record     `json:"incoming,omitempty"`
	Differences []FieldDiff `json:"differences,omitempty"`
	// Detail поясняет неоднозначность.
	Detail string `json:"detail,omitempty"`
}

// Result — полное сравнение: элементы упорядочены по ключу, счётчики
// сходятся с числом элементов.
type Result struct {
	Items  []Item
	Counts map[Status]int
}

// localOnly — поля, значения которых принадлежат экземпляру и у другого
// экземпляра заведомо иные: идентификаторы и «org_name» (в нём лежит
// идентификатор партнёра). Сравнивать их значит получать расхождение в каждой
// записи.
func localOnly(field string) bool {
	return field == "org_name" || strings.HasSuffix(field, "_id")
}

// Diff сравнивает локальные записи с пришедшими. Сопоставление идёт по
// составному ключу; повтор ключа с любой стороны выделяется в отдельный статус
// и в остальном сравнении не участвует.
func Diff(local, incoming []Record) (Result, error) {
	localIndex, localDuplicates, err := indexAllowingDuplicates(local)
	if err != nil {
		return Result{}, err
	}
	incomingIndex, incomingDuplicates, err := indexAllowingDuplicates(incoming)
	if err != nil {
		return Result{}, err
	}

	items := []Item{}
	ambiguous := map[string]bool{}
	for _, key := range localDuplicates {
		ambiguous[key.String()] = true
		items = append(items, Item{Key: key, Status: StatusAmbiguous, Detail: "ключ повторяется среди локальных записей"})
	}
	for _, key := range incomingDuplicates {
		if ambiguous[key.String()] {
			continue
		}
		ambiguous[key.String()] = true
		items = append(items, Item{Key: key, Status: StatusAmbiguous, Detail: "ключ повторяется среди записей пакета"})
	}

	ids := map[string]Key{}
	for id, entry := range localIndex {
		ids[id] = entry.key
	}
	for id, entry := range incomingIndex {
		ids[id] = entry.key
	}
	for id, key := range ids {
		if ambiguous[id] {
			continue
		}
		mine, hasLocal := localIndex[id]
		theirs, hasIncoming := incomingIndex[id]
		switch {
		case hasLocal && !hasIncoming:
			record := mine.record
			items = append(items, Item{Key: key, Status: StatusMissingIncoming, Local: &record})
		case !hasLocal && hasIncoming:
			record := theirs.record
			items = append(items, Item{Key: key, Status: StatusMissingLocal, Incoming: &record})
		default:
			localRecord, incomingRecord := mine.record, theirs.record
			differences := compare(localRecord, incomingRecord)
			status := StatusIdentical
			if len(differences) > 0 {
				status = StatusCollision
			}
			items = append(items, Item{Key: key, Status: status, Local: &localRecord, Incoming: &incomingRecord, Differences: differences})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Key.String() != items[j].Key.String() {
			return items[i].Key.String() < items[j].Key.String()
		}
		return items[i].Status < items[j].Status
	})
	counts := map[Status]int{}
	for _, item := range items {
		counts[item.Status]++
	}
	return Result{Items: items, Counts: counts}, nil
}

type indexed struct {
	key    Key
	record Record
}

// indexAllowingDuplicates строит индекс, а повторяющиеся ключи возвращает
// отдельно: для сравнения дубликат — данные, а не ошибка.
func indexAllowingDuplicates(records []Record) (map[string]indexed, []Key, error) {
	index := make(map[string]indexed, len(records))
	counts := map[string]int{}
	keys := map[string]Key{}
	for _, record := range records {
		key, err := KeyOf(record)
		if err != nil {
			return nil, nil, err
		}
		id := key.String()
		counts[id]++
		keys[id] = key
		if _, exists := index[id]; !exists {
			index[id] = indexed{key: key, record: record}
		}
	}
	duplicates := []Key{}
	for id, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, keys[id])
			delete(index, id)
		}
	}
	sort.Slice(duplicates, func(i, j int) bool { return duplicates[i].String() < duplicates[j].String() })
	return index, duplicates, nil
}

// compare возвращает расхождения двух записей с одним ключом.
func compare(local, incoming Record) []FieldDiff {
	diffs := []FieldDiff{}
	if local.Audience != incoming.Audience {
		diffs = append(diffs, FieldDiff{Field: "audience", Local: local.Audience, Incoming: incoming.Audience})
	}
	if !sameNumber(local.AmountRub, incoming.AmountRub) {
		diffs = append(diffs, FieldDiff{Field: "amount_rub", Local: local.AmountRub, Incoming: incoming.AmountRub})
	}
	fields := map[string]bool{}
	for field := range local.Payload {
		fields[field] = true
	}
	for field := range incoming.Payload {
		fields[field] = true
	}
	names := make([]string, 0, len(fields))
	for field := range fields {
		if !localOnly(field) {
			names = append(names, field)
		}
	}
	sort.Strings(names)
	for _, field := range names {
		mine, hasMine := local.Payload[field]
		theirs, hasTheirs := incoming.Payload[field]
		if isEmpty(mine) && isEmpty(theirs) {
			continue
		}
		if hasMine && hasTheirs && sameValue(mine, theirs) {
			continue
		}
		diffs = append(diffs, FieldDiff{Field: "payload." + field, Local: mine, Incoming: theirs})
	}
	return diffs
}

// isEmpty: отсутствующее поле, null и пустая строка — одно и то же «не
// заполнено». Иначе расхождением считалось бы разное представление пустоты.
func isEmpty(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	}
	return false
}

// sameValue сравнивает значения по смыслу: 64, 64.0 и json.Number("64.0") —
// одно число; число и строка с тем же текстом — разные значения.
func sameValue(a, b interface{}) bool {
	if left, ok := number(a); ok {
		if right, ok := number(b); ok {
			return left.Cmp(right) == 0
		}
		return false
	}
	if _, ok := number(b); ok {
		return false
	}
	if ls, ok := a.(string); ok {
		rs, ok := b.(string)
		return ok && strings.TrimSpace(ls) == strings.TrimSpace(rs)
	}
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && string(left) == string(right)
}

func sameNumber(a, b string) bool {
	left, okLeft := new(big.Rat).SetString(strings.TrimSpace(a))
	right, okRight := new(big.Rat).SetString(strings.TrimSpace(b))
	if okLeft && okRight {
		return left.Cmp(right) == 0
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func number(value interface{}) (*big.Rat, bool) {
	switch v := value.(type) {
	case float64:
		return new(big.Rat).SetFloat64(v), true
	case int:
		return big.NewRat(int64(v), 1), true
	case int64:
		return big.NewRat(v, 1), true
	case interface{ String() string }: // json.Number
		return new(big.Rat).SetString(v.String())
	}
	return nil, false
}
