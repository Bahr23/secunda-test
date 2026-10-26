package persistent

import (
	"context"
	"fmt"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type Users struct{ q querier }

func (r *Users) Create(ctx context.Context, u *entity.User) error {
	result, err := r.q.ExecContext(ctx, `INSERT INTO users (email, password_hash, name, created_at) VALUES (?, ?, ?, ?)`, u.Email, u.PasswordHash, u.Name, u.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert user: %w", mapError(err))
	}
	u.ID, err = result.LastInsertId()
	return err
}

func (r *Users) ByEmail(ctx context.Context, email string) (entity.User, error) {
	var u entity.User
	err := r.q.QueryRowContext(ctx, `SELECT id, email, password_hash, name, created_at FROM users WHERE email = ?`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt)
	return u, mapError(err)
}

var _ usecase.UserRepository = (*Users)(nil)
