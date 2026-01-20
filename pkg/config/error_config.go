// ==============================================================================
// ERROR HANDLER CONFIGURATION - pkg/config/error_config.go
// ==============================================================================
// Configuration for error handling service
// ==============================================================================

package config

import (
	"strings"
	"time"
)

// ErrorHandlerConfig extends the main config with error handling settings
type ErrorHandlerConfig struct {
	// Logging settings
	EnableDetailedLogging bool   `json:"enable_detailed_logging" env:"ERROR_DETAILED_LOGGING" default:"true"`
	LogStackTrace         bool   `json:"log_stack_trace" env:"ERROR_LOG_STACK_TRACE" default:"true"`
	ErrorLogLevel         string `json:"error_log_level" env:"ERROR_LOG_LEVEL" default:"error"`

	// Retry settings
	MaxRetryAttempts  int           `json:"max_retry_attempts" env:"ERROR_MAX_RETRY_ATTEMPTS" default:"3"`
	DefaultRetryDelay time.Duration `json:"default_retry_delay" env:"ERROR_DEFAULT_RETRY_DELAY" default:"1s"`
	MaxRetryDelay     time.Duration `json:"max_retry_delay" env:"ERROR_MAX_RETRY_DELAY" default:"30s"`

	// Circuit breaker settings
	CircuitBreakerEnabled   bool          `json:"circuit_breaker_enabled" env:"CIRCUIT_BREAKER_ENABLED" default:"true"`
	CircuitBreakerTimeout   time.Duration `json:"circuit_breaker_timeout" env:"CIRCUIT_BREAKER_TIMEOUT" default:"60s"`
	CircuitBreakerThreshold int           `json:"circuit_breaker_threshold" env:"CIRCUIT_BREAKER_THRESHOLD" default:"5"`

	// Error reporting
	ErrorReportingEnabled bool   `json:"error_reporting_enabled" env:"ERROR_REPORTING_ENABLED" default:"false"`
	ErrorReportingURL     string `json:"error_reporting_url" env:"ERROR_REPORTING_URL" default:""`
	ErrorReportingAPIKey  string `json:"error_reporting_api_key" env:"ERROR_REPORTING_API_KEY" default:""`

	// Fallback and degradation
	EnableFallbackMode  bool          `json:"enable_fallback_mode" env:"ENABLE_FALLBACK_MODE" default:"true"`
	FallbackCacheTTL    time.Duration `json:"fallback_cache_ttl" env:"FALLBACK_CACHE_TTL" default:"5m"`
	DegradedModeTimeout time.Duration `json:"degraded_mode_timeout" env:"DEGRADED_MODE_TIMEOUT" default:"10m"`

	// Alerting and notifications
	EnableErrorAlerts bool          `json:"enable_error_alerts" env:"ENABLE_ERROR_ALERTS" default:"true"`
	AlertThreshold    int           `json:"alert_threshold" env:"ALERT_THRESHOLD" default:"10"`
	AlertCooldown     time.Duration `json:"alert_cooldown" env:"ALERT_COOLDOWN" default:"5m"`
	AlertRecipients   []string      `json:"alert_recipients" env:"ALERT_RECIPIENTS" default:"admin@example.com"`

	// Performance monitoring
	EnableErrorMetrics   bool          `json:"enable_error_metrics" env:"ENABLE_ERROR_METRICS" default:"true"`
	MetricsFlushInterval time.Duration `json:"metrics_flush_interval" env:"METRICS_FLUSH_INTERVAL" default:"30s"`

	// Recovery settings
	AutoRecoveryEnabled bool          `json:"auto_recovery_enabled" env:"AUTO_RECOVERY_ENABLED" default:"true"`
	MaxRecoveryAttempts int           `json:"max_recovery_attempts" env:"MAX_RECOVERY_ATTEMPTS" default:"3"`
	RecoveryDelay       time.Duration `json:"recovery_delay" env:"RECOVERY_DELAY" default:"10s"`
}

// LoadErrorHandlerConfig loads error handler configuration from environment
func LoadErrorHandlerConfig() *ErrorHandlerConfig {
	return &ErrorHandlerConfig{
		EnableDetailedLogging: getBoolEnv("ERROR_DETAILED_LOGGING", true),
		LogStackTrace:         getBoolEnv("ERROR_LOG_STACK_TRACE", true),
		ErrorLogLevel:         getEnv("ERROR_LOG_LEVEL", "error"),

		MaxRetryAttempts:  getIntEnv("ERROR_MAX_RETRY_ATTEMPTS", 3),
		DefaultRetryDelay: getDurationEnv("ERROR_DEFAULT_RETRY_DELAY", 1*time.Second),
		MaxRetryDelay:     getDurationEnv("ERROR_MAX_RETRY_DELAY", 30*time.Second),

		CircuitBreakerEnabled:   getBoolEnv("CIRCUIT_BREAKER_ENABLED", true),
		CircuitBreakerTimeout:   getDurationEnv("CIRCUIT_BREAKER_TIMEOUT", 60*time.Second),
		CircuitBreakerThreshold: getIntEnv("CIRCUIT_BREAKER_THRESHOLD", 5),

		ErrorReportingEnabled: getBoolEnv("ERROR_REPORTING_ENABLED", false),
		ErrorReportingURL:     getEnv("ERROR_REPORTING_URL", ""),
		ErrorReportingAPIKey:  getEnv("ERROR_REPORTING_API_KEY", ""),

		EnableFallbackMode:  getBoolEnv("ENABLE_FALLBACK_MODE", true),
		FallbackCacheTTL:    getDurationEnv("FALLBACK_CACHE_TTL", 5*time.Minute),
		DegradedModeTimeout: getDurationEnv("DEGRADED_MODE_TIMEOUT", 10*time.Minute),

		EnableErrorAlerts: getBoolEnv("ENABLE_ERROR_ALERTS", true),
		AlertThreshold:    getIntEnv("ALERT_THRESHOLD", 10),
		AlertCooldown:     getDurationEnv("ALERT_COOLDOWN", 5*time.Minute),
		AlertRecipients:   strings.Split(getEnv("ALERT_RECIPIENTS", "admin@example.com"), ","),

		EnableErrorMetrics:   getBoolEnv("ENABLE_ERROR_METRICS", true),
		MetricsFlushInterval: getDurationEnv("METRICS_FLUSH_INTERVAL", 30*time.Second),

		AutoRecoveryEnabled: getBoolEnv("AUTO_RECOVERY_ENABLED", true),
		MaxRecoveryAttempts: getIntEnv("MAX_RECOVERY_ATTEMPTS", 3),
		RecoveryDelay:       getDurationEnv("RECOVERY_DELAY", 10*time.Second),
	}
}
