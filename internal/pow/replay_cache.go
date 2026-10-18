package pow

import (
	"context"
	"sync"
	"time"
)

type ReplayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	now  func() time.Time
}

func NewReplayCache(now func() time.Time) *ReplayCache {
	return &ReplayCache{
		seen: make(map[string]time.Time),
		now:  now,
	}
}

func (c *ReplayCache) TryAdd(id string, expires time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.seen[id]; ok {
		return false
	}

	c.seen[id] = expires
	return true
}

func (c *ReplayCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	for id, exp := range c.seen {
		if now.After(exp) {
			delete(c.seen, id)
		}
	}
}

func (c *ReplayCache) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.Cleanup()
		case <-ctx.Done():
			return
		}
	}
}
