package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type StoredRequest struct {
	Method string  `json:"method"`
	Url    string  `json:"url"`
	Body   *[]byte `json:"body,omitempty"`
}

// responseOverride instructs the mock to respond to requests with the given HTTP Method whose URL contains
// RouteSubstring with the given StatusCode, for the first Times matching requests. Once Times reaches zero, the mock
// responds with HTTP 200 again. This is used by the e2e tests to simulate transient failures (HTTP 503 etc.) for a
// specific resource, so that the operator's periodic synchronization retry can be tested.
type responseOverride struct {
	// Method is the HTTP method (e.g. "PUT", "DELETE") the override applies to. Only requests with this method are
	// matched.
	Method string `json:"method"`
	// RouteSubstring is matched against the request URL: the override applies to requests whose URL contains this
	// substring (e.g. the name of a specific rule, dashboard or spam filter).
	RouteSubstring string `json:"routeSubstring"`
	StatusCode     int    `json:"statusCode"`
	// Times is the number of matching requests that should still receive StatusCode. It is decremented on each match
	// and is also accepted (as the initial value) when configuring overrides via PUT /control/response-overrides.
	Times int `json:"times"`
}

var (
	requests     = make([]StoredRequest, 0)
	requestMutex sync.RWMutex

	checkRuleOrigins     []string
	checkRuleOriginMutex sync.RWMutex

	recordingRuleOrigins     []string
	recordingRuleOriginMutex sync.RWMutex

	responseOverrides     []responseOverride
	responseOverrideMutex sync.Mutex
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

	router.PUT("/api/notification-channels/:origin", handleNotificationChannelRequest)
	router.DELETE("/api/notification-channels/:origin", handleNotificationChannelRequest)

	router.PUT("/api/spam-filters/:origin", handleSpamFilterRequest)
	router.DELETE("/api/spam-filters/:origin", handleSpamFilterRequest)

	router.PUT("/api/dashboards/:origin", handleDashboardRequest)
	router.DELETE("/api/dashboards/:origin", handleDashboardRequest)

	router.PUT("/api/sampling-rules/:origin", handleSamplingRuleRequest)
	router.DELETE("/api/sampling-rules/:origin", handleSamplingRuleRequest)

	router.PUT("/api/signal-to-metrics/:origin", handleSignalToMetricsRequest)
	router.DELETE("/api/signal-to-metrics/:origin", handleSignalToMetricsRequest)

	router.GET("/api/recording-rules", handleGetRecordingRuleOriginsRequest)
	router.PUT("/api/recording-rules/:origin", handlePutRecordingRuleRequest)
	router.DELETE("/api/recording-rules/:origin", handleDeleteRecordingRuleRequest)

	router.GET("/api/alerting/check-rules", handleGetCheckRuleOriginsRequest)
	router.PUT("/api/alerting/check-rules/:origin", handlePutCheckRuleRequest)
	router.DELETE("/api/alerting/check-rules/:origin", handleDeleteCheckRuleRequest)

	router.GET("/requests", getAllRequests)
	router.DELETE("/requests", deleteStoredRequests)

	router.PUT("/control/response-overrides", setResponseOverrides)
	router.DELETE("/control/response-overrides", clearResponseOverrides)

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
	respondWithOverrideOrOK(ginCtx)
}

func handleViewRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func handleDashboardRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func handleNotificationChannelRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func handleSamplingRuleRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func handleSignalToMetricsRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func handleSpamFilterRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
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
	respondWithOverrideOrOK(ginCtx)
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
	respondWithOverrideOrOK(ginCtx)
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

func handleGetRecordingRuleOriginsRequest(ginCtx *gin.Context) {
	storeRequest(ginCtx)

	recordingRuleOriginMutex.RLock()
	defer recordingRuleOriginMutex.RUnlock()

	responsePayload := make([]any, 0, len(recordingRuleOrigins))
	for _, recordingRuleOrigin := range recordingRuleOrigins {
		responsePayload = append(responsePayload, map[string]any{
			"origin": recordingRuleOrigin,
		})
	}
	ginCtx.JSON(http.StatusOK, responsePayload)
}

