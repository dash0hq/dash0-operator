// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const (
	// common telemetry-matcher query parameters
	queryParamExpectationMode        = "expectation-mode"
	queryParamRuntime                = "runtime"
	queryParamRuntimeWorkloadName    = "runtime-workload-name"
	queryParamWorkloadType           = "workload-type"
	queryParamTimestampLowerBoundStr = "timestamp-lower-bound"
	queryParamClusterName            = "cluster"
	queryParamOperatorNamespace      = "operator-namespace"

	// query parameters for matching spans
	queryParamCheckResourceAttributes = "check-resource-attributes"
	queryParamRoute                   = "route"
	queryParamQuery                   = "query"
	queryParamTarget                  = "target"

	// query parameters for matching logs
	queryParamLogsResourceMatcherMode = "logs-resource-matcher"
	queryParamServiceVersion          = "service-version"
	queryParamLogBodyEquals           = "log-body-equals"
	queryParamLogBodyContains         = "log-body-contains"

	// query parameters for matching metrics
	queryParamMetricsMatchMode            = "metrics-match-mode"
	queryParamDeploymentName              = "deployment-name"
	queryParamExpectPodUid                = "expect-pod-uid"
	queryParamFailOnNamespaceOtherThan    = "fail-on-namespace-other-than"
	queryParamFailOnNamespaceScopedMetric = "fail-on-namespace-scoped-metric"
	queryParamMetricNameList              = "metric-name-list"
)

type Routes struct {
	Config Configuration
}

type commonQueryParams struct {
	mode                expectationMode
	runtime             string
	runtimeWorkloadName string
	workloadType        string
	timestampLowerBound time.Time
	clusterName         string
	operatorNamespace   string
}

func newRoutes(config Configuration) *Routes {
	return &Routes{
		Config: config,
	}
}

func (r *Routes) defineRoutes(router *gin.Engine) {
	router.GET("/ready", r.readyCheckRouteHandler)
	router.GET("/matching-spans", r.matchingSpansRouteHandler)
	router.GET("/matching-logs", r.matchingLogsRouteHandler)
	router.GET("/matching-metrics", r.matchingMetricsRouteHandler)
}

func (r *Routes) readyCheckRouteHandler(c *gin.Context) {
	c.Status(200)
}

// matchingSpansRouteHandler checks for matching spans using the given query parameters of the request. This is not a
// general purpose span searching engine, instead, it deliberately only implements the bare minimum of matching
// functionality used in the e2e test suite (i.e. find http server spans with a specific combination of
// http.target/http.route and url.query, see spans.go#matchHttpSpanServerSpan).
func (r *Routes) matchingSpansRouteHandler(c *gin.Context) {
	commonParams, ok := readCommonQueryParams(c)
	if !ok {
		return
	}

	checkResourceAttributes, ok := readBooleanQueryParameter(c, queryParamCheckResourceAttributes)
	if !ok {
		return
	}

	var resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans])
	if checkResourceAttributes {
		resourceMatchFn = workloadSpansResourceMatcher(
			commonParams.runtimeWorkloadName,
			commonParams.workloadType,
			commonParams.clusterName,
		)
	}

	route := c.Query(queryParamRoute)
	query := c.Query(queryParamQuery)
	target := c.Query(queryParamTarget)

	allMatchResults, err := readFileAndGetMatchingSpans(
		r.Config.TracesFile,
		resourceMatchFn,
		httpServerSpanMatcher(route, query, target),
		commonParams.timestampLowerBound,
	)
	if err != nil {
		c.JSON(500, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("error: %v", err),
		})
		return
	}

	processMatchResults(
		c,
		commonParams,
		allMatchResults,
		"span",
	)
}

