// ==============================================================================
// CONFIG PACKAGE EXTENSIONS - pkg/config/config.go (FULL VERSION)
// ==============================================================================
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server       ServerConfig
	Database     DatabaseConfig
	Redis        RedisConfig
	JWT          JWTConfig
	TOTP         TOTPConfig
	Stellar      StellarConfig
	Ripple       RippleConfig
	Email        EmailConfig
	Verification VerificationConfig
	Security     SecurityConfig
	Risk         RiskConfig
	Compliance   ComplianceConfig
}

type RiskConfig struct {
	EnableCircuitBreaker    bool
	MaxDailyLimit           int64
	HighValueThreshold      int64
	MaxVelocityPerHour      int
	MaxVelocityPerDay       int
	SuspiciousLocationAlert string
	GlobalSystemPause       bool
	AdminApprovalThreshold  int64
	RestrictedCountries     []string
	EnableDisputeResolution bool
}

type ComplianceConfig struct {
	EnableSanctionsCheck bool
	EnableZKProof        bool
}

type ServerConfig struct {
	Host         string
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	UseTLS       bool
	CertFile     string
	KeyFile      string
	CAFile       string
}

type DatabaseConfig struct {
	URL             string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	URL      string
	Password string
	DB       int
}

type JWTConfig struct {
	Secret     string
	Expiration time.Duration
}

type TOTPConfig struct {
	Issuer string
	Period int
	Digits int
}

type StellarConfig struct {
	NetworkURL    string
	IssuerAccount string
	SecretKey     string
}

type RippleConfig struct {
	ServerURL     string
	IssuerAddress string
	SecretKey     string
}

type EmailConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPUseTLS   bool
}

type VerificationConfig struct {
	BaseURL         string
	TokenExpiration time.Duration
}

type SecurityConfig struct {
	SigningSecret  string
	RequireSigning bool
	SignatureTTL   time.Duration
}

