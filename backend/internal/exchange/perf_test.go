package exchange

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// QA-10: бюджеты времени и памяти для обмена пакетами на согласованном объёме
// (docs/PERFORMANCE.md): 20 000 записей в пакете и столько же локальных.
const (
	perfRecords = 20_000
	// Под детектором гонок выполнение медленнее в разы: там проверяется порядок
	// величины, а строгий бюджет — в обычном прогоне.
	perfRaceSlowdown = 8
)

// perfRecordsSet строит записи; у каждой третьей при differ другие часы.
func perfRecordsSet(n int, differ bool) []Record {
	out := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		hours := 32 + i%3*16
		if differ && i%3 == 0 {
			hours += 8
		}
		out = append(out, teacherRecord(fmt.Sprintf("Преподаватель %06d Иванович", i), fmt.Sprintf("Дисциплина %d", i%400), hours))
	}
	return out
}

func budget(base time.Duration) time.Duration {
	if raceEnabled {
		return base * perfRaceSlowdown
	}
	return base
}

func heapAfterGC() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestExchangeBudgetOnAgreedVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("длинная проверка")
	}
	f := newFixture(t)
	base := heapAfterGC()
	incoming := perfRecordsSet(perfRecords, false)
	local := perfRecordsSet(perfRecords, true) // у трети записей другие часы

	start := time.Now()
	data := f.export(t, incoming)
	exportTime := time.Since(start)

	start = time.Now()
	imported, err := f.open(data)
	openTime := time.Since(start)
	if err != nil || len(imported.Records) != perfRecords {
		t.Fatalf("открытие: %d записей, %v", len(imported.Records), err)
	}

	start = time.Now()
	result, err := Diff(local, imported.Records)
	diffTime := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Counts[StatusIdentical] + result.Counts[StatusCollision]; got != perfRecords {
		t.Fatalf("каждая запись сопоставлена: %d из %d (%v)", got, perfRecords, result.Counts)
	}
	if result.Counts[StatusCollision] == 0 || result.Counts[StatusIdentical] == 0 {
		t.Fatalf("в наборе есть и совпадения, и расхождения: %v", result.Counts)
	}
	// Протокол разногласий по всем расхождениям (REPORT-02 через общий XLSX).
	start = time.Now()
	protocol, err := BuildProtocol(ProtocolSource{Operator: "оператор", CheckedAt: fixtureNow, LocalRecordsNum: perfRecords}, result, map[string]Decision{})
	protocolTime := time.Since(start)
	if err != nil || len(protocol) == 0 {
		t.Fatalf("протокол: %d байт, %v", len(protocol), err)
	}
	if protocolTime > budget(5*time.Second) {
		t.Errorf("протокол по %d расхождениям занял %v", result.Counts[StatusCollision], protocolTime)
	}
	t.Logf("протокол %v, %d КиБ", protocolTime, len(protocol)>>10)
	// Всё построенное держим живым: измеряется то, что занимает набор целиком.
	after := heapAfterGC()
	runtime.KeepAlive(result)
	runtime.KeepAlive(imported)
	runtime.KeepAlive(incoming)
	runtime.KeepAlive(local)
	runtime.KeepAlive(data)

	t.Logf("экспорт %v, открытие %v, сравнение %v, пакет %d КиБ, куча набора +%d МиБ", exportTime, openTime, diffTime, len(data)>>10, (int64(after)-int64(base))>>20)
	for name, spent := range map[string]struct{ took, limit time.Duration }{
		"экспорт":   {exportTime, budget(3 * time.Second)},
		"открытие":  {openTime, budget(3 * time.Second)},
		"сравнение": {diffTime, budget(3 * time.Second)},
	} {
		if spent.took > spent.limit {
			t.Errorf("%s заняло %v при бюджете %v на %d записях", name, spent.took, spent.limit, perfRecords)
		}
	}
	// Память: набор из 20 000 записей с обеих сторон и результатом сравнения
	// занимает десятки мегабайт; предел с большим запасом ловит утечки на порядок.
	if growth := int64(after) - int64(base); growth > 400<<20 {
		t.Errorf("результат сравнения занимает %d МиБ на %d записях", growth>>20, perfRecords)
	}
}
