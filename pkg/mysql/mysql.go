package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

func Open(ctx context.Context, dsn string, maxOpen, maxIdle int) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL DSN")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure MySQL connection")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(3 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	return db, nil
}
