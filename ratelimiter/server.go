package ratelimiter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SAVE-1/distributed-rate-limiter/internal"
	"github.com/dgraph-io/ristretto/v2"
	"github.com/gin-gonic/gin"
	"go-micro.dev/v5/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	version = "0.7"
)

func GetVersion() string {
	return version
}

const (
	Development = "dev"
	Production  = "prod"
)

type RateLimiterConfiguration struct {
	RedisAddress             string
	RedisUsername            string
	RedisPassword            string
	Period                   time.Duration
	Limit                    int64
	AllowStartupWithoutRedis bool
	DefaultAlgorithm         string
	Port                     int
	LogFormat                string
}

func newLogger(c *RateLimiterConfiguration) (*zap.Logger, error) {
	var config zap.Config = zap.Config{
		OutputPaths:      []string{"stdout", "rate-limiter.log"},
		ErrorOutputPaths: []string{"stdout", "stderr", "rate-limiter.log"},
	}

	if c.LogFormat == Development {
		// NewJSONEncoder() outputs in JSON format, NewConsoleEncoder() outputs in plain text format
		encoderConfig := zap.NewDevelopmentEncoderConfig()
		// Display different colors according to levels. If not needed, use zapcore.CapitalLevelEncoder.
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		config.Development = true
		config.EncoderConfig = encoderConfig
		config.Encoding = "console"
	} else if c.LogFormat == Production {
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
		config.Development = false
		config.EncoderConfig = zap.NewProductionEncoderConfig()
		config.Encoding = "json"
	} else {
		return nil, fmt.Errorf("invalid mode '%q': must be '%q' or '%q'", c.LogFormat, Development, Production)
	}

	logger, err := config.Build()

	return logger, err
}

func NewRatelimiter(ratelimiterConfig RateLimiterConfiguration) (*RatelimiterHandler, error) {
	logger, err := newLogger(&ratelimiterConfig)
	if err != nil {
		return nil, err
	}

	defer logger.Sync()

	address := ratelimiterConfig.RedisAddress
	username := ratelimiterConfig.RedisUsername
	password := ratelimiterConfig.RedisPassword

	redisConnection := internal.NewRedisConnection(address, username, password, logger)

	client, redisConnectionError := internal.OpenRedisConnection(redisConnection)

	if redisConnectionError != nil {
		// stop the server altogether
		if !ratelimiterConfig.AllowStartupWithoutRedis {
			return nil, errors.Join(redisConnectionError, errors.New("startup not allowed without Redis"))
		}
	} else {
		logger.Info("Connected to REDIS instance successfully")
	}

	redisConnection.RedisClient = client

	serverConfig := NewHandlerConfig(ratelimiterConfig.DefaultAlgorithm, ratelimiterConfig.Period)

	h := NewRatelimiterHandler(logger, *redisConnection, serverConfig)

	if h.Config.Period != 1*time.Minute {
		logger.Error("Configuration error - global window must be 1min for now")
		return nil, errors.New("Startup not allowed with a limit other than 1min")
	}

	h.Config.Limit = ratelimiterConfig.Limit

	logger.Info("Custom logger initialized")
	logger.Info("Distributed rate limiter, version", zap.String("version", version))
	logger.Info("Allow startupt without REDIS:", zap.Bool("allow startup without redis", h.Config.AllowStartupWithoutRedis))
	logger.Info("Request window:", zap.String("request window", h.Config.Period.String()))
	logger.Info("Requests until limit:", zap.Int64("global limit", h.Config.Limit))

	var ristrettoInitError error
	h.ristrettoCache, ristrettoInitError = ristretto.NewCache(&ristretto.Config[string, internal.RedisEntry]{
		NumCounters: 1e7,     // number of keys to track frequency of (10M).
		MaxCost:     1 << 30, // maximum cost of cache (1GB).
		BufferItems: 64,      // number of keys per Get buffer.
		Cost:        redisConnection.GetRedisEntryCostFunction(),
	})

	// this is fatal, internal cache is required in all cases
	if ristrettoInitError != nil {
		h.Logger.Error("Internal error, unable to init internal cache, shutting down rate limiter", zap.Error(ristrettoInitError))
		return nil, ristrettoInitError
	} else {
		h.Logger.Info("Internal cache started successfully")
	}

	defer h.ristrettoCache.Close()

	router := BuildRouter(h)
	server := &http.Server{
		// Addr:    ":" + ratelimiterConfig.Port fmt.Printf(":%d", ratelimiterConfig.Port),
		Addr:    fmt.Sprintf(":%d", ratelimiterConfig.Port),
		Handler: router,
	}

	h.httpThingy = server
	h.router = router

	return h, nil
}

