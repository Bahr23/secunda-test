// Package app is the composition root: it wires adapters to application ports.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/bahr/secunda/config"
	v1 "github.com/bahr/secunda/internal/controller/http/v1"
	"github.com/bahr/secunda/internal/repo/cache"
	"github.com/bahr/secunda/internal/repo/persistent"
	"github.com/bahr/secunda/internal/usecase"
	"github.com/bahr/secunda/pkg/auth"
	"github.com/bahr/secunda/pkg/mysql"
)

func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	db, err := mysql.Open(startup, cfg.MySQLDSN, cfg.DBMaxOpen, cfg.DBMaxIdle)
	cancel()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	taskCache, err := cache.NewTasks(cfg.RedisURL, log, cache.Options{Timeout: cfg.CacheTimeout, FailureThreshold: cfg.CacheBreakerFailures, OpenTimeout: cfg.CacheBreakerTimeout})
	if err != nil {
		return err
	}
	defer func() { _ = taskCache.Close() }()
	if err := taskCache.Ping(ctx); err != nil {
		log.Warn("Redis is unavailable; using MySQL directly", slog.Any("error", err))
	}

	store := persistent.New(db)
	tokens := auth.NewJWT(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTTTL)

	authService, err := usecase.NewAuth(store.Users(), auth.Passwords{Cost: cfg.BcryptCost}, tokens)
	if err != nil {
		return err
	}

	teams := usecase.NewTeam(store.Teams(), store)

	tasks := usecase.NewTask(store.Tasks(), store.Teams(), store, taskCache)

	options := v1.Options{Logger: log, RequestTimeout: cfg.HTTPTimeout, CursorSecret: []byte(cfg.JWTSecret)}
	if cfg.RateLimitEnabled {
		options.RateLimit = &v1.RateLimitOptions{RequestsPerSecond: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst,
			AuthRequestsPerSecond: cfg.AuthRateLimitRPS, AuthBurst: cfg.AuthRateLimitBurst,
			MaxKeys: cfg.RateLimitMaxKeys, IdleTTL: cfg.RateLimitIdleTTL}
	}
	router := v1.New(v1.Dependencies{Auth: authService, Teams: teams, Tasks: tasks, Tokens: tokens, Ready: db.PingContext}, options)
	server := &http.Server{Addr: cfg.HTTPAddress, Handler: router,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.HTTPTimeout,
		WriteTimeout: cfg.HTTPTimeout + 5*time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError)}

	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return serveHTTP(ctx, server, listener, cfg.ShutdownTimeout, log)
}
