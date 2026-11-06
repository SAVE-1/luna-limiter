package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/SAVE-1/distributed-rate-limiter/internal"
	"github.com/SAVE-1/distributed-rate-limiter/ratelimiter"
	"github.com/urfave/cli/v3"
)

/*

LIMITATIONS:
	for now, only allowed global window is 1min

FOR NOW:
	if the REDIS cache instance is down,
	the internal cache will be updated, but once REDIS is back up again,
	the REDIS cache will not be updated against the internal cache, but I suppose it is okay'ish in this case
	as the data is not mission critical, and the data velocity should be pretty high as well

	it does mean the rate limiting -rate is going to be somewhat lower in some cases though,
	which is bad for the overall system, but maybe I'll just live with it for now

LIMITATIONS
	For now, the only supported global window is 1 minute.

CURRENT BEHAVIOR / KNOWN ISSUES
	If the Redis cache instance goes down:
	– The in-memory (internal) cache continues to be updated normally.
	– Once Redis comes back online, it will not be automatically repopulated from the in-memory cache.
	– This is acceptable for the moment because the data is not mission-critical and has high churn/velocity anyway.

	This does mean that, during a Redis outage + recovery, the effective rate-limit enforcement may be slightly looser than intended in some cases.
	– This is suboptimal for the overall system, but I’ll live with it for now.

VARIABLE PRECEDENCE:
	I tested enviromental vs. commandline vs. default -variable precedence in powershell

	$env:testVar = "test from powershell"

	go run main.go --testVar jee

	precedence is:
		command line > environmental > default
*/

const (
	REDIS_ADDR          string = "redisaddr"
	REDIS_USER          string = "redisuser"
	REDIS_PASSWORD      string = "redispassword"
	PERIOD              string = "period"
	REQLIMIT            string = "reqlimit"
	START_WITHOUT_REDIS string = "start_without_redis"
	VERSION             string = "version"
	ALGORITHM           string = "algorithm"
	PORT                string = "port"
	MODE                string = "log_format"
)

func main() {
	cmd := &cli.Command{
		UseShortOptionHandling: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    REDIS_ADDR,
				Value:   "",
				Usage:   "redis ip address",
				Sources: cli.EnvVars("LUNA_REDIS_ADDR"),
			},
			&cli.StringFlag{
				Name:    REDIS_USER,
				Value:   "",
				Usage:   "redis user name",
				Sources: cli.EnvVars("LUNA_REDIS_USER"),
			},
			&cli.StringFlag{
				Name:    REDIS_PASSWORD,
				Value:   "",
				Usage:   "redis user password",
				Sources: cli.EnvVars("LUNA_REDIS_PASSWORD"),
			},
			&cli.IntFlag{
				Name:    PERIOD,
				Value:   60,
				Usage:   "user request reset period in seconds",
				Sources: cli.EnvVars("LUNA_PERIOD"),
				Validator: func(t int) error {
					if t < 0 {
						return fmt.Errorf("The period must greater than zero")
					}
					return nil
				}},
			&cli.IntFlag{
				Name:    REQLIMIT,
				Value:   2,
				Usage:   "user request limit",
				Sources: cli.EnvVars("LUNA_REQ_LIMIT"),
				Validator: func(t int) error {
					if t <= 0 {
						return fmt.Errorf("User request limit must be greater than zero")
					}
					return nil
				}},
			&cli.BoolFlag{
				Name:    START_WITHOUT_REDIS,
				Value:   false,
				Usage:   "is startup without Redis allowed",
				Sources: cli.EnvVars("LUNA_START_WITHOUT_REDIS"),
			},
			&cli.BoolFlag{
				Name:    VERSION,
				Value:   false,
				Aliases: []string{"V", "v"},
				Usage:   "server version",
			},
			&cli.StringFlag{
				Name:    ALGORITHM,
				Value:   "fixed_window",
				Usage:   "what the default algorithm is used",
				Sources: cli.EnvVars("LUNA_ALGORITHM"),
				Validator: func(t string) error {
					if internal.VerifyAlgorithm(t) {
						return nil
					}
					return fmt.Errorf("Wrong algorithm in environment variables")
				}},
			&cli.IntFlag{
				Name:    PORT,
				Value:   12600,
				Usage:   "what port should be used",
				Sources: cli.EnvVars("LUNA_PORT", "LUNA_RATELIMITER_PORT"),
			},
			&cli.StringFlag{
				Name:    MODE,
				Value:   "log_format",
				Usage:   "what release mode should be used for logging",
				Sources: cli.EnvVars("LUNA_RELEASE_MODE"),
				Validator: func(m string) error {
					if m != ratelimiter.Development && m != ratelimiter.Production {
						return fmt.Errorf("mode must be %q or %q", ratelimiter.Development, ratelimiter.Production)
					}
					return nil
				}},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Bool(VERSION) {
				fmt.Println("Luna rate limiter, version:", ratelimiter.GetVersion())
				return nil
			}

			seconds, err := time.ParseDuration(strconv.Itoa(int(cmd.Int(PERIOD))) + "s")
			if err != nil {
				return err
			}

			config := ratelimiter.RateLimiterConfiguration{
				RedisAddress:             cmd.String(REDIS_ADDR),
				RedisUsername:            cmd.String(REDIS_USER),
				RedisPassword:            cmd.String(REDIS_PASSWORD),
				Period:                   seconds,
				Limit:                    int64(cmd.Int(REQLIMIT)),
				AllowStartupWithoutRedis: cmd.Bool(START_WITHOUT_REDIS),
				Port:                     cmd.Int(PORT),
				LogFormat:                     cmd.String(MODE),
				DefaultAlgorithm:         cmd.String(ALGORITHM),
			}

			if err := ratelimiter.Start(config); err != nil {
				return err
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}

}
