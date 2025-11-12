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
)

func verifyAtLeastOneSelfMonitoringLogRecord(
	g Gomega,
	resourceMatcherMode logResourceMatcherMode,
	serviceVersion string,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) {
	askTelemetryMatcherForMatchingLogRecords(
		g,
		expectAtLeastOne,
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
		expectExactlyOne,
		logResourceMatcherWorkload,
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
	expectationMode expectationMode,
	resourceMatcherMode logResourceMatcherMode,
	runtime *runtimeType,
	workloadType *workloadType,
	serviceVersion string,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) {
	updateTelemetryMatcherUrlForKind()
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
	expectationMode expectationMode,
	resourceMatcherMode logResourceMatcherMode,
	runtime *runtimeType,
	workloadType *workloadType,
	serviceVersion string,
	timestampLowerBound time.Time,
	logBodyEquals string,
	logBodyContains string,
) string {
	baseUrl := fmt.Sprintf("%s/matching-logs", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(queryParamExpectationMode, string(expectationMode))
	params.Add(queryParamLogsResourceMatcherMode, string(resourceMatcherMode))
	if runtime != nil {
		params.Add(queryParamRuntime, runtime.runtimeTypeLabel)
		params.Add(queryParamRuntimeWorkloadName, runtime.workloadName)
	}
	if workloadType != nil {
		params.Add(queryParamWorkloadType, workloadType.workloadTypeString)
	}
	// Note: Using the name of the Kubernetes context as the cluster name match parameter works because we set this
	// as the clusterName setting for the operator configuration resource when doing helm install or creating the
	// operator configuration resource.
	params.Add(queryParamClusterName, e2eKubernetesContext)
	params.Add(queryParamOperatorNamespace, operatorNamespace)
	if serviceVersion != "" {
		params.Add(queryParamServiceVersion, serviceVersion)
	}
	params.Add(queryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixMilli(), 10))
	if logBodyEquals != "" {
		params.Add(queryParamLogBodyEquals, logBodyEquals)
	} else if logBodyContains != "" {
		params.Add(queryParamLogBodyContains, logBodyContains)
	}
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
