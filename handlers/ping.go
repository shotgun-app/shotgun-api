package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Ping is a health check: if it answers, the API is running
func Ping(c *gin.Context) {
	c.String(http.StatusOK, "pong")
}
