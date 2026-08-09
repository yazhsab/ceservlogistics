// Package config performs strict environment parsing and validation.
//
// Every setting is explicit. Unparsable or out-of-range values are fatal at
// startup rather than at first use, and production mode additionally refuses to
// start on insecure defaults (short JWT secret, wildcard CORS, debug endpoints).
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment names.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config is the fully validated application configuration.
type Config struct {
	Env       string
	Service   ServiceConfig
	HTTP      HTTPConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Auth      AuthConfig
	Security  SecurityConfig
	Worker    WorkerConfig
	Telemetry TelemetryConfig
	Log       LogConfig
	Booking   BookingConfig
	Storage   StorageConfig
	// Market is the home market's defaults.
	Market MarketConfig
	// Notification wires real providers. Empty leaves every channel on the
	// logging sender.
	Notification NotificationConfig
}

// StorageConfig configures object storage for POD artifacts and generated
// documents (§31). Production uses S3; development falls back to the filesystem
// so a working stack does not require MinIO.
type StorageConfig struct {
	// Driver is "s3" or "filesystem".
	Driver       string
	Bucket       string
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	// Dir is the filesystem root when Driver is "filesystem".
	Dir string
	// MaxObjectBytes bounds one uploaded artifact.
	MaxObjectBytes int64
	// SignedURLTTL bounds how long a retrieval link stays valid.
	SignedURLTTL time.Duration
}

// ServiceConfig identifies the running binary.
type ServiceConfig struct {
	Name    string
	Version string
	Commit  string
	BuiltAt string
}

// HTTPConfig controls the API listener.
type HTTPConfig struct {
	Addr              string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
	MaxUploadBytes    int64
	CORSOrigins       []string
	CORSAllowCreds    bool
	TrustedProxyCIDRs []string
	EnableDebugRoutes bool
	EnableMetrics     bool
	MetricsAddr       string
}

// DatabaseConfig controls the PostgreSQL pool.
//
// Pool sizing for the 4 vCPU / 16 GB target (Constitution §28): the API pool
// defaults to 20 and the worker pool to 8. With two API replicas that is 48
// server-side connections against a max_connections of 100, leaving headroom
// for migrations, psql sessions and backups.
type DatabaseConfig struct {
	URL              string
	MaxConns         int32
	MinConns         int32
	MaxConnLifetime  time.Duration
	MaxConnIdleTime  time.Duration
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
	// SlowQueryThreshold is the duration past which a statement is logged.
	// Zero keeps the latency histogram and disables the log line.
	SlowQueryThreshold time.Duration
	HealthCheckPeriod  time.Duration
}

// RedisConfig controls the cache/rate-limit client.
type RedisConfig struct {
	URL          string
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	KeyPrefix    string
	Required     bool
}

// AuthConfig controls token lifetimes and credential policy.
type AuthConfig struct {
	JWTSecret            []byte
	JWTIssuer            string
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	SessionCacheTTL      time.Duration
	PasswordResetTTL     time.Duration
	PasswordMinLength    int
	MaxFailedLogins      int
	LoginLockoutDuration time.Duration
	LoginThrottleWindow  time.Duration
	LoginThrottleMax     int
	Argon2Time           uint32
	Argon2MemoryKiB      uint32
	Argon2Parallelism    uint8
	Argon2KeyLength      uint32
	BootstrapEmail       string
	BootstrapPassword    string
	BootstrapOrgCode     string
	BootstrapOrgName     string
}

// SecurityConfig controls transport-level protections.
type SecurityConfig struct {
	RateLimitEnabled   bool
	RateLimitPerMinute int
	RateLimitBurst     int
	// PartnerRateLimitPerMinute applies to an API key that does not carry its
	// own limit. Lower than the session default: a partner integration is a
	// program, and a program that needs more than this should be given an
	// explicit per-key allowance rather than raising the floor for everyone.
	PartnerRateLimitPerMinute int
	HSTSEnabled               bool
	HSTSMaxAge                time.Duration
}

