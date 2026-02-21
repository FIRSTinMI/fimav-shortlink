package main

import (
	"sync"
	"time"
)

type cacheEntry[V any] struct {
	data        V
	dataExpires time.Time
}

type Cache[K comparable, V any] struct {
	data           map[K]cacheEntry[V]
	ttl            time.Duration
	populationFunc func(requestedKey K) map[K]V
	rwMutex        sync.RWMutex
}

func NewCache[K comparable, V any](duration time.Duration, populationFunc func(requestedKey K) map[K]V) *Cache[K, V] {
	return &Cache[K, V]{
		data:           make(map[K]cacheEntry[V]),
		ttl:            duration,
		populationFunc: populationFunc,
	}
}

func (c *Cache[K, V]) Get(key K) *V {
	if value, found := c.data[key]; found && time.Now().Before(value.dataExpires) {
		return &value.data
	}

	// not in there or expired, try fetching it
	newData := c.populationFunc(key)
	c.rwMutex.Lock()
	for k, v := range newData {
		c.data[k] = cacheEntry[V]{
			data:        v,
			dataExpires: time.Now().Add(c.ttl),
		}
	}
	c.rwMutex.Unlock()

	if value, found := c.data[key]; found {
		return &value.data
	} else {
		var zero V
		c.data[key] = cacheEntry[V]{
			data:        zero,
			dataExpires: time.Now().Add(c.ttl),
		}
	}

	return nil
}

func (c *Cache[K, V]) Invalidate() {
	for _, v := range c.data {
		v.dataExpires = time.Now()
	}
}
