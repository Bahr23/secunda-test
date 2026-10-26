package config_test

import (
	"math"
	"testing"
	"time"

	"github.com/bahr/secunda/config"
)

func TestResilienceConfiguration(t *testing.T) {
	t.Setenv("MYSQL_DSN", "test:test@tcp(localhost:3306)/test")
	t.Setenv("JWT_SECRET", "test-secret-at-least-thirty-two-bytes")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RateLimitEnabled || cfg.CacheBreakerFailures != 5 {
		t.Fatal("protections must be enabled by default")
	}
	for _, mutate := range []func(*config.Config){
		func(c *config.Config) { c.RateLimitRPS = 0 },
		func(c *config.Config) { c.RateLimitRPS = math.NaN() },
		func(c *config.Config) { c.AuthRateLimitRPS = math.Inf(1) },
		func(c *config.Config) { c.RateLimitBurst = 0 },
		func(c *config.Config) { c.RateLimitMaxKeys = 0 },
		func(c *config.Config) { c.RateLimitIdleTTL = time.Millisecond },
		func(c *config.Config) { c.CacheBreakerFailures = 0 },
		func(c *config.Config) { c.CacheBreakerTimeout = -time.Second },
	} {
		invalid := cfg
		mutate(&invalid)
		if invalid.Validate() == nil {
			t.Fatalf("invalid settings accepted: %+v", invalid.RateLimitRPS)
		}
	}
}
