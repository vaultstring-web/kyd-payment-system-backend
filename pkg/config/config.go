// ==============================================================================
// CONFIG PACKAGE EXTENSIONS - pkg/config/config.go (FULL VERSION)
// ==============================================================================
package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Config struct {
	Server       ServerConfig
	Database     DatabaseConfig
	Redis        RedisConfig
	JWT          JWTConfig
	Stellar      StellarConfig
	Ripple       RippleConfig
	Email        EmailConfig
	Verification VerificationConfig
	FileUpload   FileUploadConfig
	AML          AMLConfig
	VirusScan    VirusScanConfig
	KYC          KYCConfig
}

type ServerConfig struct {
	Host         string
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type KYCConfig struct {
	MaxFileSize        int64
	AllowedFileTypes   []string
	DocumentRetention  time.Duration
	AutoApproveEnabled bool
	AutoApproveLimit   decimal.Decimal
}

type DatabaseConfig struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// ========== File Upload Configuration ==========
type FileUploadConfig struct {
	StorageType     string // "local", "s3", "minio"
	LocalUploadDir  string
	MaxUploadSize   int64
	PresignedURLExp time.Duration
	CDNBaseURL      string
}

// ========== AML Configuration ==========
type AMLConfig struct {
	Enabled        bool
	Provider       string // "mock", "sanction-screening", "local"
	CheckThreshold decimal.Decimal
	AutoBlock      bool
	APIKey         string
	Endpoint       string
}

// ========== Virus Scan Configuration ==========
type VirusScanConfig struct {
	Enabled       bool
	Engine        string // "mock", "clamav", "cloud-scan"
	ClamAVHost    string
	ClamAVPort    int
	ScanTimeout   time.Duration
	QuarantineDir string
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

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         getEnv("SERVER_HOST", "0.0.0.0"),
			Port:         getEnv("SERVER_PORT", "8080"),
			ReadTimeout:  getDurationEnv("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout: getDurationEnv("SERVER_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:  getDurationEnv("SERVER_IDLE_TIMEOUT", 120*time.Second),
		},
		Database: DatabaseConfig{
			URL:             getEnv("DATABASE_URL", ""),
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

		KYC: KYCConfig{
			MaxFileSize:        getInt64Env("KYC_MAX_FILE_SIZE", 10*1024*1024), // 10MB
			AllowedFileTypes:   strings.Split(getEnv("KYC_ALLOWED_FILE_TYPES", "image/jpeg,image/png,application/pdf"), ","),
			DocumentRetention:  getDurationEnv("KYC_DOCUMENT_RETENTION", 365*24*time.Hour),
			AutoApproveEnabled: getBoolEnv("KYC_AUTO_APPROVE_ENABLED", false),
			AutoApproveLimit:   getDecimalEnv("KYC_AUTO_APPROVE_LIMIT", "1000"),
		},
		FileUpload: FileUploadConfig{
			StorageType:     getEnv("FILE_STORAGE_TYPE", "local"),
			LocalUploadDir:  getEnv("LOCAL_UPLOAD_DIR", "./uploads"),
			MaxUploadSize:   getInt64Env("MAX_UPLOAD_SIZE", 25*1024*1024), // 25MB
			PresignedURLExp: getDurationEnv("PRESIGNED_URL_EXPIRY", 15*time.Minute),
			CDNBaseURL:      getEnv("CDN_BASE_URL", "http://localhost:8080/uploads"),
		},
		AML: AMLConfig{
			Enabled:        getBoolEnv("AML_ENABLED", false),
			Provider:       getEnv("AML_PROVIDER", "mock"),
			CheckThreshold: getDecimalEnv("AML_CHECK_THRESHOLD", "5000"),
			AutoBlock:      getBoolEnv("AML_AUTO_BLOCK", false),
			APIKey:         getEnv("AML_API_KEY", ""),
			Endpoint:       getEnv("AML_ENDPOINT", ""),
		},
		VirusScan: VirusScanConfig{
			Enabled:       getBoolEnv("VIRUS_SCAN_ENABLED", true),
			Engine:        getEnv("VIRUS_SCAN_ENGINE", "mock"),
			ClamAVHost:    getEnv("CLAMAV_HOST", "localhost"),
			ClamAVPort:    getIntEnv("CLAMAV_PORT", 3310),
			ScanTimeout:   getDurationEnv("VIRUS_SCAN_TIMEOUT", 30*time.Second),
			QuarantineDir: getEnv("VIRUS_QUARANTINE_DIR", "./quarantine"),
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

func getInt64Env(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getDecimalEnv(key string, defaultValue string) decimal.Decimal {
	if value := os.Getenv(key); value != "" {
		if dec, err := decimal.NewFromString(value); err == nil {
			return dec
		}
	}
	return decimal.RequireFromString(defaultValue)
}
