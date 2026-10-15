package ratelimiter

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *RatelimiterHandler) ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}
