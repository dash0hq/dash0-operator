// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	. "github.com/onsi/gomega"
)

const (
	logsJsonMaxLineLength = 1_048_576
)

var (
	logsUnmarshaller = &plog.JSONUnmarshaler{}
)

func verifyExactlyOneLogRecordIsReported(g Gomega, testId string, timestampLowerBound *time.Time) error {
	matches := fileCountMatchingLogRecords(g, "deployment", fmt.Sprintf("processing request %s", testId), timestampLowerBound)
	switch matches {
	case 0:
		return fmt.Errorf("no matching logs found")
	case 1:
		return nil
	default:
		return fmt.Errorf("too many matching logs found: %d", matches)
	}
}

//nolint:all
func fileCountMatchingLogRecords(g Gomega, workloadType string, logBody string, timestampLowerBound *time.Time) int {
	fileHandle, err := os.Open("test-resources/e2e-test-volumes/otlp-sink/logs.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, logsJsonMaxLineLength), logsJsonMaxLineLength)

	var resourceMatchFn func(log plog.ResourceLogs) bool
	if workloadType != "" {
		resourceMatchFn = resourceLogRecordsHaveExpectedResourceAttributes(workloadType)
	}

	// read file line by line
	logsFound := 0
	for scanner.Scan() {
		resourceSpanBytes := scanner.Bytes()
		logs, err := logsUnmarshaller.UnmarshalLogs(resourceSpanBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}

		logsFound += countMatchingLogRecords(
			logs,
			resourceMatchFn,
			logBodyContains(logBody),
			timestampLowerBound,
		)
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return logsFound
}

func countMatchingLogRecords(
	logs plog.Logs,
	resourceMatchFn func(span plog.ResourceLogs) bool,
	logRecordMatchFn func(span plog.LogRecord) bool,
	timestampLowerBound *time.Time,
) int {
	matches := 0
	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLog := logs.ResourceLogs().At(i)
		if resourceMatchFn != nil {
			if !resourceMatchFn(resourceLog) {
				continue
			}
		}
		for j := 0; j < resourceLog.ScopeLogs().Len(); j++ {
			scopeLog := resourceLog.ScopeLogs().At(j)
			for k := 0; k < scopeLog.LogRecords().Len(); k++ {
				logRecord := scopeLog.LogRecords().At(k)
				if (timestampLowerBound == nil || logRecord.Timestamp().AsTime().After(*timestampLowerBound)) &&
					logRecordMatchFn(logRecord) {
					matches += 1
				}
			}
		}
	}
	return matches
}

//nolint:all
func resourceLogRecordsHaveExpectedResourceAttributes(workloadType string) func(span plog.ResourceLogs) bool {
	return func(resourceLogs plog.ResourceLogs) bool {
		attributes := resourceLogs.Resource().Attributes()
		attributes.Range(func(k string, v pcommon.Value) bool {
			return true
		})

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

func logBodyContains(substring string) func(logRecord plog.LogRecord) bool {
	return func(logRecord plog.LogRecord) bool {
		return strings.Contains(logRecord.Body().AsString(), substring)
	}
}
