package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/satyamraj1643/pine_backend_v2/internal/cache"
	"github.com/satyamraj1643/pine_backend_v2/internal/helpers"
)

// Atomic across replicas. Expiration is assigned with the first increment, so
// an exhausted limit cannot be extended indefinitely by repeated requests.
var authLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('EXPIRE', KEYS[1], 600) end
return count
`)

func allowAuthAttempt(w http.ResponseWriter, r *http.Request, purpose, identity string, limit int64) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	// Do not put raw email addresses or codes in Redis keys.
	key := fmt.Sprintf("auth-limit:%s:%x", purpose, sha256.Sum256([]byte(identity)))
	if cache.RDB == nil {
		helpers.Error(w, http.StatusServiceUnavailable, "Please try again shortly")
		return false
	}
	count, err := authLimitScript.Run(ctx, cache.RDB, []string{key}).Int64()
	if err != nil {
		helpers.Error(w, http.StatusServiceUnavailable, "Please try again shortly")
		return false
	}
	if count > limit {
		w.Header().Set("Retry-After", "600")
		helpers.Error(w, http.StatusTooManyRequests, "Too many attempts. Please try again in 10 minutes.")
		return false
	}
	return true
}
