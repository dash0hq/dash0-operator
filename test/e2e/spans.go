// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"

	. "github.com/onsi/gomega"
)

const (
	tracesJsonMaxLineLength = 1_048_576

	spanKindKey      = "span.kind"
	httpTargetAttrib = "http.target"
	httpRouteAttrib  = "http.route"
	urlQueryAttrib   = "url.query"
)

var (
	traceUnmarshaller = &ptrace.JSONUnmarshaler{}
)

func verifySpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
	timestampLowerBound time.Time,
	expectClusterName bool,
) {
	allMatchResults :=
		sendRequestAndFindMatchingSpans(
			g,
			runtime,
			workloadType,
			route,
			query,
			timestampLowerBound,
			true,
			expectClusterName,
		)
	allMatchResults.expectAtLeastOneMatch(
		g,
		fmt.Sprintf(
			"%s %s: expected to find at least one matching HTTP server span",
			runtime.runtimeTypeLabel,
			workloadType.workloadTypeString,
		),
	)
}

func verifyNoSpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
	timestampLowerBound time.Time,
) {
	allMatchResults :=
		sendRequestAndFindMatchingSpans(
			g,
			runtime,
			workloadType,
			route,
			query,
			timestampLowerBound,
			false,
			false,
		)
	allMatchResults.expectZeroMatches(
		g,
		fmt.Sprintf(
			"%s %s: expected to find no matching HTTP server span",
			runtime.runtimeTypeLabel,
			workloadType.workloadTypeString,
		),
	)
}

func sendRequestAndFindMatchingSpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
	timestampLowerBound time.Time,
	checkResourceAttributes bool,
	expectClusterName bool,
) MatchResultList[ptrace.ResourceSpans, ptrace.Span] {
	if !workloadType.isBatch {
		sendRequest(g, runtime, workloadType, route, query)
	}
	var resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans])
	if checkResourceAttributes {
		resourceMatchFn = workloadSpansResourceMatcher(runtime, workloadType, expectClusterName)
	}
	return fileHasMatchingSpan(
		g,
		resourceMatchFn,
		matchHttpServerSpanWithHttpTarget(route, query),
		timestampLowerBound,
	)
}

//nolint:all
func fileHasMatchingSpan(
	g Gomega,
	resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans]),
	spanMatchFn func(ptrace.Span, *ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span]),
	timestampLowerBound time.Time,
) MatchResultList[ptrace.ResourceSpans, ptrace.Span] {
	fileHandle, err := os.Open("test-resources/e2e/volumes/otlp-sink/traces.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, tracesJsonMaxLineLength), tracesJsonMaxLineLength)

	matchResults := newMatchResultList[ptrace.ResourceSpans, ptrace.Span]()

	// read file line by line
	for scanner.Scan() {
		resourceSpanBytes := scanner.Bytes()
		traces, err := traceUnmarshaller.UnmarshalTraces(resourceSpanBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		hasMatchingSpans(
			traces,
			resourceMatchFn,
			spanMatchFn,
			timestampLowerBound,
			&matchResults,
		)
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return matchResults
}

//nolint:all
func hasMatchingSpans(
	traces ptrace.Traces,
	resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans]),
	spanMatchFn func(ptrace.Span, *ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span]),
	timestampLowerBound time.Time,
	allMatchResults *MatchResultList[ptrace.ResourceSpans, ptrace.Span],
) {
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpan := traces.ResourceSpans().At(i)
		resourceMatchResult := newResourceMatchResult(resourceSpan)
		if resourceMatchFn != nil {
			resourceMatchFn(resourceSpan, &resourceMatchResult)
		}

		for j := 0; j < resourceSpan.ScopeSpans().Len(); j++ {
			scopeSpan := resourceSpan.ScopeSpans().At(j)
			for k := 0; k < scopeSpan.Spans().Len(); k++ {
				span := scopeSpan.Spans().At(k)
				if !span.StartTimestamp().AsTime().After(timestampLowerBound) {
					// This span is too old, it is probably from a previously running test case, ignore it.
					continue
				}
				spanMatchResult := newObjectMatchResult[ptrace.ResourceSpans, ptrace.Span](
					span.Name(),
					resourceSpan,
					resourceMatchResult,
					span,
				)
				spanMatchFn(span, &spanMatchResult)
				allMatchResults.addResultForObject(spanMatchResult)
			}
		}
	}
}

