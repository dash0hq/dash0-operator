// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
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

func verifyAtLeastOneKubernetesObjectRecord(
	g Gomega,
	timestampLowerBound time.Time,
	objectResourceName string,
	objectNameContains string,
	logBodyContains string,
	logBodyDoesNotContain string,
) {
	askTelemetryMatcherForMatchingObjects(
		g,
		shared.ExpectAtLeastOne,
		timestampLowerBound,
		objectResourceName,
		objectNameContains,
		logBodyContains,
		logBodyDoesNotContain,
	)
}

// askTelemetryMatcherForMatchingObjects executes an HTTP request to query telemetry-matcher to search through the
// telemetry captured in otlp-sink for a log record representing a collected Kubernetes object manifest matching the
// given criteria.
func askTelemetryMatcherForMatchingObjects(
	g Gomega,
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	objectResourceName string,
	objectNameContains string,
	logBodyContains string,
	logBodyDoesNotContain string,
) {
	requestUrl := compileTelemetryMatcherUrlForObjects(
		expectationMode,
		timestampLowerBound,
		objectResourceName,
		objectNameContains,
		logBodyContains,
		logBodyDoesNotContain,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForObjects(
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	objectResourceName string,
	objectNameContains string,
	logBodyContains string,
	logBodyDoesNotContain string,
) string {
	baseUrl := fmt.Sprintf("%s/matching-objects", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	params.Add(shared.QueryParamClusterName, e2eKubernetesContext)
	params.Add(shared.QueryParamNamespace, applicationUnderTestNamespace)
	params.Add(shared.QueryParamOperatorNamespace, operatorNamespace)
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixNano(), 10))
	params.Add(shared.QueryParamObjectResourceName, objectResourceName)
	params.Add(shared.QueryParamObjectNameContains, objectNameContains)
	params.Add(shared.QueryParamLogBodyContains, logBodyContains)
	params.Add(shared.QueryParamLogBodyDoesNotContain, logBodyDoesNotContain)
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
