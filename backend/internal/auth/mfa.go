package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // RFC 6238 compatibility with authenticator applications.
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func NewMFASecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}
func MFAKey(raw string) (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("MFA_ENCRYPTION_KEY должен содержать 32 случайных байта в Base64")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func SealMFA(raw, secret, user string) (string, error) {
	a, err := MFAKey(raw)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(secret), []byte(user))), nil
}
func OpenMFA(raw, sealed, user string) (string, error) {
	a, err := MFAKey(raw)
	if err != nil {
		return "", err
	}
	b, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil || len(b) < a.NonceSize() {
		return "", fmt.Errorf("неверный MFA секрет")
	}
	plain, err := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(user))
	return string(plain), err
}
func TOTP(secret string, counter int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", err
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(b[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	code := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code), nil
}
func VerifyTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, delta := range []int64{0, -1, 1} {
		counter := now.Unix()/30 + delta
		expected, err := TOTP(secret, counter)
		if err == nil && subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return counter, true
		}
	}
	return 0, false
}
