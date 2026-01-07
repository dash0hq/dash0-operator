package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type StoredRequest struct {
	Method string  `json:"method"`
	Url    string  `json:"url"`
	Body   *[]byte `json:"body,omitempty"`
}

var (
	requests     = make([]StoredRequest, 0)
	requestMutex sync.RWMutex

	checkRuleOrigins     []string
	checkRuleOriginMutex sync.RWMutex
)

func main() {
	router := gin.Default()

	router.Use(gin.Recovery(), Logger())

	router.GET("/ready", func(ginCtx *gin.Context) {
		ginCtx.Status(http.StatusNoContent)
	})

	router.PUT("/api/synthetic-checks/:origin", handleSyntheticCheckRequest)
	router.DELETE("/api/synthetic-checks/:origin", handleSyntheticCheckRequest)

	router.PUT("/api/views/:origin", handleViewRequest)
	router.DELETE("/api/views/:origin", handleViewRequest)

	router.PUT("/api/dashboards/:origin", handleDashboardRequest)
	router.DELETE("/api/dashboards/:origin", handleDashboardRequest)

	router.GET("/api/alerting/check-rules", handleGetCheckRuleOriginsRequest)
	router.PUT("/api/alerting/check-rules/:origin", handlePutCheckRuleRequest)
	router.DELETE("/api/alerting/check-rules/:origin", handleDeleteCheckRuleRequest)

	router.GET("/requests", getAllRequests)
	router.DELETE("/requests", deleteStoredRequests)

	server := &http.Server{
		Addr:    ":8001",
		Handler: router,
	}
	server.SetKeepAlivesEnabled(false)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func handleSyntheticCheckRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{
		"message": "ok",
	})
}

func handleViewRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{
		"message": "ok",
	})
}

func handleDashboardRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{
		"message": "ok",
	})
}

func handleGetCheckRuleOriginsRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)

	checkRuleOriginMutex.RLock()
	defer checkRuleOriginMutex.RUnlock()

	responsePayload := make([]any, 0, len(checkRuleOrigins))
	for _, checkRuleOrigin := range checkRuleOrigins {
		responsePayload = append(responsePayload, map[string]any{
			"origin": checkRuleOrigin,
		})
	}
	ginCtx.JSON(http.StatusOK, responsePayload)
}

func handlePutCheckRuleRequest(ginCtx *gin.Context) {
	ok := storeCheckRuleOrigin(ginCtx)
	if !ok {
		return
	}
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{
		"message": "ok",
	})
}

func storeCheckRuleOrigin(ginCtx *gin.Context) bool {
	checkRuleOrigin := ginCtx.Param("origin")
	if checkRuleOrigin == "" {
		ginCtx.JSON(http.StatusBadRequest, map[string]any{
			"message": "check rule origin is required",
		})
		return false
	}

	checkRuleOriginMutex.Lock()
	defer checkRuleOriginMutex.Unlock()
	checkRuleOrigins = append(checkRuleOrigins, checkRuleOrigin)
	return true
}

func handleDeleteCheckRuleRequest(ginCtx *gin.Context) {
	ok := deleteCheckRuleOrigin(ginCtx)
	if !ok {
		return
	}
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{
		"message": "ok",
	})
}

func deleteCheckRuleOrigin(ginCtx *gin.Context) bool {
	checkRuleOrigin := ginCtx.Param("origin")
	if checkRuleOrigin == "" {
		ginCtx.JSON(http.StatusBadRequest, map[string]any{
			"message": "check rule origin is required",
		})
		return false
	}

	checkRuleOriginMutex.Lock()
	defer checkRuleOriginMutex.Unlock()
	checkRuleOrigins = slices.DeleteFunc(checkRuleOrigins, func(val string) bool { return val == checkRuleOrigin })
	return true
}

func storeRequest(ginCtx *gin.Context) {
	fmt.Printf("handling request: %s %s\n", ginCtx.Request.Method, ginCtx.Request.URL.String())
	req := ginCtx.Request

	var body *[]byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			ginCtx.JSON(http.StatusInternalServerError, map[string]any{"error": "failed to read request body"})
			return
		}
		body = &b
	}

	requestMutex.Lock()
	defer requestMutex.Unlock()
	requests = append(requests, StoredRequest{
		Method: req.Method,
		Url:    req.URL.String(),
		Body:   body,
	})
}

func getAllRequests(ginCtx *gin.Context) {
	fmt.Printf("retrieving stored requests: %s %s\n", ginCtx.Request.Method, ginCtx.Request.URL.String())
	requestMutex.RLock()
	defer requestMutex.RUnlock()
	ginCtx.JSON(http.StatusOK, map[string]any{
		"requests": requests,
	})
}

func deleteStoredRequests(ginCtx *gin.Context) {
	fmt.Printf("deleting stored requests: %s %s\n", ginCtx.Request.Method, ginCtx.Request.URL.String())
	requestMutex.Lock()
	defer requestMutex.Unlock()
	requests = make([]StoredRequest, 0)
	ginCtx.Status(http.StatusNoContent)
}

func Logger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(Param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s - [%s], %s %s %d %s \n",
			Param.ClientIP,
			Param.TimeStamp.Format(time.RFC1123Z),
			Param.Method,
			Param.Path,
			Param.StatusCode,
			Param.Latency,
		)
	})
}
