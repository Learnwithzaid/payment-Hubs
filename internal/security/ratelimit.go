package security

import (
    "context"
    "net/http"
    "strconv"
    "time"

    "github.com/redis/go-redis/v9"
)

type RedisTokenBucket struct {
    Redis      *redis.Client
    Prefix     string
    Capacity   int
    RefillRate float64 // tokens per second
}

var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

local data = redis.call('HMGET', key, 'tokens', 'last')
local tokens = tonumber(data[1])
local last = tonumber(data[2])

if tokens == nil then tokens = capacity end
if last == nil then last = now end

local delta = now - last
if delta < 0 then delta = 0 end

local filled = tokens + (delta * refill_rate)
if filled > capacity then filled = capacity end

local allowed = 0
if filled >= 1 then
  allowed = 1
  filled = filled - 1
end

redis.call('HSET', key, 'tokens', filled, 'last', now)
redis.call('EXPIRE', key, ttl)

return {allowed, filled}
`)

func (l *RedisTokenBucket) key(raw string) string {
    if l.Prefix == "" {
        return raw
    }
    return l.Prefix + ":" + raw
}

func (l *RedisTokenBucket) Allow(ctx context.Context, rawKey string) (bool, int, error) {
    if l.Redis == nil || l.Capacity <= 0 || l.RefillRate <= 0 {
        return true, 0, nil
    }

    now := float64(time.Now().UnixNano()) / 1e9
    ttl := int64(float64(l.Capacity)/l.RefillRate) + 1

    res, err := tokenBucketScript.Run(ctx, l.Redis, []string{l.key(rawKey)}, l.Capacity, l.RefillRate, now, ttl).Result()
    if err != nil {
        return false, 0, err
    }

    vals, ok := res.([]interface{})
    if !ok || len(vals) != 2 {
        return false, 0, redis.ErrClosed
    }

    allowedInt, ok := toInt64(vals[0])
    if !ok {
        return false, 0, redis.ErrClosed
    }
    remainingFloat, ok := toFloat64(vals[1])
    if !ok {
        return false, 0, redis.ErrClosed
    }

    allowed := allowedInt == 1
    remaining := int(remainingFloat)
    return allowed, remaining, nil
}

func toInt64(v interface{}) (int64, bool) {
    switch t := v.(type) {
    case int64:
        return t, true
    case float64:
        return int64(t), true
    case string:
        f, err := strconv.ParseFloat(t, 64)
        if err != nil {
            return 0, false
        }
        return int64(f), true
    default:
        return 0, false
    }
}

func toFloat64(v interface{}) (float64, bool) {
    switch t := v.(type) {
    case float64:
        return t, true
    case int64:
        return float64(t), true
    case string:
        f, err := strconv.ParseFloat(t, 64)
        if err != nil {
            return 0, false
        }
        return f, true
    default:
        return 0, false
    }
}

func RateLimitMiddleware(l *RedisTokenBucket, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            key := ""
            if keyFn != nil {
                key = keyFn(r)
            }
            if key == "" {
                next.ServeHTTP(w, r)
                return
            }

            allowed, _, err := l.Allow(r.Context(), key)
            if err != nil {
                WriteJSONError(w, r, http.StatusServiceUnavailable, "rate_limiter_unavailable")
                return
            }
            if !allowed {
                WriteJSONError(w, r, http.StatusTooManyRequests, "rate_limited")
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