// WorkerConfig controls the background job runner.
type WorkerConfig struct {
	// HealthAddr serves /livez, /readyz and /metrics for the worker process.
	HealthAddr      string
	Concurrency     int
	PollInterval    time.Duration
	BatchSize       int
	Queues          []string
	ShutdownTimeout time.Duration
	JobMaxAttempts  int
	StuckJobTimeout time.Duration
}

// TelemetryConfig controls OpenTelemetry export.
type TelemetryConfig struct {
	Enabled      bool
	OTLPEndpoint string
	OTLPInsecure bool
	SampleRatio  float64
}

// LogConfig controls the logger.
type LogConfig struct {
	Level  string
	Format string
}

// BookingConfig holds shipment booking policy knobs that operations may tune
// without a code change.
type BookingConfig struct {
	AWBPrefixDefault    string
	MaxPackagesPerShip  int
	QuoteCacheTTL       time.Duration
	RoutingCacheTTL     time.Duration
	ExplanationTTL      time.Duration
	AllowBackdatedHours int
	// PublicTrackingURL is the customer-facing tracking page. It goes into
	// notification bodies, so it must be the address a recipient can actually
	// open, not the API base.
	PublicTrackingURL string
}

// MarketConfig is the home market: what a tenant becomes when it does not say.
//
// Nigeria is first. These are defaults, not constants — the platform is
// multi-market and an organization states its own — but a value that is only
// ever implied is a value nobody notices is wrong, which is how INR amounts
// would have been written for Lagos.
type MarketConfig struct {
	// Country is ISO-3166 alpha-2. Decides tax identifier validation, address
	// defaults and which reference data a request without a country means.
	Country  string
	Currency string
	Timezone string
}

