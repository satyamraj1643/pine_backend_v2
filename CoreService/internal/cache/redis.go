package cache

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

func Connect() {
	addr := os.Getenv("REDIS_URL")
	if addr == "" {
		addr = "localhost:6379"
	}

	var opts *redis.Options
	if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
		parsedOpts, err := redis.ParseURL(addr)
		if err != nil {
			log.Fatalf("Unable to parse REDIS_URL: %v", err)
		}
		opts = parsedOpts
	} else {
		opts = &redis.Options{
			Addr: addr,
			DB:   0,
		}
	}

	RDB = redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RDB.Ping(ctx).Err(); err != nil {
		log.Fatalf("Unable to connect to Redis: %v", err)
	}
	log.Println("Redis connected")
}

func Close() {
	if RDB != nil {
		RDB.Close()
	}
}

// Set stores a value with TTL.
func Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return RDB.Set(ctx, key, value, ttl).Err()
}

// Get retrieves a value.
func Get(ctx context.Context, key string) (string, error) {
	return RDB.Get(ctx, key).Result()
}

// Cache revisions prevent an in-flight pre-mutation read from resurrecting stale data.
func revisionKey(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) >= 2 {
		return "cache-revision:" + parts[0] + ":" + parts[1]
	}
	return "cache-revision:" + key
}

var readScript = redis.NewScript(`
return {redis.call('GET', KEYS[1]) or '0', redis.call('GET', KEYS[2]) or ''}
`)

func Read(ctx context.Context, key string) (string, string, error) {
	values, err := readScript.Run(ctx, RDB, []string{revisionKey(key), key}).Slice()
	if err != nil {
		return "", "", err
	}
	return values[1].(string), values[0].(string), nil
}

var fillScript = redis.NewScript(`
if (redis.call('GET', KEYS[1]) or '0') ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
return 1
`)

func SetIfRevision(ctx context.Context, key, revision string, value interface{}, ttl time.Duration) error {
	// If reading the revision failed, never publish an unverifiable snapshot.
	if revision == "" {
		return nil
	}
	return fillScript.Run(ctx, RDB, []string{revisionKey(key), key}, revision, value, ttl.Milliseconds()).Err()
}

var invalidateScript = redis.NewScript(`
redis.call('INCR', KEYS[1])
return redis.call('DEL', KEYS[2], KEYS[3])
`)

// All existing list formats: legacy clients and EditedAt-aware clients.
func DelByPrefix(ctx context.Context, prefix string) error {
	err := invalidateScript.Run(ctx, RDB, []string{revisionKey(prefix), prefix, prefix + ":edited-v1"}).Err()
	if err != nil {
		log.Printf("Cache invalidation failed: %v", err)
	}
	return err
}

func Del(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		if err := DelByPrefix(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
