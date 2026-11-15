package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/axin7/faultberth/internal/password"
)

func (s *Store) CreateAdmin(ctx context.Context, username, plaintext string) error {
	if username == "" || strings.Contains(username, ":") || len(username) > 150 || len(plaintext) < 12 {
		return errors.New("invalid username or password shorter than 12 characters")
	}
	hash, err := password.Hash(plaintext)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,active,is_admin)
		VALUES(?,?,1,1)`, username, hash)
	return err
}

func (s *Store) CreateProject(ctx context.Context, name string) (int64, string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 255 {
		return 0, "", errors.New("invalid project name")
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return 0, "", err
	}
	public := hex.EncodeToString(key)
	result, err := s.db.ExecContext(ctx, `INSERT INTO projects(name,sentry_key) VALUES(?,?)`, name, public)
	if err != nil {
		return 0, "", err
	}
	id, err := result.LastInsertId()
	return id, public, err
}
