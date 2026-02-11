package backend

import (
	"context"
	"errors"
	"testing"
	"time"

	"opencode-telegram/internal/proxy/contracts"
)

func TestInMemoryRedisClient_Branches(t *testing.T) {
	clk := time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)
	rc := NewInMemoryRedisClient()
	rc.SetClock(func() time.Time { return clk })
	ctx := context.Background()

	if err := rc.LPush(ctx, "list", "a", "b", "c"); err != nil {
		t.Fatalf("lpush: %v", err)
	}
	if got, _ := rc.LRange(ctx, "list", -10, -1); len(got) != 3 {
		t.Fatalf("expected full list from negative range, got %+v", got)
	}
	if err := rc.LRem(ctx, "list", -1, "b"); err != nil {
		t.Fatalf("lrem tail: %v", err)
	}
	if got, _ := rc.LRange(ctx, "list", 0, -1); len(got) != 2 {
		t.Fatalf("expected one value removed from tail mode, got %+v", got)
	}

	if err := rc.Set(ctx, "k", "v", 10*time.Millisecond); err != nil {
		t.Fatalf("set ttl: %v", err)
	}
	clk = clk.Add(20 * time.Millisecond)
	if _, err := rc.Get(ctx, "k"); err == nil {
		t.Fatal("expected expired key to return redis nil")
	}

	if err := rc.HSet(ctx, "h", "f"); err == nil {
		t.Fatal("expected odd hset args error")
	}
	if err := rc.HSet(ctx, "h", "f", "v"); err != nil {
		t.Fatalf("hset: %v", err)
	}
	if err := rc.Expire(ctx, "h", -1); err != nil {
		t.Fatalf("expire clear: %v", err)
	}
	if err := rc.HDel(ctx, "h", "f"); err != nil {
		t.Fatalf("hdel: %v", err)
	}
	if err := rc.Del(ctx, "list", "k", "h"); err != nil {
		t.Fatalf("del: %v", err)
	}
}

func TestRedisQueue_ErrorPaths(t *testing.T) {
	q := NewRedisQueue(&stubRedisClient{})
	if _, err := q.Poll(context.Background(), "", 1); err == nil {
		t.Fatal("expected empty agent id error")
	}
	if err := q.Enqueue(context.Background(), "", contracts.Command{}); err == nil {
		t.Fatal("expected enqueue empty agent id error")
	}
	if err := q.StoreResult(context.Background(), "", contracts.CommandResult{CommandID: "c"}); err == nil {
		t.Fatal("expected empty agent id error")
	}
	if err := q.StoreResult(context.Background(), "a1", contracts.CommandResult{}); err == nil {
		t.Fatal("expected missing command id error")
	}
}

func TestRedisQueue_MarshalAndGetBranches(t *testing.T) {
	s := &stubRedisClient{
		lrangeFn: func(ctx context.Context, key string, start, stop int64) ([]string, error) {
			return nil, errors.New("boom")
		},
	}
	q := NewRedisQueue(s)
	if _, err := q.Poll(context.Background(), "a1", 1); err == nil {
		t.Fatal("expected poll stale lookup error")
	}

	s = &stubRedisClient{
		lrangeFn: func(ctx context.Context, key string, start, stop int64) ([]string, error) { return []string{}, nil },
		brpoplpushFn: func(ctx context.Context, source, destination string, timeout time.Duration) (string, error) {
			return "{bad", nil
		},
	}
	q = NewRedisQueue(s)
	if _, err := q.Poll(context.Background(), "a1", 1); err == nil {
		t.Fatal("expected poll unmarshal error")
	}

	s = &stubRedisClient{
		getFn: func(ctx context.Context, key string) (string, error) { return "{bad", nil },
	}
	q = NewRedisQueue(s)
	if _, err := q.GetResult(context.Background(), "a1", "c1"); err == nil {
		t.Fatal("expected get result unmarshal error")
	}

	s = &stubRedisClient{
		lrangeFn: func(ctx context.Context, key string, start, stop int64) ([]string, error) { return []string{}, nil },
		setFn: func(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
			return errors.New("set failed")
		},
	}
	q = NewRedisQueue(s)
	err := q.StoreResult(context.Background(), "a1", contracts.CommandResult{CommandID: "c1", OK: true})
	if err == nil {
		t.Fatal("expected set failure to bubble")
	}
}

type stubRedisClient struct {
	lpushFn      func(ctx context.Context, key string, values ...interface{}) error
	brpoplpushFn func(ctx context.Context, source, destination string, timeout time.Duration) (string, error)
	lrangeFn     func(ctx context.Context, key string, start, stop int64) ([]string, error)
	lremFn       func(ctx context.Context, key string, count int64, value interface{}) error
	setFn        func(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	getFn        func(ctx context.Context, key string) (string, error)
	delFn        func(ctx context.Context, keys ...string) error
	hsetFn       func(ctx context.Context, key string, values ...interface{}) error
	hgetFn       func(ctx context.Context, key, field string) (string, error)
	hdelFn       func(ctx context.Context, key string, fields ...string) error
	expireFn     func(ctx context.Context, key string, expiration time.Duration) error
}

func (s *stubRedisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	if s.lpushFn != nil {
		return s.lpushFn(ctx, key, values...)
	}
	return nil
}

func (s *stubRedisClient) BRPopLPush(ctx context.Context, source, destination string, timeout time.Duration) (string, error) {
	if s.brpoplpushFn != nil {
		return s.brpoplpushFn(ctx, source, destination, timeout)
	}
	return "", errors.New("redis: nil")
}

func (s *stubRedisClient) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	if s.lrangeFn != nil {
		return s.lrangeFn(ctx, key, start, stop)
	}
	return []string{}, nil
}

func (s *stubRedisClient) LRem(ctx context.Context, key string, count int64, value interface{}) error {
	if s.lremFn != nil {
		return s.lremFn(ctx, key, count, value)
	}
	return nil
}

func (s *stubRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if s.setFn != nil {
		return s.setFn(ctx, key, value, expiration)
	}
	return nil
}

func (s *stubRedisClient) Get(ctx context.Context, key string) (string, error) {
	if s.getFn != nil {
		return s.getFn(ctx, key)
	}
	return "", errors.New("redis: nil")
}

func (s *stubRedisClient) Del(ctx context.Context, keys ...string) error {
	if s.delFn != nil {
		return s.delFn(ctx, keys...)
	}
	return nil
}

func (s *stubRedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	if s.hsetFn != nil {
		return s.hsetFn(ctx, key, values...)
	}
	return nil
}

func (s *stubRedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	if s.hgetFn != nil {
		return s.hgetFn(ctx, key, field)
	}
	return "", errors.New("redis: nil")
}

func (s *stubRedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	if s.hdelFn != nil {
		return s.hdelFn(ctx, key, fields...)
	}
	return nil
}

func (s *stubRedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	if s.expireFn != nil {
		return s.expireFn(ctx, key, expiration)
	}
	return nil
}
