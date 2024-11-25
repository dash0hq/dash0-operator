// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"

	. "github.com/onsi/gomega"
)

const (
	tracesJsonMaxLineLength = 1_048_576
)

var (
	traceUnmarshaller = &ptrace.JSONUnmarshaler{}
)

func verifySpans(g Gomega, isBatch bool, workloadType string, port int, httpPathWithQuery string) {
	allMatchResults :=
		sendRequestAndFindMatchingSpans(
			g,
			isBatch,
			workloadType,
			port,
			httpPathWithQuery,
			nil,
			true,
		)
	allMatchResults.expectAtLeastOneMatch(
		g,
		fmt.Sprintf("%s: expected to find at least one matching HTTP server span", workloadType),
	)
}

func verifyNoSpans(g Gomega, isBatch bool, workloadType string, port int, httpPathWithQuery string) {
	timestampLowerBound := time.Now()
	allMatchResults :=
		sendRequestAndFindMatchingSpans(
			g,
			isBatch,
			workloadType,
			port,
			httpPathWithQuery,
			&timestampLowerBound,
			false,
		)
	allMatchResults.expectZeroMatches(
		g,
		fmt.Sprintf("%s: expected to find no matching HTTP server span", workloadType),
	)
}

func sendRequestAndFindMatchingSpans(
	g Gomega,
	isBatch bool,
	workloadType string,
	port int,
	httpPathWithQuery string,
	timestampLowerBound *time.Time,
	checkResourceAttributes bool,
) MatchResultList[ptrace.ResourceSpans, ptrace.Span] {
	if !isBatch {
		sendRequest(g, port, httpPathWithQuery)
	}
	var resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans])
	if checkResourceAttributes {
		resourceMatchFn = resourceSpansHaveExpectedResourceAttributes(workloadType)
	}
	return fileHasMatchingSpan(
		g,
		resourceMatchFn,
		matchHttpServerSpanWithHttpTarget(httpPathWithQuery),
		timestampLowerBound,
	)
}

func sendRequest(g Gomega, port int, httpPathWithQuery string) {
	url := fmt.Sprintf("http://localhost:%d%s", port, httpPathWithQuery)
	client := http.Client{
		Timeout: 500 * time.Millisecond,
	}
	response, err := client.Get(url)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = response.Body.Close()
	}()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		e2ePrint("could not read http response from %s: %s\n", url, err.Error())
	}
	g.Expect(err).NotTo(HaveOccurred())
	status := response.StatusCode
	g.Expect(
		string(responseBody)).To(
		ContainSubstring("We make Observability easy for every developer."),
		fmt.Sprintf("unexpected response body for workload type %s at %s, HTTP %d", workloadType, url, status),
	)
	g.Expect(status).To(
		Equal(200),
		fmt.Sprintf("unexpected status workload type %s at %s", workloadType, url),
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
func resourceSpansHaveExpectedResourceAttributes(workloadType string) func(
	ptrace.ResourceSpans,
	*ResourceMatchResult[ptrace.ResourceSpans],
) {
	return func(resourceSpans ptrace.ResourceSpans, matchResult *ResourceMatchResult[ptrace.ResourceSpans]) {
		attributes := resourceSpans.Resource().Attributes()

		if workloadType == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			matchResult.addSkippedAssertion("k8s.replicaset.name", "not checked, there is no k8s.replicaset.name attribute")
		} else {
			workloadNameKey := fmt.Sprintf("k8s.%s.name", workloadType)
			expectedWorkloadName := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
			actualWorkloadName, hasWorkloadName := attributes.Get(workloadNameKey)
			if !hasWorkloadName {
				matchResult.addFailedAssertion(workloadNameKey, fmt.Sprintf("expected %s but the span has no such attribute", expectedWorkloadName))
			} else if actualWorkloadName.Str() == expectedWorkloadName {
				matchResult.addPassedAssertion(workloadNameKey)
			} else {
				matchResult.addFailedAssertion(workloadNameKey, fmt.Sprintf("expected %s but it was %s", expectedWorkloadName, actualWorkloadName.Str()))
			}
		}

		podNameKey := "k8s.pod.name"
		expectedPodName := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
		expectedPodPrefix := fmt.Sprintf("%s-", expectedPodName)
		actualPodName, hasPodAttribute := attributes.Get(podNameKey)
		if hasPodAttribute {
			if workloadType == "pod" {
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

func matchHttpServerSpanWithHttpTarget(expectedTarget string) func(
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
		httpTargetAttrib := "http.target"
		target, hasTarget := span.Attributes().Get(httpTargetAttrib)
		if hasTarget {
			if target.Str() == expectedTarget {
				matchResult.addPassedAssertion(httpTargetAttrib)
			} else {
				matchResult.addFailedAssertion(
					httpTargetAttrib,
					fmt.Sprintf("expected %s but it was %s", expectedTarget, target.Str()),
				)
			}
		} else {
			matchResult.addFailedAssertion(
				httpTargetAttrib,
				fmt.Sprintf("expected %s but the span had no such atttribute", expectedTarget),
			)
		}
	}
}
