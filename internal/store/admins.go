package store

import (
	"context"
	"database/sql"
	"time"
)

type Admin struct {
	ID             int64
	Username       string
	PasswordHash   string
	SessionVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (*Admin, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, session_version, created_at, updated_at
		 FROM admins WHERE username = ?`, username)
	var a Admin
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.SessionVersion, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	return &a, nil
}

func (s *Store) GetAdmin(ctx context.Context, id int64) (*Admin, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, session_version, created_at, updated_at
		 FROM admins WHERE id = ?`, id)
	var a Admin
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.SessionVersion, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	return &a, nil
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admins (username, password_hash) VALUES (?, ?)`,
		username, passwordHash)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE admins SET password_hash = ?, session_version = session_version + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`, passwordHash, id)
	return err
}

// for tests / readability
var _ = sql.ErrNoRows