// matchingLogsRouteHandler checks for matching logs using the given query parameters of the request. This is not a
// general purpose log searching engine, instead, it deliberately only implements the bare minimum of matching
// functionality used in the e2e test suite.
func (r *Routes) matchingLogsRouteHandler(c *gin.Context) {
	commonParams, ok := readCommonQueryParams(c)
	if !ok {
		return
	}

	resourceMatcherMode, ok :=
		readMandatoryStringEnumQueryParameter(c, queryParamLogsResourceMatcherMode, allLogResourceMatcherModes)
	if !ok {
		return
	}

	serviceVersion := c.Query(queryParamServiceVersion)
	var resourceMatchFn func(plog.ResourceLogs, *ResourceMatchResult[plog.ResourceLogs])
	switch logResourceMatcherMode(resourceMatcherMode) {
	case logResourceMatcherWorkload:
		resourceMatchFn = workloadLogsResourceMatcher(commonParams.runtimeWorkloadName, commonParams.workloadType)
	case logResourceMatcherSelfMonitoringLogsOperatorManager:
		resourceMatchFn = selfMonitoringLogsResourceMatcherOperatorManager(
			commonParams.clusterName,
			commonParams.operatorNamespace,
			serviceVersion,
		)
	case logResourceMatcherSelfMonitoringLogsCollector:
		resourceMatchFn = selfMonitoringLogsResourceMatcherCollector(
			commonParams.clusterName,
			commonParams.operatorNamespace,
		)
	default:
		c.JSON(400, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("unknown logs resource matcher mode: %s", resourceMatcherMode),
		})
		return
	}

	logBodyEqualsStr := c.Query(queryParamLogBodyEquals)
	logBodyContainsStr := c.Query(queryParamLogBodyContains)
	var logMatchFn func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord])
	if logBodyEqualsStr != "" {
		logMatchFn = logBodyEqualsMatcher(logBodyEqualsStr)
	} else if logBodyContainsStr != "" {
		logMatchFn = logBodyContainsMatcher(logBodyContainsStr)
	} else {
		c.JSON(400, ExpectationResult{
			Success: false,
			Description: fmt.Sprintf(
				"no log records match criteria (%s, %s, ...) have been provided",
				queryParamLogBodyEquals,
				queryParamLogBodyContains,
			),
		})
		return
	}

	allMatchResults, err := readFileAndGetMatchingLogs(
		r.Config.LogsFile,
		resourceMatchFn,
		logMatchFn,
		commonParams.timestampLowerBound,
	)
	if err != nil {
		c.JSON(500, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("error: %v", err),
		})
		return
	}

	processMatchResults(
		c,
		commonParams,
		allMatchResults,
		"log record",
	)
}

// matchingMetricsRouteHandler checks for matching metrics using the given query parameters of the request. This is not
// a general purpose metrics search engine, instead, it deliberately only implements the bare minimum of matching
// functionality used in the e2e test suite.
func (r *Routes) matchingMetricsRouteHandler(c *gin.Context) {
	commonParams, ok := readCommonQueryParams(c)
	if !ok {
		return
	}

	failOnNamespaceOtherThan := c.Query(queryParamFailOnNamespaceOtherThan)
	failOnNamespaceScopedMetric, ok := readBooleanQueryParameter(c, queryParamFailOnNamespaceScopedMetric)
	if !ok {
		return
	}

	matchMode, ok := readMandatoryStringEnumQueryParameter(c, queryParamMetricsMatchMode, allMetricsMatchModes)
	if !ok {
		return
	}

	var resourceMatchFn resourceMatcher
	var metricMatchFn metricMatcher

	switch metricsMatchMode(matchMode) {
	case metricsMatchModeWorkload:
		deploymentName := c.Query(queryParamDeploymentName)
		expectPodUid, ok := readBooleanQueryParameter(c, queryParamExpectPodUid)
		if !ok {
			return
		}
		metricNameListId, ok := readMandatoryStringEnumQueryParameter(c, queryParamMetricNameList, allMetricNameLists)
		if !ok {
			return
		}
		var metricNames []string
		switch metricNameList(metricNameListId) {
		case kubeletStatsReceiverMetricNameList:
			metricNames = kubeletStatsReceiverMetricNames
		case k8sClusterReceiverMetricNameList:
			metricNames = k8sClusterReceiverMetricNames
		case prometheusReceiverMetricNameList:
			metricNames = prometheusReceiverMetricNames
		default:
			c.JSON(400, ExpectationResult{
				Success:     false,
				Description: fmt.Sprintf("unknown metric name list: %s", metricNameListId),
			})
			return
		}
		resourceMatchFn = resourceAttributeMatcherWorkloads(deploymentName, expectPodUid)
		metricMatchFn = metricNameIsMemberOfList(metricNames)

	case metricsMatchModeSelfMonitoringOperatorManager:
		resourceMatchFn = resourceAttributeMatcherSelfMonitoringOperatorManager(commonParams.operatorNamespace)
		metricMatchFn = hasDash0OperatorPrefixMatcher()

	case metricsMatchModeSelfMonitoringCollector:
		resourceMatchFn = resourceAttributeMatcherSelfMonitoringCollector(commonParams.operatorNamespace)
		metricMatchFn = hasOtelColPrefixMatcher()

	case metricsMatchModeMatchAll:
		resourceMatchFn = matchAllResourceAttributeMatcher()
		metricMatchFn = matchAllMetricMatcher()

	default:
		c.JSON(400, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("unknown metrics matcher mode: %s", matchMode),
		})
		return
	}

	allMatchResults, err := readFileAndGetMatchingMetrics(
		r.Config.MetricsFile,
		resourceMatchFn,
		metricMatchFn,
		failOnNamespaceOtherThan,
		failOnNamespaceScopedMetric,
		commonParams.timestampLowerBound,
	)
	if err != nil {
		var mae *MatchAssertionError
		if errors.As(err, &mae) {
			// An MatchAssertionError is produced if a precondition for the metrics matching logic fails (i.e. we cannot
			// figure out the most recent timestamp for a set of metrics etc.), or if an assertion fails, for example,
			// if the metrics contain a namespace-scoped metric, although they should not. We use HTTP 417
			// "Expectation Failed" when something like this happens. This code usually specifically refers to the
			// "Expect" request-header field, so we are arguably abusing this status code here quite a bit. OTOH, there
			// is no other more matching HTTP status code.
			c.JSON(417, ExpectationResult{
				Success:     false,
				Description: fmt.Sprintf("assertion error: %v", err),
			})
		} else {
			c.JSON(500, ExpectationResult{
				Success:     false,
				Description: fmt.Sprintf("error: %v", err),
			})
		}
		return
	}

	processMatchResults(
		c,
		commonParams,
		allMatchResults,
		"metric",
	)
}

