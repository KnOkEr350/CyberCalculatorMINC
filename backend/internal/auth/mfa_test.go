package auth

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestRFC6238AndEncryption(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, tt := range []struct {
		at   int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1234567890, "005924"}} {
		code, err := TOTP(secret, tt.at/30)
		if err != nil || code != tt.code {
			t.Fatalf("RFC vector %v: %s %v", tt, code, err)
		}
		if _, ok := VerifyTOTP(secret, tt.code, time.Unix(tt.at, 0)); !ok {
			t.Fatal("valid code rejected")
		}
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	sealed, err := SealMFA(key, secret, "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := OpenMFA(key, sealed, "user-a"); err != nil || plain != secret {
		t.Fatal("encryption roundtrip", err)
	}
	if _, err := OpenMFA(key, sealed, "user-b"); err == nil {
		t.Fatal("secret transferable across users")
	}
	if _, err := OpenMFA(key, sealed+"broken", "user-a"); err == nil {
		t.Fatal("tampering accepted")
	}
}