func Load() *Config {
	dbURL := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/kyd_payment?sslmode=disable")
	sslMode := getEnv("DB_SSL_MODE", "verify-full")

	// Enforce SSL Mode if not present in URL, or override it
	if !strings.Contains(dbURL, "sslmode=") {
		if strings.Contains(dbURL, "?") {
			dbURL += "&sslmode=" + sslMode
		} else {
			dbURL += "?sslmode=" + sslMode
		}
	} else {
		// Optional: Force override to ensure security policy compliance
		// dbURL = regexp.MustCompile(`sslmode=[^&]+`).ReplaceAllString(dbURL, "sslmode="+sslMode)
	}

	return &Config{
		Server: ServerConfig{
			Host:         getEnv("SERVER_HOST", "0.0.0.0"),
			Port:         getEnv("SERVER_PORT", "8080"),
			ReadTimeout:  getDurationEnv("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout: getDurationEnv("SERVER_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:  getDurationEnv("SERVER_IDLE_TIMEOUT", 120*time.Second),
			UseTLS:       getBoolEnv("SERVER_USE_TLS", false),
			CertFile:     getEnv("SERVER_CERT_FILE", ""),
			KeyFile:      getEnv("SERVER_KEY_FILE", ""),
			CAFile:       getEnv("SERVER_CA_FILE", ""),
		},
		Database: DatabaseConfig{
			URL:             dbURL,
			SSLMode:         sslMode,
			MaxOpenConns:    getIntEnv("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getIntEnv("DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime: getDurationEnv("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Redis: RedisConfig{
			URL:      normalizeRedisURL(getEnv("REDIS_URL", "localhost:6379")),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getIntEnv("REDIS_DB", 0),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", "change-this-secret"),
			Expiration: getDurationEnv("JWT_EXPIRATION", 15*time.Minute),
		},
		TOTP: TOTPConfig{
			Issuer: getEnv("TOTP_ISSUER", "KYD"),
			Period: getIntEnv("TOTP_PERIOD", 30),
			Digits: getIntEnv("TOTP_DIGITS", 6),
		},
		Email: EmailConfig{
			SMTPHost:     getEnv("SMTP_HOST", "smtp.gmail.com"),
			SMTPPort:     getIntEnv("SMTP_PORT", 587),
			SMTPUsername: getEnv("SMTP_USERNAME", ""),
			SMTPPassword: getEnv("SMTP_PASSWORD", ""),
			SMTPFrom:     getEnv("SMTP_FROM", ""),
			SMTPUseTLS:   getBoolEnv("SMTP_USE_TLS", true),
		},
		Verification: VerificationConfig{
			BaseURL:         getEnv("VERIFICATION_BASE_URL", "http://localhost:9000/api/v1/auth/verify"),
			TokenExpiration: getDurationEnv("EMAIL_VERIFICATION_EXPIRATION", 24*time.Hour),
		},
		Stellar: StellarConfig{
			NetworkURL:    getEnv("STELLAR_NETWORK_URL", "https://horizon-testnet.stellar.org"),
			IssuerAccount: getEnv("STELLAR_ISSUER_ACCOUNT", ""),
			SecretKey:     getEnv("STELLAR_SECRET_KEY", ""),
		},
		Ripple: RippleConfig{
			ServerURL:     getEnv("RIPPLE_SERVER_URL", "wss://s.altnet.rippletest.net:51233"),
			IssuerAddress: getEnv("RIPPLE_ISSUER_ADDRESS", ""),
			SecretKey:     getEnv("RIPPLE_SECRET_KEY", ""),
		},
		Security: SecurityConfig{
			SigningSecret:  getEnv("SIGNING_SECRET", ""),
			RequireSigning: getBoolEnv("SIGNING_REQUIRED", false),
			SignatureTTL:   getDurationEnv("SIGNATURE_TTL", 5*time.Minute),
		},
		Risk: RiskConfig{
			EnableCircuitBreaker:    getBoolEnv("RISK_ENABLE_CIRCUIT_BREAKER", true),
			MaxDailyLimit:           int64(getIntEnv("RISK_MAX_DAILY_LIMIT", 100000000)),   // Default 100M atomic units
			HighValueThreshold:      int64(getIntEnv("RISK_HIGH_VALUE_THRESHOLD", 100000)), // Default 100k atomic units
			MaxVelocityPerHour:      getIntEnv("RISK_MAX_VELOCITY_PER_HOUR", 10),
			MaxVelocityPerDay:       getIntEnv("RISK_MAX_VELOCITY_PER_DAY", 50),
			SuspiciousLocationAlert: getEnv("RISK_SUSPICIOUS_LOCATION_ALERT", "North Korea"),
			GlobalSystemPause:       getBoolEnv("RISK_GLOBAL_SYSTEM_PAUSE", false),
			AdminApprovalThreshold:  int64(getIntEnv("RISK_ADMIN_APPROVAL_THRESHOLD", 500000)), // Default 500k atomic units
			RestrictedCountries:     getStringSliceEnv("RISK_RESTRICTED_COUNTRIES", "KP,IR,SY,CU"),
			EnableDisputeResolution: getBoolEnv("RISK_ENABLE_DISPUTE_RESOLUTION", true),
		},
		Compliance: ComplianceConfig{
			EnableSanctionsCheck: getBoolEnv("COMPLIANCE_ENABLE_SANCTIONS", true),
			EnableZKProof:        getBoolEnv("COMPLIANCE_ENABLE_ZK_PROOF", true),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func normalizeRedisURL(url string) string {
	// Strip redis:// or redis+tls:// scheme if present
	if strings.HasPrefix(url, "redis+tls://") {
		return url[len("redis+tls://"):]
	}
	if strings.HasPrefix(url, "redis://") {
		return url[len("redis://"):]
	}
	return url
}

func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "y", "on":
			return true
		case "0", "false", "no", "n", "off":
			return false
		}
	}
	return defaultValue
}

func getStringSliceEnv(key string, defaultValue string) []string {
	value := os.Getenv(key)
	if value == "" {
		value = defaultValue
	}
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
																														