func handlePutRecordingRuleRequest(ginCtx *gin.Context) {
	ok := storeRecordingRuleOrigin(ginCtx)
	if !ok {
		return
	}
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func storeRecordingRuleOrigin(ginCtx *gin.Context) bool {
	recordingRuleOrigin := ginCtx.Param("origin")
	if recordingRuleOrigin == "" {
		ginCtx.JSON(http.StatusBadRequest, map[string]any{
			"message": "recording rule origin is required",
		})
		return false
	}

	recordingRuleOriginMutex.Lock()
	defer recordingRuleOriginMutex.Unlock()
	recordingRuleOrigins = append(recordingRuleOrigins, recordingRuleOrigin)
	return true
}

func handleDeleteRecordingRuleRequest(ginCtx *gin.Context) {
	ok := deleteRecordingRuleOrigin(ginCtx)
	if !ok {
		return
	}
	storeRequest(ginCtx)
	respondWithOverrideOrOK(ginCtx)
}

func deleteRecordingRuleOrigin(ginCtx *gin.Context) bool {
	recordingRuleOrigin := ginCtx.Param("origin")
	if recordingRuleOrigin == "" {
		ginCtx.JSON(http.StatusBadRequest, map[string]any{
			"message": "recording rule origin is required",
		})
		return false
	}

	recordingRuleOriginMutex.Lock()
	defer recordingRuleOriginMutex.Unlock()
	recordingRuleOrigins = slices.DeleteFunc(recordingRuleOrigins, func(val string) bool { return val == recordingRuleOrigin })
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
	requests = make([]StoredRequest, 0)
	requestMutex.Unlock()

	// Also reset any configured response overrides, so each test starts from a clean slate.
	responseOverrideMutex.Lock()
	responseOverrides = nil
	responseOverrideMutex.Unlock()

	ginCtx.Status(http.StatusNoContent)
}

// respondWithOverrideOrOK responds to a request, taking any configured response override into account. If the request's
// method and URL match a configured override with a remaining count greater than zero, it responds with the configured
// status code (and decrements the remaining count). In all other cases it responds with HTTP 200.
func respondWithOverrideOrOK(ginCtx *gin.Context) {
	if statusCode, ok := consumeResponseOverride(ginCtx.Request); ok {
		fmt.Printf("responding with override status code %d for %s %s\n",
			statusCode, ginCtx.Request.Method, ginCtx.Request.URL.String())
		ginCtx.JSON(statusCode, map[string]any{
			"message": "simulated failure",
		})
		return
	}
	ginCtx.JSON(http.StatusOK, map[string]any{
		"message": "ok",
	})
}

// consumeResponseOverride returns the status code that should be used for the given request if a matching response
// override with a remaining count greater than zero exists, decrementing that override's remaining count. A request
// matches an override when its HTTP method equals the override's Method and its URL contains the override's
// RouteSubstring.
func consumeResponseOverride(req *http.Request) (int, bool) {
	url := req.URL.String()

	responseOverrideMutex.Lock()
	defer responseOverrideMutex.Unlock()
	for i := range responseOverrides {
		override := &responseOverrides[i]
		if req.Method == override.Method && strings.Contains(url, override.RouteSubstring) && override.Times > 0 {
			override.Times--
			return override.StatusCode, true
		}
	}
	return 0, false
}

func setResponseOverrides(ginCtx *gin.Context) {
	fmt.Printf("setting response overrides: %s %s\n", ginCtx.Request.Method, ginCtx.Request.URL.String())
	var overrides []responseOverride
	if err := ginCtx.ShouldBindJSON(&overrides); err != nil {
		ginCtx.JSON(http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	responseOverrideMutex.Lock()
	responseOverrides = overrides
	responseOverrideMutex.Unlock()

	ginCtx.Status(http.StatusNoContent)
}

func clearResponseOverrides(ginCtx *gin.Context) {
	fmt.Printf("clearing response overrides: %s %s\n", ginCtx.Request.Method, ginCtx.Request.URL.String())
	responseOverrideMutex.Lock()
	responseOverrides = nil
	responseOverrideMutex.Unlock()
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
