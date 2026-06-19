package middleware

import (
	"log"

	"github.com/gin-gonic/gin"

	appErr "privacy-relay/pkg/errors"
)

func ValidateRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		contentType := c.GetHeader("Content-Type")
		if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH" {
			if contentType == "" {
				SetError(c, appErr.InvalidParams("Content-Type header required"))
				return
			}
		}
		c.Next()
	}
}

func PanicRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				requestID, _ := c.Get("request_id")
				log.Printf("[PANIC] request_id=%v recovered: %v", requestID, r)
				SetError(c, appErr.InternalError("internal panic recovered", nil))
			}
		}()
		c.Next()
	}
}
