// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

const (
	logsJsonMaxLineLength = 1_048_576

	logBodyKey = "log.body"
)

var (
	logsUnmarshaller = &plog.JSONUnmarshaler{}
)

//nolint:dupl
func readFileAndGetMatchingLogs(
	logsJsonlFilename string,
	resourceMatchFn func(plog.ResourceLogs, *ResourceMatchResult[plog.ResourceLogs]),
	logRecordMatchFn func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]),
	timestampLowerBound time.Time,
) (*MatchResultList[plog.ResourceLogs, plog.LogRecord], error) {
	fileHandle, err := os.Open(logsJsonlFilename)
	if err != nil {
		return nil, fmt.Errorf("cannot open file %s: %v", logsJsonlFilename, err)
	}
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, logsJsonMaxLineLength), logsJsonMaxLineLength)

	matchResults := newMatchResultList[plog.ResourceLogs, plog.LogRecord]()

	// read file line by line, each line can contain multiple resources logs, each with multiple scope logs and actual
	// log records
	for scanner.Scan() {
		resourceLogRecordBytes := scanner.Bytes()
		logs, err := logsUnmarshaller.UnmarshalLogs(resourceLogRecordBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		extractMatchingLogRecords(
			logs,
			resourceMatchFn,
			logRecordMatchFn,
			timestampLowerBound,
			&matchResults,
		)
	}

	if scanner.Err() != nil {
		return nil, fmt.Errorf("error while scanning file %s: %v", logsJsonlFilename, err)
	}

	return &matchResults, nil
}

func extractMatchingLogRecords(
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

func workloadLogsResourceMatcher(runtimeWorkloadName string, workloadType string) func(
	plog.ResourceLogs,
	*ResourceMatchResult[plog.ResourceLogs],
) {
	return func(resourceLogs plog.ResourceLogs, matchResult *ResourceMatchResult[plog.ResourceLogs]) {
		resourceAttributes := resourceLogs.Resource().Attributes()

		if workloadType == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			matchResult.addSkippedAssertion("k8s.replicaset.name", "not checked, there is no k8s.replicaset.name attribute")
		} else {
			verifyResourceAttributeEquals(
				resourceAttributes,
				fmt.Sprintf("k8s.%s.name", workloadType),
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
	}
}

func selfMonitoringLogsResourceMatcherOperatorManager(
	clusterName string,
	operatorNamespace string,
	serviceVersion string,
) func(
	plog.ResourceLogs,
	*ResourceMatchResult[plog.ResourceLogs],
) {
	return func(
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
		if serviceVersion == "" {
			// The expected service version will be the empty string when we are testing with a published Helm chart
			// and are using the container images from the chart.
			verifyResourceAttributeExists(
				attributes,
				string(semconv.ServiceVersionKey),
				matchResult,
			)
		} else {
			verifyResourceAttributeEquals(
				attributes,
				string(semconv.ServiceVersionKey),
				serviceVersion,
				matchResult,
			)
		}
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

		selfMonitoringCommonResourceMatcher(
			clusterName,
			operatorNamespace,
			attributes,
			matchResult,
		)
	}
}

func selfMonitoringLogsResourceMatcherCollector(
	clusterName string,
	operatorNamespace string,
) func(
	plog.ResourceLogs,
	*ResourceMatchResult[plog.ResourceLogs],
) {
	return func(
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
			"e2e-tests-operator-hr-opentelemetry-collector-agent-daemonset",
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
			"e2e-tests-operator-hr-opentelemetry-collector-a",
			matchResult,
		)
		verifyResourceAttributeStartsWith(
			attributes,
			string(semconv.K8SContainerNameKey),
			"opentelemetry-collector",
			matchResult,
		)

		selfMonitoringCommonResourceMatcher(
			clusterName,
			operatorNamespace,
			attributes,
			matchResult,
		)
	}
}

func selfMonitoringCommonResourceMatcher(
	clusterName string,
	operatorNamespace string,
	attributes pcommon.Map,
	matchResult *ResourceMatchResult[plog.ResourceLogs],
) {
	verifyResourceAttributeEquals(
		attributes,
		string(semconv.K8SClusterNameKey),
		clusterName,
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

func logBodyEqualsMatcher(
	expectedLogBody string,
) func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]) {
	return func(logRecord plog.LogRecord, matchResult *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]) {
		logBody := logRecord.Body().AsString()
		if logBody == expectedLogBody {
			matchResult.addPassedAssertion(logBodyKey)
		} else {
			matchResult.addFailedAssertion(
				logBodyKey,
				fmt.Sprintf("expected %s but it was %s", expectedLogBody, logBody),
			)
		}
	}
}

func logBodyContainsMatcher(
	expectedLogBodyPart string,
) func(plog.LogRecord, *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]) {
	return func(logRecord plog.LogRecord, matchResult *ObjectMatchResult[plog.ResourceLogs, plog.LogRecord]) {
		if strings.Contains(logRecord.Body().AsString(), expectedLogBodyPart) {
			matchResult.addPassedAssertion(logBodyKey)
		} else {
			matchResult.addFailedAssertion(
				logBodyKey,
				fmt.Sprintf("expected a string containing %s but it was %s", expectedLogBodyPart, logRecord.Body().AsString()),
			)
		}
	}
}
