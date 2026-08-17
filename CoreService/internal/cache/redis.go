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

// Del removes keys.
func Del(ctx context.Context, keys ...string) error {
	return RDB.Del(ctx, keys...).Err()
}

// DelByPrefix removes all keys matching a prefix.
func DelByPrefix(ctx context.Context, prefix string) error {
	iter := RDB.Scan(ctx, 0, prefix+"*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return RDB.Del(ctx, keys...).Err()
	}
	return nil
}