//nolint:all
func workloadSpansResourceMatcher(runtime runtimeType, workloadType workloadType, expectClusterName bool) func(
	ptrace.ResourceSpans,
	*ResourceMatchResult[ptrace.ResourceSpans],
) {
	return func(resourceSpans ptrace.ResourceSpans, matchResult *ResourceMatchResult[ptrace.ResourceSpans]) {
		resourceAttributes := resourceSpans.Resource().Attributes()

		if expectClusterName {
			verifyResourceAttributeEquals(
				resourceAttributes,
				string(semconv.K8SClusterNameKey),
				e2eKubernetesContext,
				matchResult,
			)
		}

		// Note: On kind clusters, the workload type attribute (k8s.deployment.name) etc. is often missing. This needs
		// to be investigated more.
		workloadAttribute := fmt.Sprintf("k8s.%s.name", workloadType.workloadTypeString)
		if workloadType.workloadTypeString == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			matchResult.addSkippedAssertion(workloadAttribute, "not checked, there is no k8s.replicaset.name attribute")
		} else {
			verifyResourceAttributeEquals(
				resourceAttributes,
				workloadAttribute,
				workloadName(runtime, workloadType),
				matchResult,
			)
		}

		expectedPodName := workloadName(runtime, workloadType)
		if workloadType.workloadTypeString == "pod" {
			verifyResourceAttributeEquals(
				resourceAttributes,
				string(semconv.K8SPodNameKey),
				expectedPodName,
				matchResult,
			)
		} else {
			verifyResourceAttributeStartsWith(
				resourceAttributes,
				string(semconv.K8SPodNameKey),
				fmt.Sprintf("%s-", expectedPodName),
				matchResult,
			)
		}

		verifyResourceAttributeStartsWith(
			resourceAttributes,
			"k8s.pod.label.test.label/key",
			"label-value",
			matchResult,
		)
		verifyResourceAttributeStartsWith(
			resourceAttributes,
			"k8s.pod.annotation.test.annotation/key",
			"annotation value",
			matchResult,
		)
	}
}

func matchHttpServerSpanWithHttpTarget(expectedRoute string, expectedQuery string) func(
	ptrace.Span,
	*ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span],
) {
	return func(span ptrace.Span, matchResult *ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span]) {
		if span.Kind() == ptrace.SpanKindServer {
			matchResult.addPassedAssertion(spanKindKey)
		} else {
			matchResult.addFailedAssertion(
				spanKindKey,
				fmt.Sprintf("expected a server span, this span has kind \"%s\"", span.Kind().String()),
			)
		}

		var expectedTarget string
		if expectedQuery != "" {
			expectedTarget = fmt.Sprintf("%s?%s", expectedRoute, expectedQuery)
		} else {
			expectedTarget = expectedRoute
		}

		target, hasTarget := span.Attributes().Get(httpTargetAttrib)
		route, hasRoute := span.Attributes().Get(httpRouteAttrib)
		query, hasQuery := span.Attributes().Get(urlQueryAttrib)
		if hasTarget {
			if target.Str() == expectedTarget {
				matchResult.addPassedAssertion(httpTargetAttrib)
			} else {
				matchResult.addFailedAssertion(
					httpTargetAttrib,
					fmt.Sprintf("expected %s but it was %s", expectedTarget, target.Str()),
				)
			}
		} else if hasRoute && hasQuery {
			if route.Str() == expectedRoute &&
				(query.Str() == expectedQuery ||
					// .NET instrumentation includes the "?" in the query attribute
					query.Str() == "?"+expectedQuery) {
				matchResult.addPassedAssertion(httpRouteAttrib + " and " + urlQueryAttrib)
			} else {
				matchResult.addFailedAssertion(
					httpRouteAttrib+" and "+urlQueryAttrib,
					fmt.Sprintf("expected %s & %s but it was %s & %s", expectedRoute, expectedQuery, route.Str(), query.Str()),
				)
			}
		} else {
			matchResult.addFailedAssertion(
				httpTargetAttrib+" or ("+httpRouteAttrib+" and "+urlQueryAttrib+")",
				fmt.Sprintf(
					"expected %s or (%s and %s) but the span had no such atttribute",
					expectedTarget,
					expectedRoute,
					expectedQuery,
				),
			)
		}
	}
}
