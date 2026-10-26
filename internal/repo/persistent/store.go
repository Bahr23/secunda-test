package persistent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store                    { return &Store{db: db} }
func (s *Store) Users() *Users                 { return &Users{q: s.db} }
func (s *Store) Teams() usecase.TeamRepository { return &Teams{q: s.db} }
func (s *Store) Tasks() usecase.TaskRepository { return &Tasks{q: s.db} }

type transaction struct{ q *sql.Tx }

func (t transaction) Teams() usecase.TeamWriter { return &teamWriter{q: t.q} }
func (t transaction) Tasks() usecase.TaskWriter { return &taskWriter{q: t.q} }

func (s *Store) Within(ctx context.Context, fn func(usecase.Transaction) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(transaction{q: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", mapError(err))
	}
	return nil
}

func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return entity.ErrNotFound
	}
	var dbErr *mysql.MySQLError
	if errors.As(err, &dbErr) {
		switch dbErr.Number {
		case 1062:
			return entity.ErrConflict
		case 1452:
			return entity.ErrNotFound
		case 1205, 1213:
			return fmt.Errorf("%w: concurrent transaction; retry", entity.ErrConflict)
		}
	}
	return err
}

var _ usecase.Transactor = (*Store)(nil)
