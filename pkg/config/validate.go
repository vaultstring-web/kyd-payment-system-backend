// Package config loads and validates service configuration.
package config

import (
	"fmt"
	"strings"
)

// ValidateCore ensures critical configuration is present.
func (c *Config) ValidateCore() error {
	var missing []string

	if strings.TrimSpace(c.Database.URL) == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if strings.TrimSpace(c.Redis.URL) == "" {
		missing = append(missing, "REDIS_URL")
	}
	if strings.TrimSpace(c.Server.Port) == "" {
		missing = append(missing, "SERVER_PORT")
	}
	if strings.TrimSpace(c.JWT.Secret) == "" || c.JWT.Secret == "change-this-secret" {
		missing = append(missing, "JWT_SECRET")
	}
	if c.Security.RequireSigning && strings.TrimSpace(c.Security.SigningSecret) == "" {
		missing = append(missing, "SIGNING_SECRET")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}

	return nil
}
