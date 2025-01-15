// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"

	. "github.com/onsi/gomega"
)

const (
	tracesJsonMaxLineLength = 1_048_576

	clusterNameKey = "k8s.cluster.name"
	podNameKey     = "k8s.pod.name"

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
	expectClusterName bool,
) {
	allMatchResults :=
		sendRequestAndFindMatchingSpans(
			g,
			runtime,
			workloadType,
			route,
			query,
			nil,
			true,
			expectClusterName,
		)
	allMatchResults.expectAtLeastOneMatch(
		g,
		fmt.Sprintf("%s: expected to find at least one matching HTTP server span", workloadType.workloadTypeString),
	)
}

func verifyNoSpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
) {
	timestampLowerBound := time.Now()
	allMatchResults :=
		sendRequestAndFindMatchingSpans(
			g,
			runtime,
			workloadType,
			route,
			query,
			&timestampLowerBound,
			false,
			false,
		)
	allMatchResults.expectZeroMatches(
		g,
		fmt.Sprintf("%s: expected to find no matching HTTP server span", workloadType.workloadTypeString),
	)
}

func sendRequestAndFindMatchingSpans(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	route string,
	query string,
	timestampLowerBound *time.Time,
	checkResourceAttributes bool,
	expectClusterName bool,
) MatchResultList[ptrace.ResourceSpans, ptrace.Span] {
	if !workloadType.isBatch {
		sendRequest(g, runtime, workloadType, route, query)
	}
	var resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans])
	if checkResourceAttributes {
		resourceMatchFn = resourceSpansHaveExpectedResourceAttributes(runtime, workloadType, expectClusterName)
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
	timestampLowerBound *time.Time,
) MatchResultList[ptrace.ResourceSpans, ptrace.Span] {
	fileHandle, err := os.Open("test-resources/e2e-test-volumes/otlp-sink/traces.jsonl")
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
	timestampLowerBound *time.Time,
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
				spanMatchResult := newObjectMatchResult[ptrace.ResourceSpans, ptrace.Span](
					resourceSpan,
					resourceMatchResult,
					span,
				)
				if timestampLowerBound != nil {
					if span.StartTimestamp().AsTime().After(*timestampLowerBound) {
						spanMatchResult.addPassedAssertion("timestamp")
					} else {
						spanMatchResult.addFailedAssertion(
							"timestamp",
							fmt.Sprintf(
								"expected timestamp after %s but it was %s",
								timestampLowerBound.String(),
								span.StartTimestamp().AsTime().String(),
							),
						)
					}
				} else {
					spanMatchResult.addSkippedAssertion("timestamp", "no lower bound provided")
				}
				spanMatchFn(span, &spanMatchResult)
				allMatchResults.addResultForObject(spanMatchResult)
			}
		}
	}
}

//nolint:all
func resourceSpansHaveExpectedResourceAttributes(runtime runtimeType, workloadType workloadType, expectClusterName bool) func(
	ptrace.ResourceSpans,
	*ResourceMatchResult[ptrace.ResourceSpans],
) {
	return func(resourceSpans ptrace.ResourceSpans, matchResult *ResourceMatchResult[ptrace.ResourceSpans]) {
		attributes := resourceSpans.Resource().Attributes()

		if expectClusterName {
			expectedClusterName := "e2e-test-cluster"
			actualClusterName, hasClusterNameAttribute := attributes.Get(clusterNameKey)
			if hasClusterNameAttribute {
				if actualClusterName.Str() == expectedClusterName {
					matchResult.addPassedAssertion(podNameKey)
				} else {
					matchResult.addFailedAssertion(podNameKey, fmt.Sprintf("expected %s but it was %s", expectedClusterName, actualClusterName.Str()))
				}
			} else {
				matchResult.addFailedAssertion(clusterNameKey, fmt.Sprintf("expected %s but the span has no such attribute", expectedClusterName))
			}
		}

		// Note: On kind clusters, the workload type attribute (k8s.deployment.name) etc. is often missing. This needs
		// to be investigated more.
		if workloadType.workloadTypeString == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			matchResult.addSkippedAssertion("k8s.replicaset.name", "not checked, there is no k8s.replicaset.name attribute")
		} else {
			workloadNameKey := fmt.Sprintf("k8s.%s.name", workloadType.workloadTypeString)
			expectedWorkloadName := workloadName(runtime, workloadType)
			actualWorkloadName, hasWorkloadName := attributes.Get(workloadNameKey)
			if !hasWorkloadName {
				matchResult.addFailedAssertion(workloadNameKey, fmt.Sprintf("expected %s but the span has no such attribute", expectedWorkloadName))
			} else if actualWorkloadName.Str() == expectedWorkloadName {
				matchResult.addPassedAssertion(workloadNameKey)
			} else {
				matchResult.addFailedAssertion(workloadNameKey, fmt.Sprintf("expected %s but it was %s", expectedWorkloadName, actualWorkloadName.Str()))
			}
		}

		expectedPodName := workloadName(runtime, workloadType)
		expectedPodPrefix := fmt.Sprintf("%s-", expectedPodName)
		actualPodName, hasPodAttribute := attributes.Get(podNameKey)
		if hasPodAttribute {
			if workloadType.workloadTypeString == "pod" {
				if actualPodName.Str() == expectedPodName {
					matchResult.addPassedAssertion(podNameKey)
				} else {
					matchResult.addFailedAssertion(podNameKey, fmt.Sprintf("expected %s but it was %s", expectedPodName, actualPodName.Str()))
				}
			} else {
				if strings.Contains(actualPodName.Str(), expectedPodPrefix) {
					matchResult.addPassedAssertion(podNameKey)
				} else {
					matchResult.addFailedAssertion(podNameKey, fmt.Sprintf("expected to contain %s but it was %s", expectedPodName, actualPodName.Str()))
				}
			}
		} else {
			matchResult.addFailedAssertion(podNameKey, fmt.Sprintf("expected %s but the span has no such attribute", expectedPodName))
		}
	}
}

func matchHttpServerSpanWithHttpTarget(expectedRoute string, expectedQuery string) func(
	ptrace.Span,
	*ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span],
) {
	return func(span ptrace.Span, matchResult *ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span]) {
		if span.Kind() == ptrace.SpanKindServer {
			matchResult.addPassedAssertion("span.kind")
		} else {
			matchResult.addFailedAssertion(
				"span.kind",
				fmt.Sprintf("expected a server span, this span has kind \"%s\"", span.Kind().String()),
			)
		}

		expectedTarget := fmt.Sprintf("%s?%s", expectedRoute, expectedQuery)
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
			if route.Str() == expectedRoute && query.Str() == expectedQuery {
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
