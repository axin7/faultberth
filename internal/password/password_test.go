package password

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func TestVerifyPassword(t *testing.T) {
	password, salt, rounds := "correct horse battery", "random-salt", 2000
	hash := pbkdf2.Key([]byte(password), []byte(salt), rounds, 32, sha256.New)
	encoded := strings.Join([]string{"pbkdf2_sha256", strconv.Itoa(rounds), salt,
		base64.StdEncoding.EncodeToString(hash)}, "$")
	if !Verify(encoded, password) || Verify(encoded, "incorrect") {
		t.Fatal("password verification returned incorrect result")
	}
	for _, malformed := range []string{"", "wrong$1$salt$digest", "pbkdf2_sha256$999999999$salt$abc"} {
		if Verify(malformed, password) {
			t.Fatalf("accepted invalid hash %q", malformed)
		}
	}
}
