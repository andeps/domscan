// Package rediscache implements the availability cache with Redis.
package rediscache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"domscan/availability"

	"github.com/redis/go-redis/v9"
)

// Store persists availability results as expiring Redis string values.
type Store struct {
	client *redis.Client
	prefix string
}

// New creates a Redis store from a redis:// or rediss:// URL.
func New(rawURL, prefix string, dialTimeout time.Duration) (*Store, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("解析 Redis URL：%w", err)
	}
	options.DialTimeout = dialTimeout
	options.ReadTimeout = dialTimeout
	options.WriteTimeout = dialTimeout
	options.MaxRetries = 1
	return &Store{client: redis.NewClient(options), prefix: prefix}, nil
}

// Ping verifies that Redis is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// Close releases Redis connections.
func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) Get(ctx context.Context, domain string) (availability.Result, bool, error) {
	payload, err := s.client.Get(ctx, s.key(domain)).Bytes()
	if err == redis.Nil {
		return availability.Result{}, false, nil
	}
	if err != nil {
		return availability.Result{}, false, err
	}
	var result availability.Result
	if err := json.Unmarshal(payload, &result); err != nil {
		return availability.Result{}, false, fmt.Errorf("解析缓存结果：%w", err)
	}
	if result.Status != availability.StatusAvailable && result.Status != availability.StatusRegistered && result.Status != availability.StatusUnknown {
		return availability.Result{}, false, fmt.Errorf("缓存状态 %q 无效", result.Status)
	}
	result.Domain = domain
	result.Cached = false
	return result, true, nil
}

func (s *Store) Set(ctx context.Context, domain string, result availability.Result, ttl time.Duration) error {
	result.Cached = false
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(domain), payload, ttl).Err()
}

func (s *Store) key(domain string) string {
	return s.prefix + strings.ToLower(domain)
}
