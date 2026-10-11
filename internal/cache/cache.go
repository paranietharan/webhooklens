package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"webhooklens/internal/models"
)

var ErrCacheMiss = errors.New("cache miss")

type Cache interface {
	GetEndpointConfig(ctx context.Context, endpointID string) (*models.EndpointConfig, error)
	SetEndpointConfig(ctx context.Context, endpointID string, cfg *models.EndpointConfig, ttl time.Duration) error
	InvalidateEndpointConfig(ctx context.Context, endpointID string) error
	PublishWebhookEvent(ctx context.Context, endpointID string, event *models.WebhookEvent) error
	Subscribe(ctx context.Context, endpointID string) *redis.PubSub
	Ping(ctx context.Context) error
	Close() error
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(ctx context.Context, redisURL string) (*RedisCache, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}

	client := redis.NewClient(opt)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &RedisCache{client: client}, nil
}

func endpointKey(endpointID string) string {
	return fmt.Sprintf("endpoint:config:%s", endpointID)
}

func pubSubChannel(endpointID string) string {
	return fmt.Sprintf("endpoint:%s", endpointID)
}

func (r *RedisCache) GetEndpointConfig(ctx context.Context, endpointID string) (*models.EndpointConfig, error) {
	data, err := r.client.Get(ctx, endpointKey(endpointID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrCacheMiss
		}
		return nil, err
	}

	var cfg models.EndpointConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *RedisCache) SetEndpointConfig(ctx context.Context, endpointID string, cfg *models.EndpointConfig, ttl time.Duration) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, endpointKey(endpointID), data, ttl).Err()
}

func (r *RedisCache) InvalidateEndpointConfig(ctx context.Context, endpointID string) error {
	return r.client.Del(ctx, endpointKey(endpointID)).Err()
}

func (r *RedisCache) PublishWebhookEvent(ctx context.Context, endpointID string, event *models.WebhookEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, pubSubChannel(endpointID), data).Err()
}

func (r *RedisCache) Subscribe(ctx context.Context, endpointID string) *redis.PubSub {
	return r.client.Subscribe(ctx, pubSubChannel(endpointID))
}

func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}
