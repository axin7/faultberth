// Package password manages PBKDF2 password hashes compatible with Django.
package password

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const iterations = 1_000_000

func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltText := base64.RawURLEncoding.EncodeToString(salt)
	key := pbkdf2.Key([]byte(password), []byte(saltText), iterations, sha256.Size, sha256.New)
	return strings.Join([]string{"pbkdf2_sha256", strconv.Itoa(iterations), saltText,
		base64.StdEncoding.EncodeToString(key)}, "$"), nil
}

func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > 5_000_000 || len(parts[2]) > 128 {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := pbkdf2.Key([]byte(password), []byte(parts[2]), iterations, sha256.Size, sha256.New)
	return subtle.ConstantTimeCompare(got, want) == 1
}
