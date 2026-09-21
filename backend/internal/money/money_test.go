package money

import (
	"encoding/json"
	"testing"
)

func TestDecimalPrecision(t *testing.T) {
	for _, tt := range []struct{ input, want string }{{"1.005", "1.01"}, {"99999999999999.99", "99999999999999.99"}, {"0.004", "0.00"}, {"0.005", "0.01"}} {
		a, err := Parse(tt.input)
		if err != nil || a.String() != tt.want {
			t.Fatalf("%s: %s %v", tt.input, a, err)
		}
		var scanned Amount
		if err := scanned.Scan([]byte(a.String())); err != nil || scanned != a {
			t.Fatal("DB roundtrip", err)
		}
	}
	for _, bad := range []string{"-1", "NaN", "1e9999999", "Infinity"} {
		if _, err := Parse(bad); err == nil {
			t.Fatal("unsafe amount", bad)
		}
	}
}

func TestJSONUsesVersionedDecimalStringContract(t *testing.T) {
	amount, err := Parse("125000.50")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(amount)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"125000.50"` {
		t.Fatalf("got %s, want a decimal JSON string", encoded)
	}
	for _, input := range []string{`"125000.50"`, `125000.50`} {
		var decoded Amount
		if err := json.Unmarshal([]byte(input), &decoded); err != nil {
			t.Fatalf("decode %s: %v", input, err)
		}
		if decoded != amount {
			t.Fatalf("decode %s: got %s, want %s", input, decoded, amount)
		}
	}
}
