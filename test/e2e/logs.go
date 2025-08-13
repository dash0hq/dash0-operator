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
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"

	. "github.com/onsi/gomega"
)

const (
	otlpSinkLogsJons      = "test-resources/e2e/volumes/otlp-sink/logs.jsonl"
	logsJsonMaxLineLength = 1_048_576

	logBodyKey = "log.body"
)

var (
	logsUnmarshaller = &plog.JSONUnmarshaler{}
)

func verifyAtLeastOneLogRecord(
	g Gomega,
	resourceMatchFn func(plog.ResourceLogs, *ResourceMatchResult[plog.ResourceLogs]),
	logMatchFn func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]),
	timestampLowerBound time.Time,
) {
	allMatchResults := fileHasMatchingLogRecords(
		g,
		resourceMatchFn,
		logMatchFn,
		timestampLowerBound,
	)
	allMatchResults.expectAtLeastOneMatch(
		g,
		"expected to find at least one log record",
	)
}

func verifyExactlyOneLogRecord(
	g Gomega,
	runtime runtimeType,
	workloadType workloadType,
	testId string,
	timestampLowerBound time.Time,
) {
	logBodyPattern := fmt.Sprintf("processing request %s", testId)
	logRecordMatchFn := func(
		logRecord plog.LogRecord,
		matchResult *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord],
	) {
		if strings.Contains(logRecord.Body().AsString(), logBodyPattern) {
			matchResult.addPassedAssertion(logBodyKey)
		} else {
			matchResult.addFailedAssertion(
				logBodyKey,
				fmt.Sprintf("expected a string containing %s but it was %s", logBodyPattern, logRecord.Body().AsString()),
			)
		}
	}

	allMatchResults := fileHasMatchingLogRecords(
		g,
		workloadLogsResourceMatcher(runtime, workloadType),
		logRecordMatchFn,
		timestampLowerBound,
	)

	allMatchResults.expectExactlyOneMatch(g, "expected to find exactly one log record")
}

//nolint:all
func fileHasMatchingLogRecords(
	g Gomega,
	resourceMatchFn func(plog.ResourceLogs, *ResourceMatchResult[plog.ResourceLogs]),
	logRecordMatchFn func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]),
	timestampLowerBound time.Time,
) MatchResultList[plog.ResourceLogs, plog.LogRecord] {
	fileHandle, err := os.Open(otlpSinkLogsJons)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, logsJsonMaxLineLength), logsJsonMaxLineLength)

	matchResults := newMatchResultList[plog.ResourceLogs, plog.LogRecord]()

	// read file line by line
	for scanner.Scan() {
		resourceLogRecordBytes := scanner.Bytes()
		logs, err := logsUnmarshaller.UnmarshalLogs(resourceLogRecordBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		hasMatchingLogRecords(
			logs,
			resourceMatchFn,
			logRecordMatchFn,
			timestampLowerBound,
			&matchResults,
		)
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return matchResults
}

//nolint:all
func hasMatchingLogRecords(
	logs plog.Logs,
	resourceMatchFn func(plog.ResourceLogs, *ResourceMatchResult[plog.ResourceLogs]),
	logRecordMatchFn func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]),
	timestampLowerBound time.Time,
	allMatchResults *MatchResultList[plog.ResourceLogs, plog.LogRecord],
) {
	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLogRecord := logs.ResourceLogs().At(i)
		resourceMatchResult := newResourceMatchResult(resourceLogRecord)
		if resourceMatchFn != nil {
			resourceMatchFn(resourceLogRecord, &resourceMatchResult)
		}

		for j := 0; j < resourceLogRecord.ScopeLogs().Len(); j++ {
			scopeLogRecord := resourceLogRecord.ScopeLogs().At(j)
			for k := 0; k < scopeLogRecord.LogRecords().Len(); k++ {
				logRecord := scopeLogRecord.LogRecords().At(k)
				if !logRecord.Timestamp().AsTime().After(timestampLowerBound) {
					// This log record is too old, it is probably from a previously running test case, ignore it.
					continue
				}
				logRecordMatchResult := newObjectMatchResult[plog.ResourceLogs, plog.LogRecord](
					logRecord.Body().AsString(),
					resourceLogRecord,
					resourceMatchResult,
					logRecord,
				)
				logRecordMatchFn(logRecord, &logRecordMatchResult)
				allMatchResults.addResultForObject(logRecordMatchResult)
			}
		}
	}
}

