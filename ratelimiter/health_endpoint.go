package ratelimiter

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *RatelimiterHandler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message":      "feelin' fine!",
		"health":       "healthy",
		"health-level": 100,
		"code":         239, // a custom code, mostly to have it, and as a placeholder I guess, no biggie
	})
}
