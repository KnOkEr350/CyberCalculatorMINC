package calculators

import (
	"cybercalc/internal/models"
	"encoding/json"
	"testing"
)

func TestAllOrderDocumentRates(t *testing.T) {
	for _, tt := range []struct{ doc, level, activity, want string }{
		{"rpd", "vo", "development", "300000.00"}, {"rpd", "vo", "update", "160000.00"}, {"rpd", "vo", "expertise", "55000.00"},
		{"rpd", "spo", "development", "270750.00"}, {"rpd", "spo", "update", "150000.00"}, {"rpd", "spo", "expertise", "58060.00"},
		{"oop", "vo", "development", "2039850.00"}, {"oop", "vo", "update", "626110.00"}, {"oop", "vo", "expertise", "312300.00"},
		{"oop", "spo", "development", "1731360.00"}, {"oop", "spo", "update", "427440.00"}, {"oop", "spo", "expertise", "171000.00"},
	} {
		audience := models.AudienceVuz
		if tt.level == "spo" {
			audience = models.AudienceKolledj
		}
		a, err := CalculateAmount("ood_rpd", audience, map[string]interface{}{"doc_type": tt.doc, "level": tt.level, "activity_type": tt.activity})
		if err != nil || a.String() != tt.want {
			t.Fatalf("%+v: %s %v", tt, a, err)
		}
	}
}

func TestExactDecimalAmounts(t *testing.T) {
	for _, tt := range []struct{ category, key, value, want string }{
		{"teachers", "academic_hours", "0.0025", "10.35"},
		{"top_it", "cofinancing_amount_rub", "123456789012.345", "123456789012.35"},
		{"minc_decision", "amount_manual", "1.005", "1.01"},
	} {
		a, err := CalculateAmount(tt.category, models.AudienceVuz, map[string]interface{}{tt.key: json.Number(tt.value)})
		if err != nil || a.String() != tt.want {
			t.Fatalf("%+v: %s %v", tt, a, err)
		}
	}
}