func readCommonQueryParams(c *gin.Context) (commonQueryParams, bool) {
	mode, ok := readMandatoryStringEnumQueryParameter(c, queryParamExpectationMode, allExpectationModes)
	if !ok {
		return commonQueryParams{}, false
	}
	timestampLowerBoundStr := c.Query(queryParamTimestampLowerBoundStr)
	timestampLowerBoundUnixMillis, err := strconv.ParseInt(timestampLowerBoundStr, 10, 64)
	if err != nil {
		c.JSON(400, ExpectationResult{
			Success: false,
			Description: fmt.Sprintf(
				"invalid or missing %s query param: \"%s\"",
				queryParamTimestampLowerBoundStr,
				timestampLowerBoundStr,
			),
		})
		return commonQueryParams{}, false
	}
	timestampLowerBound := time.UnixMilli(timestampLowerBoundUnixMillis)

	return commonQueryParams{
		mode:                expectationMode(mode),
		runtime:             c.Query(queryParamRuntime),
		runtimeWorkloadName: c.Query(queryParamRuntimeWorkloadName),
		workloadType:        c.Query(queryParamWorkloadType),
		timestampLowerBound: timestampLowerBound,
		clusterName:         c.Query(queryParamClusterName),
		operatorNamespace:   c.Query(queryParamOperatorNamespace),
	}, true
}

func readBooleanQueryParameter(c *gin.Context, queryParam string) (bool, bool) {
	value := false
	valueStr := c.Query(queryParam)
	if valueStr != "" {
		var err error
		value, err = strconv.ParseBool(valueStr)
		if err != nil {
			c.JSON(400, ExpectationResult{
				Success: false,
				Description: fmt.Sprintf(
					"invalid value for query param %s: \"%s\", expecting \"true\" or \"false\"",
					queryParam,
					valueStr,
				),
			})
			return false, false
		}
	}
	return value, true
}

func readMandatoryStringEnumQueryParameter(c *gin.Context, queryParam string, validValues []string) (string, bool) {
	value := c.Query(queryParam)
	if value == "" {
		c.JSON(400, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("missing %s query param", queryParam),
		})
		return "", false
	}
	if !slices.Contains(validValues, value) {
		c.JSON(400, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("invalid query param value for \"%s\": \"%s\"", queryParam, value),
		})
		return "", false
	}
	return value, true
}

func processMatchResults[R any, O any](
	c *gin.Context,
	commonParams commonQueryParams,
	allMatchResults *MatchResultList[R, O],
	signalTypeLabel string,
) {
	var expectationResult ExpectationResult
	switch commonParams.mode {
	case expectAtLeastOne:
		expectationResult = allMatchResults.expectAtLeastOneMatch(
			fmt.Sprintf(
				"%s %s: expected to find at least one matching %s",
				commonParams.runtime,
				commonParams.workloadType,
				signalTypeLabel,
			),
		)
	case expectExactlyOne:
		expectationResult = allMatchResults.expectExactlyOneMatch(
			fmt.Sprintf(
				"%s %s: expected to find exactly one matching %s",
				commonParams.runtime,
				commonParams.workloadType,
				signalTypeLabel,
			),
		)
	case expectNoMatches:
		expectationResult = allMatchResults.expectZeroMatches(
			fmt.Sprintf(
				"%s %s: expected to find no matching %ss",
				commonParams.runtime,
				commonParams.workloadType,
				signalTypeLabel,
			),
		)
	default:
		c.JSON(400, ExpectationResult{
			Success:     false,
			Description: fmt.Sprintf("unknown expectation mode: %s", string(commonParams.mode)),
		})
		return
	}

	if expectationResult.Success {
		c.JSON(200, expectationResult)
	} else {
		c.JSON(404, expectationResult)
	}
}