func selfMonitoringLogsResourceMatcherOperatorManager(
	resourceLogs plog.ResourceLogs,
	matchResult *ResourceMatchResult[plog.ResourceLogs],
) {
	attributes := resourceLogs.Resource().Attributes()
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.ServiceNamespaceKey),
		"dash0-operator",
		matchResult,
	)
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.ServiceNameKey),
		"operator-manager",
		matchResult,
	)
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.ServiceVersionKey),
		"latest",
		matchResult,
	)
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.K8SDeploymentNameKey),
		"dash0-operator-controller",
		matchResult,
	)
	verifyResourceAttributeExists(
		attributes,
		string(semconv.K8SDeploymentUIDKey),
		matchResult,
	)
	verifyResourceAttributeStartsWith(
		attributes,
		string(semconv.K8SPodNameKey),
		"dash0-operator-controller-",
		matchResult,
	)
	verifyResourceAttributeStartsWith(
		attributes,
		string(semconv.K8SContainerNameKey),
		"operator-manager",
		matchResult,
	)

	selfMonitoringCommonResourceMatcher(attributes, matchResult)
}

func selfMonitoringLogsResourceMatcherCollector(
	resourceLogs plog.ResourceLogs,
	matchResult *ResourceMatchResult[plog.ResourceLogs],
) {
	attributes := resourceLogs.Resource().Attributes()
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.ServiceNamespaceKey),
		"dash0-operator",
		matchResult,
	)
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.ServiceNameKey),
		"dash0-operator-collector",
		matchResult,
	)
	verifyResourceAttributeExists(
		attributes,
		string(semconv.ServiceVersionKey),
		matchResult,
	)
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.K8SDaemonSetNameKey),
		"e2e-tests-operator-helm-release-opentelemetry-collector-agent-daemonset",
		matchResult,
	)
	verifyResourceAttributeExists(
		attributes,
		string(semconv.K8SDaemonSetUIDKey),
		matchResult,
	)
	verifyResourceAttributeStartsWith(
		attributes,
		string(semconv.K8SPodNameKey),
		"e2e-tests-operator-helm-release-opentelemetry-collector-a",
		matchResult,
	)
	verifyResourceAttributeStartsWith(
		attributes,
		string(semconv.K8SContainerNameKey),
		"opentelemetry-collector",
		matchResult,
	)

	selfMonitoringCommonResourceMatcher(attributes, matchResult)
}

func selfMonitoringCommonResourceMatcher(attributes pcommon.Map, matchResult *ResourceMatchResult[plog.ResourceLogs]) {
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.K8SClusterNameKey),
		e2eKubernetesContext,
		matchResult,
	)
	verifyResourceAttributeExists(
		attributes,
		string(semconv.K8SClusterUIDKey),
		matchResult,
	)
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.K8SNamespaceNameKey),
		operatorNamespace,
		matchResult,
	)
}

//nolint:all
func workloadLogsResourceMatcher(runtime runtimeType, workloadType workloadType) func(
	plog.ResourceLogs,
	*ResourceMatchResult[plog.ResourceLogs],
) {
	return func(resourceLogs plog.ResourceLogs, matchResult *ResourceMatchResult[plog.ResourceLogs]) {
		resourceAttributes := resourceLogs.Resource().Attributes()

		if workloadType.workloadTypeString == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			matchResult.addSkippedAssertion("k8s.replicaset.name", "not checked, there is no k8s.replicaset.name attribute")
		} else {
			verifyResourceAttributeEquals(
				resourceAttributes,
				fmt.Sprintf("k8s.%s.name", workloadType.workloadTypeString),
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
	}
}
