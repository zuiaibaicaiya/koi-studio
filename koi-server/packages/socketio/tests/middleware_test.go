package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"koi-server/packages/socketio/middleware"
)

func TestMiddleware_AuthMiddleware(t *testing.T) {
	assert.NotNil(t, middleware.AuthMiddleware())
}

func TestMiddleware_LoggerMiddleware(t *testing.T) {
	assert.NotNil(t, middleware.LoggerMiddleware())
}

func TestMiddleware_RateLimiter(t *testing.T) {
	rl := middleware.NewRateLimiter(3, time.Second)

	assert.True(t, rl.Allow("192.168.1.1"))
	assert.True(t, rl.Allow("192.168.1.2"))
	assert.True(t, rl.Allow("192.168.1.1"))

	for i := 0; i < 3; i++ {
		rl.Allow("192.168.2.1")
	}
	assert.False(t, rl.Allow("192.168.2.1"))

	time.Sleep(time.Second + 100*time.Millisecond)
	assert.True(t, rl.Allow("192.168.2.1"))
}

func TestMiddleware_NewRateLimiter(t *testing.T) {
	rl := middleware.NewRateLimiter(100, time.Minute)
	assert.NotNil(t, rl)
	assert.True(t, rl.Allow("10.0.0.1"))
	assert.True(t, rl.Allow("10.0.0.2"))
}
