package cache

import (
	"testing"
	"time"

	"github.com/eko/gocache/lib/v4/cache"
	gocacheStore "github.com/eko/gocache/store/go_cache/v4"
	gocache "github.com/patrickmn/go-cache"
)

func TestManagerValueLifecycle(t *testing.T) {
	client := gocache.New(time.Minute, time.Minute)
	manager := &Manager[string]{
		cache: cache.New[string](gocacheStore.NewGoCache(client)),
	}

	value, err := manager.GetValue("missing")
	if err != nil || value != "" {
		t.Fatalf("missing key: value = %q, err = %v; want empty value and no error", value, err)
	}

	if err := manager.SetValue("key", "value"); err != nil {
		t.Fatalf("set key: %v", err)
	}
	value, err = manager.GetValue("key")
	if err != nil || value != "value" {
		t.Fatalf("get key: value = %q, err = %v; want value", value, err)
	}

	if err := manager.Delete("key"); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	value, err = manager.GetValue("key")
	if err != nil || value != "" {
		t.Fatalf("deleted key: value = %q, err = %v; want empty value and no error", value, err)
	}
}
