package ratelimiter

import (
	"net/http"
	"strconv"
	"time"

	"github.com/SAVE-1/distributed-rate-limiter/internal"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// isRequestAllowed(clientId, rulesId) -> {passes: boolean, remaining: number, resetTime: timestamp}
func (h *RatelimiterHandler) isRequestAllowed(c *gin.Context) {
	const useRistrettoCostCalc int64 = 0
	var messageFromClient IncomingMessage
	var userId string

	if err := c.ShouldBindJSON(&messageFromClient); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Malformed JSON, missing either 'client id' or 'rules id'"})
		return
	}

	algo := messageFromClient.Algorithm

	if !internal.VerifyAlgorithm(algo) {
		algo = h.Config.DefaultAlgorithm
	}

	userId = messageFromClient.ClientId
	requestUserHash := "ratelimit:" + userId + ":" + algo

	if entry, found := h.ristrettoCache.Get(requestUserHash); found {
		tooManyRequests := entry.HitCount >= h.Config.Limit
		if tooManyRequests {
			entry.HitCount++

			// now - "when first hit registered" + "configured period"
			resets := time.Duration(time.Now().UnixNano()) - time.Duration(entry.FirstHit) + h.Config.Period

			h.ristrettoCache.SetWithTTL(requestUserHash, entry, 0, resets)
			h.ristrettoCache.Wait()
			
			// needs headers
			c.Writer.Header().Set("Content-Type", "application/json")
			c.Writer.Header().Set("RateLimit-Remaining", "0")
			c.Writer.Header().Set("RateLimit-Reset", strconv.FormatInt(secondsUntilRatelimitReset(entry.FirstHit, h.Config.Period), 10))
			c.Writer.Header().Set("RateLimit-Limit", strconv.FormatInt(h.Config.Limit, 10))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"Passes":     false,
				"HitCount":   entry.HitCount,
				"FirstHit":   entry.FirstHit,
				"Remaining":  0,
				"ResetsUnix": time.Now().Add(resets).Unix(),
			})
			h.Logger.Info("entity found in cache", zap.String("entity", entry.String()))
			return
		}
	}

	v, err := h.RedisConnection.ProcessRatelimitRequest(requestUserHash, int(h.Config.Period.Seconds()), int(h.Config.Limit), algo)

	// TODO: not sure about this branch, I mean, yeah it has to exist, but the internals could be a tad better
	if err != nil {
		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.Header().Set("RateLimit-Remaining", strconv.FormatInt(v.Remaining, 10))
		c.Writer.Header().Set("RateLimit-Reset", strconv.FormatInt(secondsUntilRatelimitReset(v.FirstHit, h.Config.Period), 10))
		c.Writer.Header().Set("RateLimit-Limit", strconv.FormatInt(h.Config.Limit, 10))
		c.JSON(http.StatusInternalServerError, nil)

		o := internal.RedisEntry{
			HitCount: v.HitCount,
			FirstHit: v.FirstHit,
		}

		h.ristrettoCache.SetWithTTL(requestUserHash, o, 0, h.Config.Period-(time.Duration(time.Now().UnixNano())-time.Duration(o.FirstHit)))
		h.ristrettoCache.Wait()
		h.Logger.Info("error with Redis connection", zap.Error(err))
		return
	}

	newEntry := internal.RedisEntry{HitCount: v.HitCount, FirstHit: v.FirstHit}

	expiry := time.Unix(v.FirstHit, 0).Add(h.Config.Period)
	ttl := time.Until(expiry)

	if ttl > 0 {
		h.ristrettoCache.SetWithTTL(requestUserHash, newEntry, 0, ttl)
		h.ristrettoCache.Wait()
	}

	status := http.StatusOK
	if !v.Passes {
		status = http.StatusTooManyRequests
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.Header().Set("RateLimit-Remaining", strconv.FormatInt(v.Remaining, 10))
	c.Writer.Header().Set("RateLimit-Reset", strconv.FormatInt(secondsUntilRatelimitReset(v.FirstHit, h.Config.Period), 10))
	c.Writer.Header().Set("RateLimit-Limit", strconv.FormatInt(h.Config.Limit, 10))

	c.JSON(status, v)
}


func secondsUntilRatelimitReset(userFirstHit int64, window time.Duration) int64 {
	/*
		example:
			12:59:00 <- user first hit
			12:59:30 <- user reached limit
			13:00:00 <- rate limit reset

		so:
			rateLimitTimer = 13:00:00
			userFirstHit = 12:59:00
			timeNow = 12:59:30
	*/

	userFirstHitAsTime := time.Unix(userFirstHit, 0)
	const millisInSecond int64 = 1000
	return (userFirstHitAsTime.Add(window).UnixMilli() - time.Now().UnixMilli()) / millisInSecond
}
