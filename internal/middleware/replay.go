package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"privacy-relay/internal/config"
	"privacy-relay/internal/service"
	appErr "privacy-relay/pkg/errors"
)

func ReplayProtection(
	replaySvc service.ReplayProtectionService,
	securityCfg *config.SecurityConfig,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := c.GetHeader("X-Client-ID")
		if clientID == "" {
			clientID = c.Query("client_id")
		}
		nonce := c.GetHeader("X-Nonce")
		timestampStr := c.GetHeader("X-Timestamp")

		if nonce == "" || timestampStr == "" || clientID == "" {
			SetError(c, appErr.InvalidParams("missing required headers: X-Client-ID, X-Nonce, X-Timestamp"))
			return
		}

		timestampMs, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			SetError(c, appErr.InvalidParams("invalid X-Timestamp format, expected milliseconds"))
			return
		}

		now := time.Now().UnixMilli()
		tolerance := securityCfg.TimestampTolerance.Milliseconds()
		if now-timestampMs > tolerance || timestampMs-now > tolerance {
			SetError(c, appErr.ReplayAttack("request timestamp out of tolerance"))
			return
		}

		ok, err := replaySvc.CheckAndRecord(c.Request.Context(), clientID, nonce, timestampMs)
		if err != nil {
			SetError(c, err)
			return
		}
		if !ok {
			SetError(c, appErr.ReplayAttack("duplicate nonce detected, possible replay attack"))
			return
		}

		c.Set("client_id", clientID)
		c.Next()
	}
}

func OptionalReplayProtection(
	replaySvc service.ReplayProtectionService,
	securityCfg *config.SecurityConfig,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := c.GetHeader("X-Client-ID")
		nonce := c.GetHeader("X-Nonce")
		timestampStr := c.GetHeader("X-Timestamp")

		if nonce == "" && timestampStr == "" {
			if clientID != "" {
				c.Set("client_id", clientID)
			}
			c.Next()
			return
		}

		if clientID == "" {
			SetError(c, appErr.InvalidParams("X-Client-ID required when replay headers present"))
			return
		}
		if nonce == "" || timestampStr == "" {
			SetError(c, appErr.InvalidParams("X-Nonce and X-Timestamp must both be present"))
			return
		}

		timestampMs, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			SetError(c, appErr.InvalidParams("invalid X-Timestamp format"))
			return
		}

		now := time.Now().UnixMilli()
		tolerance := securityCfg.TimestampTolerance.Milliseconds()
		if now-timestampMs > tolerance || timestampMs-now > tolerance {
			SetError(c, appErr.ReplayAttack("request timestamp out of tolerance"))
			return
		}

		ok, err := replaySvc.CheckAndRecord(c.Request.Context(), clientID, nonce, timestampMs)
		if err != nil {
			SetError(c, err)
			return
		}
		if !ok {
			SetError(c, appErr.ReplayAttack("duplicate nonce detected"))
			return
		}

		c.Set("client_id", clientID)
		c.Next()
	}
}
