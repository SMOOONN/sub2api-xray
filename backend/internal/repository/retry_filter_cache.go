package repository

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	retryFilterCacheKey  = "retry_filter_rules"
	retryFilterPubSubKey = "retry_filter_rules_updated"
	retryFilterCacheTTL  = 24 * time.Hour
)

type retryFilterCache struct {
	rdb        *redis.Client
	localCache map[int64][]*model.RetryFilterRule
	localMu    sync.RWMutex
}

func NewRetryFilterCache(rdb *redis.Client) service.RetryFilterCache {
	return &retryFilterCache{rdb: rdb}
}

func (c *retryFilterCache) Get(ctx context.Context, presetID int64) ([]*model.RetryFilterRule, bool) {
	c.localMu.RLock()
	if rules, ok := c.localCache[presetID]; ok {
		c.localMu.RUnlock()
		return rules, true
	}
	c.localMu.RUnlock()
	data, err := c.rdb.Get(ctx, retryFilterCacheKey).Bytes()
	if err != nil {
		if err != redis.Nil {
			log.Printf("[RetryFilterCache] Redis get failed: %v", err)
		}
		return nil, false
	}
	var all map[int64][]*model.RetryFilterRule
	if err := json.Unmarshal(data, &all); err != nil {
		log.Printf("[RetryFilterCache] unmarshal failed: %v", err)
		return nil, false
	}
	c.localMu.Lock()
	c.localCache = all
	rules := all[presetID]
	c.localMu.Unlock()
	return rules, rules != nil
}

func (c *retryFilterCache) Set(ctx context.Context, rules map[int64][]*model.RetryFilterRule) error {
	// Preserve other presets when a caller refreshes only one preset.
	if existing, err := c.rdb.Get(ctx, retryFilterCacheKey).Bytes(); err == nil {
		var prior map[int64][]*model.RetryFilterRule
		if json.Unmarshal(existing, &prior) == nil {
			for id, rs := range rules {
				prior[id] = rs
			}
			rules = prior
		}
	}
	data, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if err := c.rdb.Set(ctx, retryFilterCacheKey, data, retryFilterCacheTTL).Err(); err != nil {
		return err
	}
	c.localMu.Lock()
	c.localCache = rules
	c.localMu.Unlock()
	return nil
}
func (c *retryFilterCache) Invalidate(ctx context.Context) error {
	c.localMu.Lock()
	c.localCache = nil
	c.localMu.Unlock()
	return c.rdb.Del(ctx, retryFilterCacheKey).Err()
}
func (c *retryFilterCache) NotifyUpdate(ctx context.Context) error {
	return c.rdb.Publish(ctx, retryFilterPubSubKey, "refresh").Err()
}
func (c *retryFilterCache) SubscribeUpdates(ctx context.Context, handler func()) {
	go func() {
		sub := c.rdb.Subscribe(ctx, retryFilterPubSubKey)
		defer func() { _ = sub.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-sub.Channel():
				if msg == nil {
					return
				}
				c.localMu.Lock()
				c.localCache = nil
				c.localMu.Unlock()
				if handler != nil {
					handler()
				}
			}
		}
	}()
}
