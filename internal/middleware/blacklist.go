package middleware

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisTokenBlacklist implements TokenBlacklist using Redis.
type RedisTokenBlacklist struct {
	client *redis.Client
}

// NewRedisTokenBlacklist creates a new RedisTokenBlacklist.
func NewRedisTokenBlacklist(client *redis.Client) *RedisTokenBlacklist {
	return &RedisTokenBlacklist{client: client}
}

// Blacklist adds a token to the blacklist with an expiration.
func (b *RedisTokenBlacklist) Blacklist(ctx context.Context, token string, expiration time.Duration) error {
	return b.client.Set(ctx, "blacklist:"+token, "revoked", expiration).Err()
}

// IsBlacklisted checks if a token is in the blacklist.
func (b *RedisTokenBlacklist) IsBlacklisted(ctx context.Context, token string) (bool, error) {
	exists, err := b.client.Exists(ctx, "blacklist:"+token).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}
