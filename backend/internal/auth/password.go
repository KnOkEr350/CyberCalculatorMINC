// Package auth implements password hashing using the standard crypto library.
package auth

import (
	pbkdf2std "crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	pbkdf2Iterations = 600_000 // OWASP PBKDF2-HMAC-SHA256 minimum.
	saltBytes        = 16
	keyBytes         = 32
)

// pbkdf2 delegates to the maintained standard-library implementation.
func pbkdf2(password, salt []byte, iterations, keyLen int) []byte {
	key, err := pbkdf2std.Key(sha256.New, string(password), salt, iterations, keyLen)
	if err != nil {
		return nil
	}
	return key
}

// HashPassword возвращает строку вида: pbkdf2$<iterations>$<salt-b64>$<hash-b64>
func HashPassword(password string) (string, error) {
	if len(password) == 0 || len(password) > 512 {
		return "", errors.New("недопустимая длина пароля")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := pbkdf2([]byte(password), salt, pbkdf2Iterations, keyBytes)
	return fmt.Sprintf("pbkdf2$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword сверяет пароль с ранее сохранённым хешем в постоянное время.
func VerifyPassword(password, encoded string) (bool, error) {
	if len(password) > 512 || len(encoded) > 256 {
		return false, errors.New("недопустимая длина")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false, errors.New("неверный формат хеша пароля")
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 210_000 || iterations > 1_200_000 {
		return false, errors.New("недопустимая стоимость хеша")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != saltBytes {
		return false, errors.New("неверная соль")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) != keyBytes {
		return false, errors.New("неверная длина хеша")
	}
	got := pbkdf2([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func NeedsRehash(encoded string) bool {
	return !strings.HasPrefix(encoded, fmt.Sprintf("pbkdf2$%d$", pbkdf2Iterations))
}
