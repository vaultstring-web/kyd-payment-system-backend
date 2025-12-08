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
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	JWT      JWTConfig
	Stellar  StellarConfig
	Ripple   RippleConfig
}

type ServerConfig struct {
	Host         string
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type DatabaseConfig struct {
	URL             string
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
