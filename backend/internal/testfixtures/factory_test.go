package testfixtures

import (
	"cybercalc/internal/auth"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestFixturePasswordUsesApplicationContract(t *testing.T) {
	hash, err := hashFixturePassword(DefaultPassword)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := auth.VerifyPassword(DefaultPassword, hash)
	if err != nil || !valid {
		t.Fatalf("fixture password is not accepted by auth: valid=%v err=%v", valid, err)
	}
}

func TestFixtureLegalIdentifiersHaveValidChecksums(t *testing.T) {
	inn := inn10(t.Name())
	if !regexp.MustCompile(`^[0-9]{10}$`).MatchString(inn) {
		t.Fatalf("invalid INN shape: %q", inn)
	}
	weights := []int{2, 4, 10, 3, 5, 9, 4, 6, 8}
	sum := 0
	for index, weight := range weights {
		sum += int(inn[index]-'0') * weight
	}
	if int(inn[9]-'0') != (sum%11)%10 {
		t.Fatalf("invalid INN checksum: %q", inn)
	}

	ogrn := ogrn13(t.Name())
	if !regexp.MustCompile(`^[0-9]{13}$`).MatchString(ogrn) {
		t.Fatalf("invalid OGRN shape: %q", ogrn)
	}
	base, err := strconv.ParseUint(ogrn[:12], 10, 64)
	if err != nil || int(ogrn[12]-'0') != int(base%11%10) {
		t.Fatalf("invalid OGRN checksum: %q", ogrn)
	}
}

func TestFactoryNamespaceIsSafeAndStable(t *testing.T) {
	factory := newFactory(nil, "Test / Параллельный #1", time.Time{}, &atomic.Uint64{})
	if factory.namespace != "test-1" {
		t.Fatalf("namespace=%q", factory.namespace)
	}
}
