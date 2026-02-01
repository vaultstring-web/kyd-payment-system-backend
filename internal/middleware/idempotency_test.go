package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestIdempotencyMiddleware_ConcurrentRequests(t *testing.T) {
	// Setup Redis mock or real connection
	// For simplicity, assuming a real redis is available or we use miniredis. 
	// Since I cannot install miniredis easily, I will skip if no redis.
	// But wait, the environment has docker-compose. I can assume redis is running on localhost:6379?
	// Or I can just rely on the code logic review. 
	// To be safe and fast, I will rely on code review and manual verification strategy.
	// But writing a unit test that mocks the redis client would be better.
	// The middleware uses *redis.Client. It's hard to mock without an interface.
	// However, I can try to connect to localhost:6379.
	
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skip("Redis not available")
	}

	mw := NewIdempotencyMiddleware(rdb, 10*time.Second)

	// Handler that sleeps 2 seconds
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := mw.Require(slowHandler)

	var wg sync.WaitGroup
	wg.Add(2)

	// First request
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Idempotency-Key", "test-key-1")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}()

	// Second request (starts 100ms later)
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Idempotency-Key", "test-key-1")
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		// Should also be OK (replayed)
		assert.Equal(t, http.StatusOK, w.Code)
	}()

	wg.Wait()
}
