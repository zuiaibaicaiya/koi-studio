package middleware

import (
	"sync"
	"time"

	"koi-server/packages/socketio/contracts"

	"github.com/goravel/framework/facades"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

type RateLimiter struct {
	visitors map[string]*visitor
	mutex    sync.Mutex
	limit    int
	window   time.Duration
}

type visitor struct {
	count    int
	lastSeen time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		rl.mutex.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mutex.Unlock()
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{count: 1, lastSeen: time.Now()}
		return true
	}

	if time.Since(v.lastSeen) > rl.window {
		v.count = 1
		v.lastSeen = time.Now()
		return true
	}

	if v.count >= rl.limit {
		return false
	}

	v.count++
	v.lastSeen = time.Now()
	return true
}

func RateLimitMiddleware(limit int, window time.Duration) contracts.Middleware {
	limiter := NewRateLimiter(limit, window)

	return contracts.MiddlewareFunc(func(socket *socketio.Socket, next func() error) error {
		socketId := string(socket.Id())

		if !limiter.Allow(socketId) {
			facades.Log().Warning("Rate limit exceeded for socket: " + socketId)
			return nil
		}

		return next()
	})
}
