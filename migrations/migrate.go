package migrations

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var files embed.FS

// Up uses a separate connection; MySQL DDL implicitly commits. golang-migrate
// records a dirty version on failure and serializes migrations with a DB lock.
func Up(dsn string) error {
	source, err := iofs.New(files, ".")
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, "mysql://"+dsn)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("initialize migrations: %w", err)
	}
	err = m.Up()
	sourceErr, dbErr := m.Close()
	if errors.Is(err, migrate.ErrNoChange) {
		err = nil
	}
	return errors.Join(err, sourceErr, dbErr)
}
