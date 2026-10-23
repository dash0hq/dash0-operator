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

func verifyExactlyOneWorkloadKubernetesEvent(
	g Gomega,
	timestampLowerBound time.Time,
	logBodyContains string,
	eventReason string,
	eventNameContains string,
) {
	askTelemetryMatcherForMatchingEvents(
		g,
		shared.ExpectExactlyOne,
		timestampLowerBound,
		logBodyContains,
		eventReason,
		eventNameContains,
		"",
		"",
	)
}

// verifyAtLeastOneKubernetesEventForInvolvedObject checks that at least one event has been collected for the given
// involved object, which is identified by the given resource attribute (e.g. k8s.hpa.name) and its expected value.
func verifyAtLeastOneKubernetesEventForInvolvedObject(
	g Gomega,
	timestampLowerBound time.Time,
	eventReason string,
	involvedObjectNameAttribute string,
	involvedObjectName string,
) {
	askTelemetryMatcherForMatchingEvents(
		g,
		shared.ExpectAtLeastOne,
		timestampLowerBound,
		"",
		eventReason,
		"",
		involvedObjectNameAttribute,
		involvedObjectName,
	)
}

// askTelemetryMatcherForMatchingEvents executes an HTTP request to query telemetry-matcher to search through the
// telemetry captured in otlp-sink for an event/log record matching the given criteria.
func askTelemetryMatcherForMatchingEvents(
	g Gomega,
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	logBodyContains string,
	eventReason string,
	eventNameContains string,
	involvedObjectNameAttribute string,
	involvedObjectName string,
) {
	requestUrl := compileTelemetryMatcherUrlForEvents(
		expectationMode,
		timestampLowerBound,
		logBodyContains,
		eventReason,
		eventNameContains,
		involvedObjectNameAttribute,
		involvedObjectName,
	)
	executeTelemetryMatcherRequest(g, requestUrl)
}

func compileTelemetryMatcherUrlForEvents(
	expectationMode shared.ExpectationMode,
	timestampLowerBound time.Time,
	logBodyContains string,
	eventReason string,
	eventNameContains string,
	involvedObjectNameAttribute string,
	involvedObjectName string,
) string {
	baseUrl := fmt.Sprintf("%s/matching-events", telemetryMatcherBaseUrl)
	params := url.Values{}
	params.Add(shared.QueryParamExpectationMode, string(expectationMode))
	params.Add(shared.QueryParamClusterName, e2eKubernetesContext)
	params.Add(shared.QueryParamNamespace, applicationUnderTestNamespace)
	params.Add(shared.QueryParamOperatorNamespace, operatorNamespace)
	params.Add(shared.QueryParamTimestampLowerBoundStr, strconv.FormatInt(timestampLowerBound.UnixNano(), 10))
	params.Add(shared.QueryParamLogBodyContains, logBodyContains)
	params.Add(shared.QueryParamEventReason, eventReason)
	params.Add(shared.QueryParamEventNameContains, eventNameContains)
	if involvedObjectNameAttribute != "" {
		params.Add(shared.QueryParamEventInvolvedObjectNameAttribute, involvedObjectNameAttribute)
		params.Add(shared.QueryParamEventInvolvedObjectName, involvedObjectName)
	}
	requestUrl := baseUrl + "?" + params.Encode()
	return requestUrl
}
