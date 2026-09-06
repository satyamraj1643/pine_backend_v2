package cache

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
	"time"
)

func TestRevisionNamespace(t *testing.T) {
	if revisionKey("entries:user:edited-v1") != revisionKey("entries:user") {
		t.Fatal("versions must share invalidation")
	}
	if revisionKey("entries:user") == revisionKey("entries:other") {
		t.Fatal("users must be isolated")
	}
}

// Opt-in only: point CACHE_TEST_REDIS_URL at a disposable Redis.
func TestRedisStaleFillAndVersionInvalidation(t *testing.T) {
	url := os.Getenv("CACHE_TEST_REDIS_URL")
	if url == "" {
		t.Skip("disposable Redis not configured")
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	previous := RDB
	RDB = redis.NewClient(options)
	defer func() { RDB.Close(); RDB = previous }()
	ctx := context.Background()
	base := fmt.Sprintf("entries:cache-test-%d", time.Now().UnixNano())
	key := base + ":edited-v1"
	defer RDB.Del(ctx, base, key, revisionKey(key))
	_, revision, err := Read(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetIfRevision(ctx, key, revision, "old", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := RDB.Set(ctx, base, "legacy", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := Del(ctx, base); err != nil {
		t.Fatal(err)
	}
	if err := SetIfRevision(ctx, key, revision, "stale", time.Minute); err != nil {
		t.Fatal(err)
	}
	value, next, err := Read(ctx, key)
	if err != nil || value != "" || next == revision {
		t.Fatalf("stale fill accepted: %q %q %v", value, next, err)
	}
	if RDB.Exists(ctx, base).Val() != 0 {
		t.Fatal("legacy cache survived")
	}
	if err := SetIfRevision(ctx, key, next, "fresh", time.Minute); err != nil {
		t.Fatal(err)
	}
	value, _, err = Read(ctx, key)
	if err != nil || value != "fresh" {
		t.Fatalf("fresh fill failed: %q %v", value, err)
	}
}
