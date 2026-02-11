package backend

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RealRedisClient struct {
	client *redis.Client
}

func NewRealRedisClient(url string) (*RealRedisClient, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &RealRedisClient{client: redis.NewClient(opt)}, nil
}

func (c *RealRedisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	return c.client.LPush(ctx, key, values...).Err()
}

func (c *RealRedisClient) BRPopLPush(ctx context.Context, source, destination string, timeout time.Duration) (string, error) {
	return c.client.BRPopLPush(ctx, source, destination, timeout).Result()
}

func (c *RealRedisClient) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return c.client.LRange(ctx, key, start, stop).Result()
}

func (c *RealRedisClient) LRem(ctx context.Context, key string, count int64, value interface{}) error {
	return c.client.LRem(ctx, key, count, value).Err()
}

func (c *RealRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return c.client.Set(ctx, key, value, expiration).Err()
}

func (c *RealRedisClient) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *RealRedisClient) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

func (c *RealRedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	return c.client.HSet(ctx, key, values...).Err()
}

func (c *RealRedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	return c.client.HGet(ctx, key, field).Result()
}

func (c *RealRedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	return c.client.HDel(ctx, key, fields...).Err()
}

func (c *RealRedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return c.client.Expire(ctx, key, expiration).Err()
}