// NotificationConfig wires real providers.
//
// A channel left unconfigured keeps its logging sender, which records what it
// would have sent. That is deliberately not the same as "no sender": a missing
// sender *suppresses* the message, and an operator investigating would rather
// see the composed text than a row saying nothing happened.
type NotificationConfig struct {
	// Termii serves SMS and WhatsApp in Nigeria from one account. Nigeria has
	// no per-template registration requirement — the NCC registers the sender
	// id, not the message — so SMS needs no template handle, unlike India.
	TermiiAPIKey   string
	TermiiBaseURL  string
	TermiiSenderID string
	// TermiiSMSChannel is "dnd" or "generic". Default "dnd": a transactional
	// courier notification must reach a subscriber who has blocked promotional
	// SMS, and a delivery update is not marketing.
	TermiiSMSChannel string
	TermiiEnableSMS  bool
	// TermiiWhatsAppFrom is the registered WhatsApp number. Empty leaves the
	// channel on its logging sender rather than half-enabling it.
	TermiiWhatsAppFrom string
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	p := &parser{}
	env := p.str("APP_ENV", EnvDevelopment)
	switch env {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
	default:
		p.errf("APP_ENV must be one of development|test|staging|production, got %q", env)
	}
	isProd := env == EnvProduction || env == EnvStaging

	cfg := &Config{
		Env: env,
		Service: ServiceConfig{
			Name:    p.str("SERVICE_NAME", "courier-os"),
			Version: p.str("SERVICE_VERSION", "dev"),
			Commit:  p.str("GIT_COMMIT", "unknown"),
			BuiltAt: p.str("BUILT_AT", "unknown"),
		},
		HTTP: HTTPConfig{
			Addr:              p.str("HTTP_ADDR", ":8080"),
			ReadTimeout:       p.dur("HTTP_READ_TIMEOUT", 15*time.Second),
			ReadHeaderTimeout: p.dur("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			WriteTimeout:      p.dur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       p.dur("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   p.dur("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			MaxBodyBytes:      p.i64("HTTP_MAX_BODY_BYTES", 1<<20),    // 1 MiB
			MaxUploadBytes:    p.i64("HTTP_MAX_UPLOAD_BYTES", 25<<20), // 25 MiB
			CORSOrigins:       p.csv("CORS_ALLOWED_ORIGINS", nil),
			CORSAllowCreds:    p.boolean("CORS_ALLOW_CREDENTIALS", false),
			TrustedProxyCIDRs: p.csv("TRUSTED_PROXY_CIDRS", []string{"127.0.0.1/32", "::1/128"}),
			EnableDebugRoutes: p.boolean("ENABLE_DEBUG_ROUTES", !isProd),
			EnableMetrics:     p.boolean("ENABLE_METRICS", true),
			MetricsAddr:       p.str("METRICS_ADDR", "127.0.0.1:9090"),
		},
		Database: DatabaseConfig{
			URL:              p.str("DATABASE_URL", ""),
			MaxConns:         int32(p.integer("DATABASE_MAX_CONNS", 20)),
			MinConns:         int32(p.integer("DATABASE_MIN_CONNS", 2)),
			MaxConnLifetime:  p.dur("DATABASE_MAX_CONN_LIFETIME", 55*time.Minute),
			MaxConnIdleTime:  p.dur("DATABASE_MAX_CONN_IDLE_TIME", 5*time.Minute),
			ConnectTimeout:   p.dur("DATABASE_CONNECT_TIMEOUT", 5*time.Second),
			StatementTimeout: p.dur("DATABASE_STATEMENT_TIMEOUT", 15*time.Second),
			// 250 ms: on a 4 vCPU host with a 10-connection pool, a statement
			// this slow is already consuming a tenth of the pool's capacity
			// for a tenth of a second. Well below the statement timeout, so it
			// warns long before anything is killed.
			SlowQueryThreshold: p.dur("DATABASE_SLOW_QUERY_THRESHOLD", 250*time.Millisecond),
			HealthCheckPeriod:  p.dur("DATABASE_HEALTHCHECK_PERIOD", 30*time.Second),
		},
		Redis: RedisConfig{
			URL:          p.str("REDIS_URL", "redis://localhost:6379/0"),
			PoolSize:     p.integer("REDIS_POOL_SIZE", 16),
			MinIdleConns: p.integer("REDIS_MIN_IDLE_CONNS", 2),
			DialTimeout:  p.dur("REDIS_DIAL_TIMEOUT", 3*time.Second),
			ReadTimeout:  p.dur("REDIS_READ_TIMEOUT", 2*time.Second),
			WriteTimeout: p.dur("REDIS_WRITE_TIMEOUT", 2*time.Second),
			KeyPrefix:    p.str("REDIS_KEY_PREFIX", "cos:"),
			Required:     p.boolean("REDIS_REQUIRED", isProd),
		},
		Auth: AuthConfig{
			JWTSecret:            []byte(p.str("JWT_SECRET", "")),
			JWTIssuer:            p.str("JWT_ISSUER", "courier-os"),
			AccessTokenTTL:       p.dur("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL:      p.dur("REFRESH_TOKEN_TTL", 720*time.Hour),
			SessionCacheTTL:      p.dur("SESSION_CACHE_TTL", 30*time.Second),
			PasswordResetTTL:     p.dur("PASSWORD_RESET_TTL", 30*time.Minute),
			PasswordMinLength:    p.integer("PASSWORD_MIN_LENGTH", 12),
			MaxFailedLogins:      p.integer("MAX_FAILED_LOGINS", 5),
			LoginLockoutDuration: p.dur("LOGIN_LOCKOUT_DURATION", 15*time.Minute),
			LoginThrottleWindow:  p.dur("LOGIN_THROTTLE_WINDOW", time.Minute),
			LoginThrottleMax:     p.integer("LOGIN_THROTTLE_MAX", 10),
			Argon2Time:           uint32(p.integer("ARGON2_TIME", 3)),
			Argon2MemoryKiB:      uint32(p.integer("ARGON2_MEMORY_KIB", 65536)),
			Argon2Parallelism:    uint8(p.integer("ARGON2_PARALLELISM", 2)),
			Argon2KeyLength:      uint32(p.integer("ARGON2_KEY_LENGTH", 32)),
			BootstrapEmail:       p.str("BOOTSTRAP_ADMIN_EMAIL", ""),
			BootstrapPassword:    p.str("BOOTSTRAP_ADMIN_PASSWORD", ""),
			BootstrapOrgCode:     p.str("BOOTSTRAP_ORG_CODE", "PLATFORM"),
			BootstrapOrgName:     p.str("BOOTSTRAP_ORG_NAME", "Platform Operator"),
		},
		Security: SecurityConfig{
			RateLimitEnabled:          p.boolean("RATE_LIMIT_ENABLED", true),
			RateLimitPerMinute:        p.integer("RATE_LIMIT_PER_MINUTE", 600),
			RateLimitBurst:            p.integer("RATE_LIMIT_BURST", 120),
			PartnerRateLimitPerMinute: p.integer("PARTNER_RATE_LIMIT_PER_MINUTE", 120),
			HSTSEnabled:               p.boolean("HSTS_ENABLED", isProd),
			HSTSMaxAge:                p.dur("HSTS_MAX_AGE", 180*24*time.Hour),
		},
		Worker: WorkerConfig{
			Concurrency: p.integer("WORKER_CONCURRENCY", 4),
			// Bound to all interfaces on the compose network so the container
			// healthcheck can reach it; not published to the host.
			HealthAddr:      p.str("WORKER_HEALTH_ADDR", "0.0.0.0:8090"),
			PollInterval:    p.dur("WORKER_POLL_INTERVAL", 2*time.Second),
			BatchSize:       p.integer("WORKER_BATCH_SIZE", 10),
			Queues:          p.csv("WORKER_QUEUES", []string{"default", "import", "notifications", "webhooks", "reports"}),
			ShutdownTimeout: p.dur("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second),
			JobMaxAttempts:  p.integer("WORKER_JOB_MAX_ATTEMPTS", 5),
			StuckJobTimeout: p.dur("WORKER_STUCK_JOB_TIMEOUT", 15*time.Minute),
		},
		Telemetry: TelemetryConfig{
			Enabled:      p.boolean("OTEL_ENABLED", false),
			OTLPEndpoint: p.str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			OTLPInsecure: p.boolean("OTEL_EXPORTER_OTLP_INSECURE", true),
			SampleRatio:  p.f64("OTEL_TRACES_SAMPLER_RATIO", 0.05),
		},
		Log: LogConfig{
			Level:  p.str("LOG_LEVEL", "info"),
			Format: p.str("LOG_FORMAT", "json"),
		},
		Booking: BookingConfig{
			AWBPrefixDefault:    p.str("AWB_PREFIX_DEFAULT", "CSV"),
			MaxPackagesPerShip:  p.integer("MAX_PACKAGES_PER_SHIPMENT", 50),
			QuoteCacheTTL:       p.dur("QUOTE_CACHE_TTL", 60*time.Second),
			RoutingCacheTTL:     p.dur("ROUTING_CACHE_TTL", 5*time.Minute),
			ExplanationTTL:      p.dur("ROUTING_EXPLANATION_TTL", time.Hour),
			AllowBackdatedHours: p.integer("BOOKING_BACKDATE_HOURS", 0),
			PublicTrackingURL:   p.str("PUBLIC_TRACKING_URL", "https://track.example.com/"),
		},
		Market: MarketConfig{
			Country:  strings.ToUpper(p.str("DEFAULT_COUNTRY", "NG")),
			Currency: strings.ToUpper(p.str("DEFAULT_CURRENCY", "NGN")),
			Timezone: p.str("DEFAULT_TIMEZONE", "Africa/Lagos"),
		},
		Notification: NotificationConfig{
			TermiiAPIKey:       p.str("TERMII_API_KEY", ""),
			TermiiBaseURL:      p.str("TERMII_BASE_URL", "https://api.ng.termii.com"),
			TermiiSenderID:     p.str("TERMII_SENDER_ID", ""),
			TermiiSMSChannel:   p.str("TERMII_SMS_CHANNEL", "dnd"),
			TermiiEnableSMS:    p.boolean("TERMII_ENABLE_SMS", true),
			TermiiWhatsAppFrom: p.str("TERMII_WHATSAPP_FROM", ""),
		},
		Storage: StorageConfig{
			Driver:         p.str("STORAGE_DRIVER", "filesystem"),
			Bucket:         p.str("STORAGE_BUCKET", "courier-os"),
			Endpoint:       p.str("STORAGE_ENDPOINT", ""),
			Region:         p.str("STORAGE_REGION", "us-east-1"),
			AccessKey:      p.str("STORAGE_ACCESS_KEY", ""),
			SecretKey:      p.str("STORAGE_SECRET_KEY", ""),
			UsePathStyle:   p.boolean("STORAGE_USE_PATH_STYLE", true),
			Dir:            p.str("STORAGE_DIR", "/var/lib/courier-os/objects"),
			MaxObjectBytes: p.i64("STORAGE_MAX_OBJECT_BYTES", 8<<20),
			SignedURLTTL:   p.dur("STORAGE_SIGNED_URL_TTL", 15*time.Minute),
		},
	}

	cfg.validate(p, isProd)
	if err := p.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate(p *parser, isProd bool) {
	// Object storage: production must not fall back to the container filesystem,
	// where POD evidence would vanish on the next deploy (§31, §39).
	switch c.Storage.Driver {
	case "s3":
		if c.Storage.Endpoint == "" || c.Storage.AccessKey == "" || c.Storage.SecretKey == "" {
			p.errf("STORAGE_DRIVER=s3 requires STORAGE_ENDPOINT, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY")
		}
	case "filesystem":
		if isProd {
			p.errf("STORAGE_DRIVER must be s3 in production; filesystem storage does not survive a container restart")
		}
	default:
		p.errf("STORAGE_DRIVER must be s3 or filesystem (got %q)", c.Storage.Driver)
	}
	if c.Storage.MaxObjectBytes < 1<<10 || c.Storage.MaxObjectBytes > 64<<20 {
		p.errf("STORAGE_MAX_OBJECT_BYTES must be between 1 KiB and 64 MiB")
	}

	if c.Database.URL == "" {
		p.errf("DATABASE_URL is required")
	} else if _, err := url.Parse(c.Database.URL); err != nil {
		p.errf("DATABASE_URL is not a valid URL")
	}
	if c.Database.MaxConns < 1 || c.Database.MaxConns > 200 {
		p.errf("DATABASE_MAX_CONNS must be between 1 and 200 (got %d); the 4 vCPU target should stay at or below 25 per instance", c.Database.MaxConns)
	}
	if c.Database.MinConns < 0 || c.Database.MinConns > c.Database.MaxConns {
		p.errf("DATABASE_MIN_CONNS must be between 0 and DATABASE_MAX_CONNS")
	}
	if len(c.Auth.JWTSecret) == 0 {
		p.errf("JWT_SECRET is required")
	} else if len(c.Auth.JWTSecret) < 32 {
		p.errf("JWT_SECRET must be at least 32 bytes")
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.AccessTokenTTL > time.Hour {
		p.errf("ACCESS_TOKEN_TTL must be > 0 and <= 1h")
	}
	if c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL {
		p.errf("REFRESH_TOKEN_TTL must exceed ACCESS_TOKEN_TTL")
	}
	if c.Auth.PasswordMinLength < 8 {
		p.errf("PASSWORD_MIN_LENGTH must be at least 8")
	}
	if c.Auth.Argon2MemoryKiB < 16384 {
		p.errf("ARGON2_MEMORY_KIB must be at least 16384 (16 MiB)")
	}
	if c.Auth.Argon2Time < 1 {
		p.errf("ARGON2_TIME must be at least 1")
	}
	if c.Auth.Argon2Parallelism < 1 {
		p.errf("ARGON2_PARALLELISM must be at least 1")
	}
	if c.Worker.Concurrency < 1 || c.Worker.Concurrency > 64 {
		p.errf("WORKER_CONCURRENCY must be between 1 and 64")
	}
	if c.Telemetry.SampleRatio < 0 || c.Telemetry.SampleRatio > 1 {
		p.errf("OTEL_TRACES_SAMPLER_RATIO must be between 0 and 1")
	}
	if c.Telemetry.Enabled && c.Telemetry.OTLPEndpoint == "" {
		p.errf("OTEL_EXPORTER_OTLP_ENDPOINT is required when OTEL_ENABLED=true")
	}
	if l := len(c.Booking.AWBPrefixDefault); l < 2 || l > 4 {
		p.errf("AWB_PREFIX_DEFAULT must be 2-4 characters")
	}
	for _, o := range c.HTTP.CORSOrigins {
		if o == "*" {
			if isProd {
				p.errf("CORS_ALLOWED_ORIGINS must not contain '*' in staging/production")
			}
			continue
		}
		u, err := url.Parse(o)
		if err != nil || u.Scheme == "" || u.Host == "" {
			p.errf("CORS_ALLOWED_ORIGINS entry %q must be a full origin such as https://app.example.com", o)
		}
	}
	if c.HTTP.CORSAllowCreds {
		for _, o := range c.HTTP.CORSOrigins {
			if o == "*" {
				p.errf("CORS_ALLOW_CREDENTIALS cannot be combined with a wildcard origin")
			}
		}
	}
	if isProd {
		if c.HTTP.EnableDebugRoutes {
			p.errf("ENABLE_DEBUG_ROUTES must be false in staging/production")
		}
		if c.Auth.BootstrapPassword != "" && len(c.Auth.BootstrapPassword) < c.Auth.PasswordMinLength {
			p.errf("BOOTSTRAP_ADMIN_PASSWORD is shorter than PASSWORD_MIN_LENGTH")
		}
		if !c.Security.RateLimitEnabled {
			p.errf("RATE_LIMIT_ENABLED must be true in staging/production")
		}
	}
	if (c.Auth.BootstrapEmail == "") != (c.Auth.BootstrapPassword == "") {
		p.errf("BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD must be set together")
	}
}

// IsProduction reports whether the process runs with production semantics.
func (c *Config) IsProduction() bool {
	return c.Env == EnvProduction || c.Env == EnvStaging
}

// ---- parsing helpers -------------------------------------------------------

type parser struct{ problems []string }

func (p *parser) errf(format string, args ...any) {
	p.problems = append(p.problems, fmt.Sprintf(format, args...))
}

func (p *parser) err() error {
	if len(p.problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(p.problems, "\n  - "))
}

func (p *parser) raw(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

func (p *parser) str(key, def string) string {
	if v, ok := p.raw(key); ok {
		return v
	}
	return def
}

func (p *parser) integer(key string, def int) int {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.errf("%s must be an integer, got %q", key, v)
		return def
	}
	return n
}

func (p *parser) i64(key string, def int64) int64 {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		p.errf("%s must be an integer, got %q", key, v)
		return def
	}
	return n
}

func (p *parser) f64(key string, def float64) float64 {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		p.errf("%s must be a number, got %q", key, v)
		return def
	}
	return n
}

func (p *parser) boolean(key string, def bool) bool {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errf("%s must be a boolean (true/false), got %q", key, v)
		return def
	}
	return b
}

func (p *parser) dur(key string, def time.Duration) time.Duration {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.errf("%s must be a duration such as 15m or 30s, got %q", key, v)
		return def
	}
	if d < 0 {
		p.errf("%s must not be negative", key)
		return def
	}
	return d
}

func (p *parser) csv(key string, def []string) []string {
	v, ok := p.raw(key)
	if !ok {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
