// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
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
)

func main() {
	router := gin.Default()

	router.Use(gin.Recovery(), Logger())

	router.GET("/ready", func(ginCtx *gin.Context) {
		ginCtx.Status(http.StatusNoContent)
	})

	// URL templates consumed by the dash0settingsonedgeextension in the
	// IE-enabled collector. See:
	//   /components/collector/extension/dash0settingsonedgeextension/settings_provider.go
	//   /components/collector/extension/dash0settingsonedgeextension/pattern_provider.go
	// in the dash0 repo. Returning empty payloads is enough to keep the
	// extension from error-looping during e2e tests.
	router.GET("/public/edge/settings", handleEdgeSettings)
	router.GET("/public/edge/patterns", handleEdgePatterns)
	router.POST("/public/edge/patterns/report", handleEdgePatternsReport)

	router.GET("/requests", getAllRequests)
	router.DELETE("/requests", deleteStoredRequests)

	server := &http.Server{
		Addr:    ":8002",
		Handler: router,
	}
	server.SetKeepAlivesEnabled(false)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func handleEdgeSettings(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{})
}

func handleEdgePatterns(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	ginCtx.JSON(http.StatusOK, map[string]any{})
}

func handleEdgePatternsReport(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	ginCtx.Status(http.StatusNoContent)
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
