package ratelimiter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	REDISPORT int = 22222
)

type TestContainers struct {
	RedisContainer  *redis.RedisContainer
	TestRateLimiter *RatelimiterHandler
}

var testContainers *TestContainers

func (tc *TestContainers) Cleanup(ctx context.Context) error {
	// Terminate the Redis container
	if tc.RedisContainer != nil {
		if err := tc.RedisContainer.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate redis container: %w", err)
		}
	}

	return nil
}

// ResetRedis flushes all data from Redis between tests
// func (tc *TestContainers) ResetRedis(ctx context.Context) error {
//     return tc.RedisClient.FlushAll(ctx).Err()
// }

/// https://oneuptime.com/blog/post/2026-01-07-go-integration-tests-testcontainers/view
func TestMain(m *testing.M) {
	// Set up the test containers
	ctx := context.Background()
	var err error
	testContainers, err = SetupContainersAndRatelimiter(ctx)
	if err != nil {
		fmt.Printf("Failed to set up containers: %v\n", err)
		os.Exit(1)
	}

	// Run all the tests
	code := m.Run()

	// Clean up the containers after tests complete
	if err := testContainers.Cleanup(ctx); err != nil {
		fmt.Printf("Failed to clean up containers: %v\n", err)
	}

	os.Exit(code)
}

func SetupContainersAndRatelimiter(ctx context.Context) (*TestContainers, error) {
	tc := &TestContainers{}

	redisContainer, err := redis.Run(ctx, "redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start redis container: %w", err)
	}
	tc.RedisContainer = redisContainer

	// "host:mappedPort", e.g. 127.0.0.1:55012. No port argument needed.
	endpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		_ = tc.Cleanup(ctx)
		return nil, fmt.Errorf("redis endpoint: %w", err)
	}

	config := RateLimiterConfiguration{
		RedisAddress:             endpoint,
		Period:                   time.Minute,
		Limit:                    2,
		AllowStartupWithoutRedis: false,
		Port:                     12600,
		Mode:                     "dev",
	}

	r, err := NewRatelimiter(config)
	if err != nil {
		_ = tc.Cleanup(ctx) // don't leak the container on failure
		return nil, fmt.Errorf("failed to create Ratelimiter: %w", err)
	}
	tc.TestRateLimiter = r
	return tc, nil
}

func TestTest(t *testing.T) {
	ctx := context.Background()
	redisC, err := testcontainers.Run(
		ctx, "redis:latest",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("6379/tcp"),
			wait.ForLog("Ready to accept connections"),
		),
	)
	testcontainers.CleanupContainer(t, redisC)
	require.NoError(t, err)
}

func TestPing(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	testContainers.TestRateLimiter.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["message"] != "pong" {
		t.Errorf("expected pong, got %v", body["message"])
	}
}

func TestHealth(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	testContainers.TestRateLimiter.router.ServeHTTP(w, req)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)

	// assert specific field values
	if body["health"] != "healthy" {
		t.Errorf("health field wrong: %v", body["health"])
	}
	if body["health-level"] != float64(100) { // JSON numbers are float64 in map[string]any
		t.Errorf("health-level wrong: %v", body["health-level"])
	}
	if body["code"] != float64(239) {
		t.Errorf("code wrong: %v", body["code"])
	}
}

// should be ok, does not reach/use redis related code at any point
func TestRateLimit_MissingFieldsInPayloadJson(t *testing.T) {
	/*
		len() 		 = 						 5
		"Passes" 	 = interface {}(bool) 	 false
		"HitCount" 	 = interface {}(float64) 3
		"FirstHit" 	 = interface {}(float64) 1787733908
		"Remaining"	 = interface {}(float64) 0
		"ResetsUnix" = interface {}(float64) 0
	*/

	var body1 map[string]any
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ratelimit",
		bytes.NewBufferString(`{"ClientId": "user1" }`))
	req.Header.Set("Content-Type", "application/json")

	testContainers.TestRateLimiter.router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &body1)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["error"]; !ok {
		t.Error("expected error field in response")
	}
}

type limitResp struct {
	Passes    bool
	HitCount  int
	Remaining int
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestRateLimiter_LimitExceeded(t *testing.T) {
	payload := fmt.Sprintf(
		`{"ClientId": %q, "RulesId": "content name", "Algorithm": "fixed_window"}`, t.Name())

	steps := []limitResp{
		{Passes: true, HitCount: 1, Remaining: 1},
		{Passes: true, HitCount: 2, Remaining: 0},
		{Passes: false, HitCount: 3, Remaining: 0},
	}
	for i, want := range steps {
		w := postJSON(t, testContainers.TestRateLimiter.router, "/v1/ratelimit", payload)

		var got limitResp
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Equal(t, want, got, "request %d", i+1)
	}
}

/// DEAD CODE FOR NOW
// func helperInitForTestContainers(t *testing.T, config RateLimiterConfiguration) *RatelimiterHandler {
// 	// code from: https://golang.testcontainers.org/quickstart/
// 	ctx := context.Background()
// 	portThing := fmt.Sprintf("%d/tcp", REDISPORT)
// 	fmt.Println("yee 1.", portThing)
// 	redisC, err := testcontainers.Run(
// 		ctx, "redis:8-alpine",
// 		testcontainers.WithExposedPorts(portThing),
// 		testcontainers.WithWaitStrategy(
// 			wait.ForListeningPort(portThing),
// 			wait.ForLog("Ready to accept connections"),
// 		),
// 	)
// 	fmt.Println("yee 2.")
// 	testcontainers.CleanupContainer(t, redisC)
// 	require.NoError(t, err)

// 	redisEndpoint, err := redisC.Endpoint(ctx, portThing)
// 	require.NoError(t, err)

// 	tt := strings.LastIndex(redisEndpoint, ":")

// 	config.RedisAddress = config.RedisAddress + ":" + redisEndpoint[tt+1:]

// 	r, err := NewRatelimiter(config)

// 	if err != nil {
// 		t.Error("error in myInit()")
// 		return nil
// 	}

// 	return r
// }
