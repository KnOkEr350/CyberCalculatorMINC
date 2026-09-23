package dbx

import (
	"testing"
)

// RISK-01 / ADR-15: риск вычисляется движком и нигде не хранится, поэтому в
// схеме не должно быть столбца, в который его можно записать вручную. Новая
// колонка с «risk» или «rating» в имени — повод остановиться: либо это
// производное значение, которому место в проекции, либо ручной статус,
// который ТЗ запрещает.
func TestSchemaHasNoStoredRiskStatus(t *testing.T) {
	db := permissionsDB(t)
	rows, err := db.Query(`SELECT table_name||'.'||column_name FROM information_schema.columns
		WHERE table_schema='public' AND (column_name ILIKE '%risk%' OR column_name ILIKE '%rating%')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		t.Errorf("в схеме есть столбец %s: риск не хранится и не задаётся вручную (RISK-01)", column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
