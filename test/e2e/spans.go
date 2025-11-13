// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/test/e2e/pkg/shared"
)

// verifySpans is meant to be polled in a gomega Eventually loop; it will send an HTTP request to the workload each time
// and then check whether a matching span has been produced. For workload types that are deployed without a matching
// service (like cronjob or job), the HTTP request to trigger the span is omitted, for these workload types we rely on
// the workload to send HTTP requests to itself.
//
// Since we send an HTTP request each time verifySpans is called, the workload might in fact produce a number of spans;
// the span that is found by the telemetry-matcher might not be from the HTTP request from the same invocation. However,
// since the query always includes a unique test ID, it is guaranteed that the span has been triggered by the same test
// case.
func verifySpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
	timestampLowerBound time.Time,
	expectClusterName bool,
) {
	if !workloadType.isBatch {
		sendRequest(g, runtime, workloadType, route, query)
	}
	askTelemetryMatcherForMatchingSpans(
		g,
		shared.ExpectAtLeastOne,
		runtime,
		workloadType,
		true,
		expectClusterName,
		timestampLowerBound,
		route,
		query,
		"",
	)
}

// verifyNoSpans is meant to be polled in a gomega Consistently loop; it will send an HTTP request to the workload each
// time and then verify that no spans have been produced. For workload types that are deployed without a matching
// service (like cronjob or job), the HTTP request to trigger the span is omitted, for these workload types we rely on
// the workload to send HTTP requests to itself.
func verifyNoSpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
	timestampLowerBound time.Time,
) {
	if !workloadType.isBatch {
		sendRequest(g, runtime, workloadType, route, query)
	}
	askTelemetryMatcherForMatchingSpans(
		g,
		shared.ExpectNoMatches,
		runtime,
		workloadType,
		false,
		false,
		timestampLowerBound,
		route,
		query,
		"",
	)
}

// askTelemetryMatcherForMatchingSpans executes an HTTP request to query telemetry-matcher to search through the
// telemetry captured in otlp-sink for a span matching the given criteria.
func askTelemetryMatcherForMatchingSpans(
	g Gomega,
	expectationMode shared.ExpectationMode,
	runtime runtimeType,
	workloadType workloadType,
	checkResourceAttributes bool,
	expectClusterName bool,
	timestampLowerBound time.Time,
	route string,
	query string,
	target string,
) {
	updateTelemetryMatcherUrlForKind()
	requestUrl := compileTelemetryMatcherUrlForSpans(
		expectationMode,
		runtime,
		workloadType,
		checkResourceAttributes,
		expectClusterName,
		timestampLowerBound,
		route,
		query,
		target,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForSpans(
	expectationMode shared.ExpectationMode,
	runtime runtimeType,
	workloadType workloadType,
	checkResourceAttributes bool,
	expectClusterName bool,
	timestampLowerBound time.Time,
	route string,
	query string,
	target string,
) string {
	baseUrl := fmt.Sprintf("%s/matching-spans", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	// e.g. "Node.js", "JVM", ".NET", ...
	params.Add(shared.QueryParamRuntime, runtime.runtimeTypeLabel)
	// e.g. "dash0-operator-nodejs-20-express-test", "dash0-operator-jvm-spring-boot-test", ...
	params.Add(shared.QueryParamRuntimeWorkloadName, runtime.workloadName)
	// e.g. "deployment", "daemonset"
	params.Add(shared.QueryParamWorkloadType, workloadType.workloadTypeString)
	params.Add(shared.QueryParamRoute, route)
	params.Add(shared.QueryParamQuery, query)
	if target == "" {
		// Usually, the expected target can be derived from expectedRoute and expectedQuery, which is why most test
		// cases leave it empty so that this method derives it; but in some special test scenarios we let the test case
		// specify it explicitly, for example for "truncates attributes when the transform is active".
		if query != "" {
			params.Add(shared.QueryParamTarget, fmt.Sprintf("%s?%s", route, query))
		} else {
			params.Add(shared.QueryParamTarget, route)
		}
	} else {
		params.Add(shared.QueryParamTarget, target)
	}
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixMilli(), 10))
	params.Add(shared.QueryParamCheckResourceAttributes, strconv.FormatBool(checkResourceAttributes))
	if expectClusterName {
		// Note: Using the name of the Kubernetes context as the cluster name match parameter works because we set this
		// as the clusterName setting for the operator configuration resource when doing helm install or creating the
		// operator configuration resource.
		params.Add(shared.QueryParamClusterName, e2eKubernetesContext)
	}
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
