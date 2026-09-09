package money

import "testing"

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
