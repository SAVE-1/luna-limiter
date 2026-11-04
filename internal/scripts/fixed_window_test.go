package scripts

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9/maintnotifications"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	redis_test_containers "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	redis_client "github.com/redis/go-redis/v9"
)

type TestContainers struct {
	RedisContainer *redis.RedisContainer
	// TestRateLimiter *ratelimiter.RatelimiterHandler
}

var testContainers *TestContainers
var my_redis_client *redis_client.Client

func (tc *TestContainers) Cleanup(ctx context.Context) error {
	if tc.RedisContainer != nil {
		if err := tc.RedisContainer.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate redis container: %w", err)
		}
	}

	if my_redis_client != nil {
		err := my_redis_client.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// / https://oneuptime.com/blog/post/2026-01-07-go-integration-tests-testcontainers/view
func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	testContainers, err = SetupContainers(ctx)
	if err != nil {
		fmt.Printf("Failed to set up containers: %v\n", err)
		os.Exit(1)
	}

	my_redis_client, err = openRedisConnection(ctx)

	if err != nil {
		fmt.Printf("Failed to set up Redis connection: %v\n", err)
		os.Exit(1)
	}

	// run tests
	code := m.Run() 

	if err := testContainers.Cleanup(ctx); err != nil {
		fmt.Printf("Failed to clean up containers: %v\n", err)
	}

	os.Exit(code)
}

func SetupContainers(ctx context.Context) (*TestContainers, error) {
	tc := &TestContainers{}

	redisContainer, err := redis_test_containers.Run(ctx, "redis:7-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").
				WithStartupTimeout(30*time.Second),
		),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to start redis container: %w", err)
	}
	tc.RedisContainer = redisContainer

	return tc, nil
}


func openRedisConnection(ctx context.Context) (*redis_client.Client, error) {
	uri, err := testContainers.RedisContainer.ConnectionString(ctx)
	if err != nil {
        return nil, fmt.Errorf("could not get connection string: %w", err)
	}

	opts, err := redis_client.ParseURL(uri)
	if err != nil {
		return nil, fmt.Errorf("could not parse redis url: %w", err)
	}

	opts.Protocol = 2
	opts.MaintNotificationsConfig = &maintnotifications.Config{
		Mode: maintnotifications.ModeDisabled,
	}

	client := redis_client.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("could not ping: %v", err)
	}

	return client, nil
}

func TestTest(t *testing.T) {
	ctx := context.Background()
	err := my_redis_client.Set(ctx, "foo", "bar", 0).Err()

	if err != nil {
	    panic(err)
	}

	val, err := my_redis_client.Get(ctx, "foo").Result()
	if err != nil {
		t.Fatalf("%v", err)
	}

	fmt.Println("foo", val)
}
