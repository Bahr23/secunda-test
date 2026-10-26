package config

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	HTTPAddress          string        `env:"HTTP_ADDRESS" env-default:":8080"`
	HTTPTimeout          time.Duration `env:"HTTP_TIMEOUT" env-default:"10s"`
	ShutdownTimeout      time.Duration `env:"SHUTDOWN_TIMEOUT" env-default:"15s"`
	MySQLDSN             string        `env:"MYSQL_DSN" env-required:"true"`
	DBMaxOpen            int           `env:"DB_MAX_OPEN" env-default:"20"`
	DBMaxIdle            int           `env:"DB_MAX_IDLE" env-default:"10"`
	RedisURL             string        `env:"REDIS_URL" env-default:"redis://localhost:6379/0"`
	CacheTimeout         time.Duration `env:"CACHE_TIMEOUT" env-default:"150ms"`
	CacheBreakerFailures uint32        `env:"CACHE_BREAKER_FAILURES" env-default:"5"`
	CacheBreakerTimeout  time.Duration `env:"CACHE_BREAKER_TIMEOUT" env-default:"10s"`
	RateLimitEnabled     bool          `env:"RATE_LIMIT_ENABLED" env-default:"true"`
	RateLimitRPS         float64       `env:"RATE_LIMIT_RPS" env-default:"20"`
	RateLimitBurst       int           `env:"RATE_LIMIT_BURST" env-default:"100"`
	AuthRateLimitRPS     float64       `env:"AUTH_RATE_LIMIT_RPS" env-default:"1"`
	AuthRateLimitBurst   int           `env:"AUTH_RATE_LIMIT_BURST" env-default:"10"`
	RateLimitMaxKeys     int           `env:"RATE_LIMIT_MAX_KEYS" env-default:"10000"`
	RateLimitIdleTTL     time.Duration `env:"RATE_LIMIT_IDLE_TTL" env-default:"10m"`
	JWTSecret            string        `env:"JWT_SECRET" env-required:"true"`
	JWTIssuer            string        `env:"JWT_ISSUER" env-default:"secunda"`
	JWTAudience          string        `env:"JWT_AUDIENCE" env-default:"secunda-api"`
	JWTTTL               time.Duration `env:"JWT_TTL" env-default:"24h"`
	BcryptCost           int           `env:"BCRYPT_COST" env-default:"12"`
	LogLevel             string        `env:"LOG_LEVEL" env-default:"info"`
	LogFormat            string        `env:"LOG_FORMAT" env-default:"json"`
}

func Load() (Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return cfg, fmt.Errorf("read configuration: %w", err)
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must contain at least 32 bytes")
	}
	if c.JWTIssuer == "" || c.JWTAudience == "" || c.HTTPAddress == "" || c.MySQLDSN == "" {
		return fmt.Errorf("issuer, audience, HTTP address and MySQL DSN must not be empty")
	}
	if c.JWTTTL <= 0 || c.HTTPTimeout <= 0 || c.ShutdownTimeout <= 0 || c.CacheTimeout <= 0 {
		return fmt.Errorf("timeouts and JWT_TTL must be positive")
	}
	if c.DBMaxOpen < 1 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return fmt.Errorf("invalid database pool limits")
	}
	if c.BcryptCost < 10 || c.BcryptCost > 16 {
		return fmt.Errorf("BCRYPT_COST must be between 10 and 16")
	}
	if c.CacheBreakerFailures == 0 || c.CacheBreakerTimeout <= 0 {
		return fmt.Errorf("cache breaker threshold and timeout must be positive")
	}
	if c.RateLimitEnabled {
		for _, policy := range []struct {
			rate  float64
			burst int
		}{{c.RateLimitRPS, c.RateLimitBurst}, {c.AuthRateLimitRPS, c.AuthRateLimitBurst}} {
			if policy.rate <= 0 || math.IsNaN(policy.rate) || math.IsInf(policy.rate, 0) || policy.burst < 1 || c.RateLimitIdleTTL.Seconds() < float64(policy.burst)/policy.rate {
				return fmt.Errorf("rate limits require a positive finite rate/burst and idle TTL covering a full refill")
			}
		}
		if c.RateLimitMaxKeys < 1 || c.RateLimitIdleTTL <= 0 {
			return fmt.Errorf("rate limiter capacity and idle TTL must be positive")
		}
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return fmt.Errorf("LOG_FORMAT must be json or text")
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}
	return nil
}
