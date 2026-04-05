package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Exists(ctx context.Context, telegramID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, telegramID).Scan(&exists)
	return exists, err
}

func (r *UserRepo) Upsert(ctx context.Context, telegramID int64, username string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, username) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`,
		telegramID, username,
	)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}
