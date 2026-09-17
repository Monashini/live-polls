// Package config loads and validates every runtime setting from the
// environment. Nothing else in the codebase calls os.Getenv, so there is
// exactly one place to look for "what can be configured" and exactly one
// place that decides whether the process is allowed to start.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// minJWTSecretLen is enforced because HS256 security depends entirely on the
// secret's entropy. A short secret is brute-forceable offline.
const minJWTSecretLen = 32

type Config struct {
	Env    string
	Port   string
	IsProd bool

	MongoURI      string
	MongoDatabase string

	// RedisURL carries host, credentials and TLS in one value. "rediss://"
	// (two esses) selects TLS, which every hosted provider requires.
	RedisURL string

	// CountsTTL bounds how long an untouched poll's counters stay in Redis.
	// Redis is a cache here; expiry keeps memory bounded and a miss just
	// costs one rebuild from MongoDB.
	CountsTTL time.Duration

	// VoteRateLimit / VoteRateWindow throttle votes per client IP. This is a
	// different control from the duplicate-vote index: that stops one person
	// voting twice on one poll, this stops a script hammering many polls.
	VoteRateLimit  int64
	VoteRateWindow time.Duration

	JWTSecret []byte
	JWTTTL    time.Duration

	AllowedOrigins []string
	TrustedProxies []string

	CookieSecure   bool
	CookieSameSite string
	CookieDomain   string

	IPHashSalt []byte
}

// Load reads configuration from the process environment and fails loudly if
// anything required is missing or nonsensical. Failing at startup is far
// better than discovering a missing JWT secret on the first login attempt.
func Load() (*Config, error) {
	// godotenv.Load populates the environment from backend/.env if that file
	// exists. It deliberately does NOT overwrite variables that are already
	// set, so a real deployment's environment always wins over a stray file.
	// The error is ignored because "no .env file" is the normal case in prod.
	_ = godotenv.Load()

	env := strings.ToLower(envOr("APP_ENV", "development"))

	cfg := &Config{
		Env:           env,
		IsProd:        env == "production",
		Port:          envOr("PORT", "8080"),
		MongoURI:      strings.TrimSpace(os.Getenv("MONGODB_URI")),
		MongoDatabase: envOr("MONGODB_DATABASE", "livepolls"),
		CookieDomain:  strings.TrimSpace(os.Getenv("COOKIE_DOMAIN")),
	}

	if cfg.MongoURI == "" {
		return nil, fmt.Errorf("MONGODB_URI is required")
	}
	if cfg.MongoDatabase == "" {
		return nil, fmt.Errorf("MONGODB_DATABASE must not be empty")
	}

	cfg.RedisURL = strings.TrimSpace(os.Getenv("REDIS_URL"))
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	var err error

	cfg.CountsTTL, err = durationOr("REDIS_COUNTS_TTL", 24*time.Hour)
	if err != nil {
		return nil, err
	}

	cfg.VoteRateLimit, err = intOr("VOTE_RATE_LIMIT", 30)
	if err != nil {
		return nil, err
	}
	cfg.VoteRateWindow, err = durationOr("VOTE_RATE_WINDOW", 5*time.Minute)
	if err != nil {
		return nil, err
	}

	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if len(secret) < minJWTSecretLen {
		return nil, fmt.Errorf("JWT_SECRET is required and must be at least %d characters (got %d)", minJWTSecretLen, len(secret))
	}
	cfg.JWTSecret = []byte(secret)

	ttl, err := time.ParseDuration(envOr("JWT_TTL", "24h")) //nolint:staticcheck // err reused
	if err != nil {
		return nil, fmt.Errorf("JWT_TTL is not a valid duration (try 24h): %w", err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("JWT_TTL must be positive")
	}
	cfg.JWTTTL = ttl

	cfg.AllowedOrigins = splitList(os.Getenv("CORS_ALLOWED_ORIGINS"))
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			// A wildcard is incompatible with credentialed requests, and the
			// voter cookie makes every vote a credentialed request.
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS must list exact origins, not '*'")
		}
		if !strings.HasPrefix(o, "http://") && !strings.HasPrefix(o, "https://") {
			return nil, fmt.Errorf("CORS origin %q must include a scheme, e.g. https://example.com", o)
		}
		if strings.HasSuffix(o, "/") {
			return nil, fmt.Errorf("CORS origin %q must not have a trailing slash; browsers send the origin without one", o)
		}
	}

	cfg.TrustedProxies = splitList(os.Getenv("TRUSTED_PROXIES"))

	cfg.CookieSecure, err = boolOr("COOKIE_SECURE", cfg.IsProd)
	if err != nil {
		return nil, err
	}

	cfg.CookieSameSite = strings.ToLower(envOr("COOKIE_SAMESITE", "lax"))
	switch cfg.CookieSameSite {
	case "lax", "strict", "none":
	default:
		return nil, fmt.Errorf("COOKIE_SAMESITE must be one of lax, strict, none (got %q)", cfg.CookieSameSite)
	}
	if cfg.CookieSameSite == "none" && !cfg.CookieSecure {
		// Browsers silently drop SameSite=None cookies that are not Secure.
		// Catching it here saves hours of "why isn't the cookie being sent".
		return nil, fmt.Errorf("COOKIE_SAMESITE=none requires COOKIE_SECURE=true; browsers reject the cookie otherwise")
	}

	// The salt only needs to be unguessable and stable. Reusing the JWT secret
	// as a fallback keeps local setup to one variable without ever storing a
	// raw IP address.
	if salt := strings.TrimSpace(os.Getenv("IP_HASH_SALT")); salt != "" {
		cfg.IPHashSalt = []byte(salt)
	} else {
		cfg.IPHashSalt = []byte("ip-salt:" + secret)
	}

	return cfg, nil
}

func durationOr(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid duration (try 24h, 5m): %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return d, nil
}

func intOr(key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer (got %q)", key, raw)
	}
	return n, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func boolOr(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false (got %q)", key, raw)
	}
	return v, nil
}

// splitList turns "a, b ,c" into ["a","b","c"], dropping empties.
func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
