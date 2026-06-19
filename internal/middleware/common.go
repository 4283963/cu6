package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"privacy-relay/internal/model"
	appErr "privacy-relay/pkg/errors"
	"privacy-relay/pkg/utils"
)

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r *responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = utils.GenerateRequestID()
			c.Request.Header.Set("X-Request-ID", requestID)
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)

		var reqBody []byte
		if c.Request.Body != nil {
			reqBody, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		}

		w := &responseBodyWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
		}
		c.Writer = w

		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		bodyForLog := string(reqBody)
		if len(bodyForLog) > 2000 {
			bodyForLog = bodyForLog[:2000] + "...(truncated)"
		}

		respBody := w.body.String()
		if len(respBody) > 1000 {
			respBody = respBody[:1000] + "...(truncated)"
		}

		log.Printf(
			"[HTTP] request_id=%s method=%s path=%s query=%s status=%d duration=%v ip=%s ua=%s req=%s resp=%s",
			requestID, method, path, query, status, duration, clientIP, userAgent, bodyForLog, respBody,
		)
	}
}

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		lastErr := c.Errors.Last()
		if lastErr == nil {
			return
		}

		err := lastErr.Err
		requestID, _ := c.Get("request_id")

		var resp model.CommonResponse
		resp.Code = int(appErr.CodeInternalError)
		resp.Message = "internal server error"

		if ae, ok := err.(*appErr.AppError); ok {
			resp.Code = int(ae.Code)
			resp.Message = ae.Message
			if ae.Code == appErr.CodeInternalError ||
				ae.Code == appErr.CodeDatabaseError ||
				ae.Code == appErr.CodeCacheError {
				log.Printf("[ERROR] request_id=%v code=%d msg=%s detail=%v", requestID, ae.Code, ae.Message, ae.Err)
			}
		} else {
			log.Printf("[ERROR] request_id=%v unhandled_error=%v", requestID, err)
		}

		c.JSON(c.Writer.Status(), resp)
	}
}

func ResponseFormatter() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Writer.Written() {
			return
		}

		if len(c.Errors) > 0 {
			return
		}

		data, exists := c.Get("response_data")
		if !exists {
			return
		}

		resp := model.CommonResponse{
			Code:    int(appErr.CodeSuccess),
			Message: "success",
			Data:    data,
		}

		statusCode, _ := c.Get("response_status")
		code, ok := statusCode.(int)
		if !ok {
			code = 200
		}
		c.JSON(code, resp)
	}
}

func SetOK(c *gin.Context, data interface{}) {
	c.Set("response_data", data)
	c.Set("response_status", 200)
	c.Abort()
}

func SetCreated(c *gin.Context, data interface{}) {
	c.Set("response_data", data)
	c.Set("response_status", 201)
	c.Abort()
}

func SetError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()

	if ae, ok := err.(*appErr.AppError); ok {
		statusCode := mapCodeToHTTP(ae.Code)
		c.Status(statusCode)
	} else {
		c.Status(500)
	}
}

func SetErrorWithStatus(c *gin.Context, err error, httpStatus int) {
	_ = c.Error(err)
	c.Abort()
	c.Status(httpStatus)
}

func mapCodeToHTTP(code appErr.Code) int {
	switch code {
	case appErr.CodeSuccess:
		return 200
	case appErr.CodeInvalidParams:
		return 400
	case appErr.CodeUnauthorized:
		return 401
	case appErr.CodeForbidden:
		return 403
	case appErr.CodeNotFound:
		return 404
	case appErr.CodeConflict, appErr.CodeIdempotentLock:
		return 409
	case appErr.CodeReplayAttack:
		return 429
	case appErr.CodeStateTransition:
		return 422
	case appErr.CodeMaxRetryExceeded:
		return 503
	default:
		return 500
	}
}

func validateJSONField(c *gin.Context, obj interface{}) bool {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		SetError(c, appErr.InvalidParams("failed to read request body"))
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	if err := json.Unmarshal(body, obj); err != nil {
		SetError(c, appErr.InvalidParams("invalid JSON: "+err.Error()))
		return false
	}
	return true
}
