// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
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

//nolint:dupl
func readFileAndGetMatchingSpans(
	tracesJsonlFilename string,
	resourceMatchFn func(ptrace.ResourceSpans, *ResourceMatchResult[ptrace.ResourceSpans]),
	spanMatchFn func(ptrace.Span, *ObjectMatchResult[ptrace.ResourceSpans, ptrace.Span]),
	timestampLowerBound time.Time,
) (*MatchResultList[ptrace.ResourceSpans, ptrace.Span], error) {
	fileHandle, err := os.Open(tracesJsonlFilename)
	if err != nil {
		return nil, fmt.Errorf("cannot open file %s: %v", tracesJsonlFilename, err)
	}
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, tracesJsonMaxLineLength), tracesJsonMaxLineLength)

	matchResults := newMatchResultList[ptrace.ResourceSpans, ptrace.Span]()

	// read file line by line, each line can contain multiple resources spans, each with multiple scope spans and actual
	// spans
	for scanner.Scan() {
		resourceSpanBytes := scanner.Bytes()
		traces, err := traceUnmarshaller.UnmarshalTraces(resourceSpanBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		// add matching spans from this line in the file to matchResults
		extractMatchingSpans(
			traces,
			resourceMatchFn,
			spanMatchFn,
			timestampLowerBound,
			&matchResults,
		)
	}

	if scanner.Err() != nil {
		return nil, fmt.Errorf("error while scanning file %s: %v", tracesJsonlFilename, err)
	}

	return &matchResults, nil
}

func extractMatchingSpans(
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
				spanMatchResult := newObjectMatchResult[ptrace.ResourceSpans, ptrace.Span](
					span.Name(),
					resourceSpan,
					resourceMatchResult,
					span,
				)
				spanMatchFn(span, &spanMatchResult)
				if span.StartTimestamp().AsTime().Before(timestampLowerBound) {
					if spanMatchResult.isMatch() {
						log.Printf(
							"Ignoring matching span because of timestamp: lower bound: %s (%d) vs. span: %s (%d)",
							timestampLowerBound.String(),
							timestampLowerBound.UnixNano(),
							span.StartTimestamp().String(),
							span.StartTimestamp().AsTime().UnixNano(),
						)
					}
					// This span is too old, it is probably from a previously running test case, ignore it.
					continue
				}
				allMatchResults.addResultForObject(spanMatchResult)
			}
		}
	}
}

func workloadSpansResourceMatcher(
	runtimeWorkloadName string,
	workloadType string,
	clusterName string,
) func(
	ptrace.ResourceSpans,
	*ResourceMatchResult[ptrace.ResourceSpans],
) {
	return func(resourceSpans ptrace.ResourceSpans, matchResult *ResourceMatchResult[ptrace.ResourceSpans]) {
		resourceAttributes := resourceSpans.Resource().Attributes()

		if clusterName != "" {
			verifyResourceAttributeEquals(
				resourceAttributes,
				string(semconv.K8SClusterNameKey),
				clusterName,
				matchResult,
			)
		}

		// Note: On kind clusters, the workload type attribute (k8s.deployment.name) etc. is often missing. This needs
		// to be investigated more.
		workloadAttribute := fmt.Sprintf("k8s.%s.name", workloadType)
		if workloadType == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			matchResult.addSkippedAssertion(workloadAttribute, "not checked, there is no k8s.replicaset.name attribute")
		} else {
			verifyResourceAttributeEquals(
				resourceAttributes,
				workloadAttribute,
				workloadName(runtimeWorkloadName, workloadType),
				matchResult,
			)
		}

		expectedPodName := workloadName(runtimeWorkloadName, workloadType)
		if workloadType == "pod" {
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

func httpServerSpanMatcher(
	expectedRoute string,
	expectedQuery string,
	expectedTarget string,
) func(
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

		// Depending on the OTel SDK (Node.js, .NET, JVM, ...), the resulting HTTP server spans will look slightly
		// different, with respect to which http-related span attributes are set (e.g. either http.target or
		// http.route and url.query). The following logic makes sure we `accommodate` these differences.
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
					fmt.Sprintf(
						"expected %s & %s but it was %s & %s",
						expectedRoute,
						expectedQuery,
						route.Str(),
						query.Str(),
					),
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
