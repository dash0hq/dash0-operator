// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

type expectationMode string

const (
	expectAtLeastOne expectationMode = "at-least-one"
	expectExactlyOne expectationMode = "exactly-one"
	expectNoMatches  expectationMode = "no-matches"
)

var (
	allExpectationModes = []string{
		string(expectAtLeastOne),
		string(expectExactlyOne),
		string(expectNoMatches),
	}
)

type logResourceMatcherMode string

const (
	logResourceMatcherWorkload                          logResourceMatcherMode = "workload"
	logResourceMatcherSelfMonitoringLogsOperatorManager logResourceMatcherMode = "self-monitoring-logs-operator-manager"
	logResourceMatcherSelfMonitoringLogsCollector       logResourceMatcherMode = "self-monitoring-logs-collector"
)

var (
	allLogResourceMatcherModes = []string{
		string(logResourceMatcherWorkload),
		string(logResourceMatcherSelfMonitoringLogsOperatorManager),
		string(logResourceMatcherSelfMonitoringLogsCollector),
	}
)

type metricsMatchMode string

const (
	metricsMatchModeWorkload                      metricsMatchMode = "metrics-match-mode-worklaod"
	metricsMatchModeSelfMonitoringOperatorManager metricsMatchMode = "metrics-match-mode-self-monitoring-operator-manager"
	metricsMatchModeSelfMonitoringCollector       metricsMatchMode = "metrics-match-mode-self-monitoring-collector"
	metricsMatchModeMatchAll                      metricsMatchMode = "metrics-match-mode-match-all"
)

var (
	allMetricsMatchModes = []string{
		string(metricsMatchModeWorkload),
		string(metricsMatchModeSelfMonitoringOperatorManager),
		string(metricsMatchModeSelfMonitoringCollector),
		string(metricsMatchModeMatchAll),
	}
)

type metricNameList string

const (
	kubeletStatsReceiverMetricNameList metricNameList = "kubelet-stats-receiver"
	k8sClusterReceiverMetricNameList   metricNameList = "k8s-cluster-receiver"
	prometheusReceiverMetricNameList   metricNameList = "prometheus-receiver"
)

var (
	allMetricNameLists = []string{
		string(kubeletStatsReceiverMetricNameList),
		string(k8sClusterReceiverMetricNameList),
		string(prometheusReceiverMetricNameList),
	}
)

type ExpectationResult struct {
	Success     bool   `json:"success"`
	Description string `json:"description,omitempty"`
}

func newSuccess() ExpectationResult {
	return ExpectationResult{
		Success: true,
	}
}

func newFailureWithDescription(description string) ExpectationResult {
	return ExpectationResult{
		Success:     false,
		Description: description,
	}
}

type MatchAssertionError struct {
	Message string
}

func newMatchAssertionError(format string, a ...any) *MatchAssertionError {
	return &MatchAssertionError{
		Message: fmt.Sprintf(format, a...),
	}
}

func (m *MatchAssertionError) Error() string {
	return m.Message
}
