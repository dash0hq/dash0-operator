// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"k8s.io/utils/ptr"

	. "github.com/onsi/gomega"

	"github.com/dash0hq/dash0-operator/test/e2e/pkg/shared"
)

func verifyAtLeastOneSelfMonitoringLogRecord(
	g Gomega,
	resourceMatcherMode shared.LogResourceMatcherMode,
	serviceVersion string,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) {
	askTelemetryMatcherForMatchingLogRecords(
		g,
		shared.ExpectAtLeastOne,
		resourceMatcherMode,
		nil,
		nil,
		serviceVersion,
		timestampLowerBound,
		logBodyEquals,
		logBodyContains,
	)
}

func verifyExactlyOneWorkloadLogRecord(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) {
	askTelemetryMatcherForMatchingLogRecords(
		g,
		shared.ExpectExactlyOne,
		shared.LogResourceMatcherWorkload,
		ptr.To(runtime),
		ptr.To(workloadType),
		"",
		timestampLowerBound,
		logBodyEquals,
		logBodyContains,
	)
}

// askTelemetryMatcherForMatchingLogRecords executes an HTTP request to query telemetry-matcher to search through the
// telemetry captured in otlp-sink for a log record matching the given criteria.
func askTelemetryMatcherForMatchingLogRecords(
	g Gomega,
	expectationMode shared.ExpectationMode,
	resourceMatcherMode shared.LogResourceMatcherMode,
	runtime *runtimeType,
	workloadType *workloadType,
	serviceVersion string,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) {
	if !isKindCluster() {
		// TODO Get rid of isKindCluster() here, either make the ingress port configurable or make the
		// ingress-nginx-controller use port 8080 in between Docker Desktop as well.
		telemetryMatcherBaseUrl = fmt.Sprintf("http://localhost/telemetry-matcher")
	}
	requestUrl := compileTelemetryMatcherUrlForLogRecords(
		expectationMode,
		resourceMatcherMode,
		runtime,
		workloadType,
		serviceVersion,
		timestampLowerBound,
		logBodyEquals,
		logBodyContains,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForLogRecords(
	expectationMode shared.ExpectationMode,
	resourceMatcherMode shared.LogResourceMatcherMode,
	runtime *runtimeType,
	workloadType *workloadType,
	serviceVersion string,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) string {
	baseUrl := fmt.Sprintf("%s/matching-logs", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	params.Add(shared.QueryParamLogsResourceMatcherMode, string(resourceMatcherMode))
	if runtime != nil {
		params.Add(shared.QueryParamRuntime, runtime.runtimeTypeLabel)
		params.Add(shared.QueryParamRuntimeWorkloadName, runtime.workloadName)
	}
	if workloadType != nil {
		params.Add(shared.QueryParamWorkloadType, workloadType.workloadTypeString)
	}
	// Note: Using the name of the Kubernetes context as the cluster name match parameter works because we set this
	// as the clusterName setting for the operator configuration resource when doing helm install or creating the
	// operator configuration resource.
	params.Add(shared.QueryParamClusterName, e2eKubernetesContext)
	params.Add(shared.QueryParamOperatorNamespace, operatorNamespace)
	if serviceVersion != "" {
		params.Add(shared.QueryParamServiceVersion, serviceVersion)
	}
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixMilli(), 10))
	if logBodyEquals != "" {
		params.Add(shared.QueryParamLogBodyEquals, logBodyEquals)
	} else if logBodyContains != "" {
		params.Add(shared.QueryParamLogBodyContains, logBodyContains)
	}
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
