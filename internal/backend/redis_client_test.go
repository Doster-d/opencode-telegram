package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRealRedisClient_NewAndDelegates(t *testing.T) {
	if _, err := NewRealRedisClient("://bad"); err == nil {
		t.Fatal("expected parse url error")
	}

	rc, err := NewRealRedisClient("redis://127.0.0.1:6379/0")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = rc.client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_ = rc.LPush(ctx, "k", "v")
	_, _ = rc.BRPopLPush(ctx, "src", "dst", 5*time.Millisecond)
	_, _ = rc.LRange(ctx, "k", 0, -1)
	_ = rc.LRem(ctx, "k", 0, "v")
	_ = rc.Set(ctx, "k", "v", time.Second)
	_, _ = rc.Get(ctx, "k")
	_ = rc.Del(ctx, "k")
	_ = rc.HSet(ctx, "h", "f", "v")
	_, _ = rc.HGet(ctx, "h", "f")
	_ = rc.HDel(ctx, "h", "f")
	_ = rc.Expire(ctx, "k", time.Second)
}

func TestRealRedisClient_DelegatesToUnderlyingClient(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	rc := &RealRedisClient{client: redisClient}
	t.Cleanup(func() { _ = redisClient.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := rc.LPush(ctx, "k", "v"); err == nil {
		t.Fatal("expected lpush to fail without redis")
	}
	if _, err := rc.BRPopLPush(ctx, "a", "b", 5*time.Millisecond); err == nil {
		t.Fatal("expected brpoplpush to fail without redis")
	}
	if _, err := rc.LRange(ctx, "k", 0, -1); err == nil {
		t.Fatal("expected lrange to fail without redis")
	}
	if err := rc.LRem(ctx, "k", 0, "v"); err == nil {
		t.Fatal("expected lrem to fail without redis")
	}
	if err := rc.Set(ctx, "k", "v", time.Second); err == nil {
		t.Fatal("expected set to fail without redis")
	}
	if _, err := rc.Get(ctx, "k"); err == nil {
		t.Fatal("expected get to fail without redis")
	}
	if err := rc.Del(ctx, "k"); err == nil {
		t.Fatal("expected del to fail without redis")
	}
	if err := rc.HSet(ctx, "h", "f", "v"); err == nil {
		t.Fatal("expected hset to fail without redis")
	}
	if _, err := rc.HGet(ctx, "h", "f"); err == nil {
		t.Fatal("expected hget to fail without redis")
	}
	if err := rc.HDel(ctx, "h", "f"); err == nil {
		t.Fatal("expected hdel to fail without redis")
	}
	if err := rc.Expire(ctx, "k", time.Second); err == nil {
		t.Fatal("expected expire to fail without redis")
	}

	if err := rc.Del(ctx, "k"); err != nil && !strings.Contains(err.Error(), "dial tcp") && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected dial tcp style error, got %v", err)
	}
}
