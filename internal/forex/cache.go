
package forex

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"kyd/internal/domain"
)

type RedisRateCache struct {
	client *redis.Client
}

func NewRedisRateCache(client *redis.Client) RateCache {
	return &RedisRateCache{client: client}
}

func (c *RedisRateCache) Get(key string) (*domain.ExchangeRate, error) {
	ctx := context.Background()
	data, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var rate domain.ExchangeRate
	if err := json.Unmarshal([]byte(data), &rate); err != nil {
		return nil, err
	}

	return &rate, nil
}

func (c *RedisRateCache) Set(key string, rate *domain.ExchangeRate, ttl time.Duration) error {
	ctx := context.Background()
	data, err := json.Marshal(rate)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, data, ttl).Err()
}