func Start(ratelimiterConfig RateLimiterConfiguration) error {
	h, err := NewRatelimiter(ratelimiterConfig)

	if err != nil {
		return err
	}

	go func() {
		log.Println("Starting server on port", ratelimiterConfig.Port)
		if err := h.httpThingy.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Listen error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	// kill (no params) by default sends syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be caught, so don't need add it
	// SIGINT = Ctrl+C, SIGTERM = Kubernetes
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.httpThingy.Shutdown(ctx); err != nil {
		logger.Fatal("Forced shutdown", zap.Error(err))
	}

	logger.Info("Server exited gracefully")
	return nil
}

// message from the web client
type IncomingMessage struct {
	ClientId  string `json:"ClientId" binding:"required"`
	RulesId   string `json:"RulesId" binding:"required"`
	Algorithm string `json:"Algorithm" binding:"required"`
}

func (i IncomingMessage) String() string {
	var b strings.Builder
	b.Grow(128) // preallocate enough

	b.WriteString("[ client-id: ")
	b.WriteString(i.ClientId)
	b.WriteString(", rules-id: ")
	b.WriteString(i.RulesId)
	b.WriteString(" ]")

	return b.String()
}

/*
	AllowStartupWithoutRedis: false,
	Period:                   1 * time.Minute,
	Limit:                    100,
*/

type HandlerConfig struct {
	DefaultAlgorithm         string
	Period                   time.Duration
	AllowStartupWithoutRedis bool
	Limit                    int64
}

func NewHandlerConfig(algo string, ttl time.Duration) *HandlerConfig {
	return &HandlerConfig{
		DefaultAlgorithm: algo,
		Period:           ttl,
	}
}

type RatelimiterHandler struct {
	Logger          *zap.Logger
	RedisConnection internal.RedisConnection
	ristrettoCache  *ristretto.Cache[string, internal.RedisEntry]
	Config          *HandlerConfig
	router          *gin.Engine
	httpThingy      *http.Server
}

func BuildRouter(h *RatelimiterHandler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New() // use gin.New() in tests — gin.Default() adds a logger that spams stdout

	v1 := router.Group("/v1")
	v1.GET("/ping", h.ping)
	v1.GET("/health", h.health)
	v1.POST("/ratelimit", h.isRequestAllowed)

	return router
}

func (h *RatelimiterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func NewRatelimiterHandler(logger *zap.Logger, conn internal.RedisConnection, h *HandlerConfig) *RatelimiterHandler {
	return &RatelimiterHandler{
		Logger:          logger,
		RedisConnection: conn,
		Config:          h,
	}
}

func (h *RatelimiterHandler) getFromCache(key string) (internal.RedisEntry, bool) {
	value, found := h.ristrettoCache.Get(key)
	if !found {
		return internal.RedisEntry{}, false
	}

	return value, true
}

type MessageToClient struct {
	Passes    bool
	ResetUnix int64
	ResetIso  string
	Limit     int64
	Remaining int64
}

func timeUntil(t int64) time.Time {
	return time.Unix(t+60, 0)
}
