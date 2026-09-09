package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestPasswordUpgradeAndBounds(t *testing.T) {
	password := "StrongPassword1!"
	salt := make([]byte, saltBytes)
	legacy := fmt.Sprintf("pbkdf2$210000$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(pbkdf2([]byte(password), salt, 210000, keyBytes)))
	if ok, err := VerifyPassword(password, legacy); err != nil || !ok || !NeedsRehash(legacy) {
		t.Fatal("legacy password no longer works", err)
	}
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(password, hash); err != nil || !ok || NeedsRehash(hash) {
		t.Fatal("new hash invalid", err)
	}
	if ok, _ := VerifyPassword("wrong", hash); ok {
		t.Fatal("wrong password accepted")
	}
	for _, invalid := range []string{strings.Replace(hash, "600000", "999999999", 1), strings.Replace(hash, "600000", "0", 1), "pbkdf2$600000$$", hash + strings.Repeat("x", 512)} {
		if ok, err := VerifyPassword(password, invalid); ok || err == nil {
			t.Fatal("unbounded hash accepted")
		}
	}
}
