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

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

const (
	tracesJsonMaxLineLength = 1_048_576
)

var (
	traceUnmarshaller = &ptrace.JSONUnmarshaler{}
)

func verifySpans(g Gomega, isBatch bool, workloadType string, port int, httpPathWithQuery string) {
	spansFound := sendRequestAndFindMatchingSpans(g, isBatch, workloadType, port, httpPathWithQuery, nil)
	g.Expect(spansFound).To(BeTrue(),
		fmt.Sprintf("%s: expected to find at least one matching HTTP server span", workloadType))
}

func verifyNoSpans(isBatch bool, workloadType string, port int, httpPathWithQuery string) {
	timestampLowerBound := time.Now()
	spansFound := sendRequestAndFindMatchingSpans(
		Default,
		isBatch,
		"",
		port,
		httpPathWithQuery,
		&timestampLowerBound,
	)
	Expect(spansFound).To(BeFalse(), fmt.Sprintf("%s: expected to find no matching HTTP server span", workloadType))
}

func sendRequestAndFindMatchingSpans(
	g Gomega,
	isBatch bool,
	workloadType string,
	port int,
	httpPathWithQuery string,
	timestampLowerBound *time.Time,
) bool {
	if !isBatch {
		sendRequest(g, port, httpPathWithQuery)
	}
	var resourceMatchFn func(span ptrace.ResourceSpans) bool
	if workloadType != "" {
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
		fmt.Fprintf(GinkgoWriter, "could not read http response from %s: %s\n", url, err.Error())
	}
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(responseBody).To(ContainSubstring("We make Observability easy for every developer."))
}

//nolint:all
func fileHasMatchingSpan(
	g Gomega,
	resourceMatchFn func(span ptrace.ResourceSpans) bool,
	spanMatchFn func(span ptrace.Span) bool,
	timestampLowerBound *time.Time,
) bool {
	fileHandle, err := os.Open("test-resources/e2e-test-volumes/otlp-sink/traces.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, tracesJsonMaxLineLength), tracesJsonMaxLineLength)

	// read file line by line
	spansFound := false
	for scanner.Scan() {
		resourceSpanBytes := scanner.Bytes()
		traces, err := traceUnmarshaller.UnmarshalTraces(resourceSpanBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		if spansFound = hasMatchingSpans(
			traces,
			resourceMatchFn,
			spanMatchFn,
			timestampLowerBound,
		); spansFound {
			break
		}
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return spansFound
}

//nolint:all
func hasMatchingSpans(
	traces ptrace.Traces,
	resourceMatchFn func(span ptrace.ResourceSpans) bool,
	spanMatchFn func(span ptrace.Span) bool,
	timestampLowerBound *time.Time,
) bool {
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpan := traces.ResourceSpans().At(i)
		if resourceMatchFn != nil {
			if !resourceMatchFn(resourceSpan) {
				continue
			}
		}

		for j := 0; j < resourceSpan.ScopeSpans().Len(); j++ {
			scopeSpan := resourceSpan.ScopeSpans().At(j)
			for k := 0; k < scopeSpan.Spans().Len(); k++ {
				span := scopeSpan.Spans().At(k)
				timestampMatch := timestampLowerBound == nil || span.StartTimestamp().AsTime().After(*timestampLowerBound)
				var spanMatches bool
				if timestampMatch {
					spanMatches = spanMatchFn(span)
				}
				if timestampMatch && spanMatches {
					return true
				}
			}
		}
	}
	return false
}

//nolint:all
func resourceSpansHaveExpectedResourceAttributes(workloadType string) func(span ptrace.ResourceSpans) bool {
	return func(resourceSpans ptrace.ResourceSpans) bool {
		attributes := resourceSpans.Resource().Attributes()

		workloadAttributeFound := false
		if workloadType == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			workloadAttributeFound = true
		} else {
			workloadKey := fmt.Sprintf("k8s.%s.name", workloadType)
			expectedWorkloadValue := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
			workloadAttribute, hasWorkloadAttribute := attributes.Get(workloadKey)
			if hasWorkloadAttribute {
				if workloadAttribute.Str() == expectedWorkloadValue {
					workloadAttributeFound = true
				}
			}
		}

		podKey := "k8s.pod.name"
		expectedPodName := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
		expectedPodPrefix := fmt.Sprintf("%s-", expectedPodName)
		podAttributeFound := false
		podAttribute, hasPodAttribute := attributes.Get(podKey)
		if hasPodAttribute {
			if workloadType == "pod" {
				if podAttribute.Str() == expectedPodName {
					podAttributeFound = true
				}
			} else {
				if strings.Contains(podAttribute.Str(), expectedPodPrefix) {
					podAttributeFound = true
				}
			}
		}

		return workloadAttributeFound && podAttributeFound
	}
}

func matchHttpServerSpanWithHttpTarget(expectedTarget string) func(span ptrace.Span) bool {
	return func(span ptrace.Span) bool {
		if span.Kind() == ptrace.SpanKindServer {
			target, hasTarget := span.Attributes().Get("http.target")
			if hasTarget {
				if target.Str() == expectedTarget {
					return true
				}
			}
		}
		return false
	}
}
