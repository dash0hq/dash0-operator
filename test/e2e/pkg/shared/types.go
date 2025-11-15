// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package shared

type ExpectationMode string

const (
	ExpectAtLeastOne ExpectationMode = "at-least-one"
	ExpectExactlyOne ExpectationMode = "exactly-one"
	ExpectNoMatches  ExpectationMode = "no-matches"
)

var (
	AllExpectationModes = []string{
		string(ExpectAtLeastOne),
		string(ExpectExactlyOne),
		string(ExpectNoMatches),
	}
)

type LogResourceMatcherMode string

const (
	LogResourceMatcherWorkload                          LogResourceMatcherMode = "workload"
	LogResourceMatcherSelfMonitoringLogsOperatorManager LogResourceMatcherMode = "self-monitoring-logs-operator-manager"
	LogResourceMatcherSelfMonitoringLogsCollector       LogResourceMatcherMode = "self-monitoring-logs-collector"
)

var (
	AllLogResourceMatcherModes = []string{
		string(LogResourceMatcherWorkload),
		string(LogResourceMatcherSelfMonitoringLogsOperatorManager),
		string(LogResourceMatcherSelfMonitoringLogsCollector),
	}
)

type MetricsMatchMode string

const (
	MetricsMatchModeWorkload                      MetricsMatchMode = "metrics-match-mode-workload"
	MetricsMatchModeSelfMonitoringOperatorManager MetricsMatchMode = "metrics-match-mode-self-monitoring-operator-manager"
	MetricsMatchModeSelfMonitoringCollector       MetricsMatchMode = "metrics-match-mode-self-monitoring-collector"
	MetricsMatchModeMatchAll                      MetricsMatchMode = "metrics-match-mode-match-all"
)

var (
	AllMetricsMatchModes = []string{
		string(MetricsMatchModeWorkload),
		string(MetricsMatchModeSelfMonitoringOperatorManager),
		string(MetricsMatchModeSelfMonitoringCollector),
		string(MetricsMatchModeMatchAll),
	}
)

type MetricNameList string

const (
	KubeletStatsReceiverMetricNameList MetricNameList = "kubelet-stats-receiver"
	K8sClusterReceiverMetricNameList   MetricNameList = "k8s-cluster-receiver"
	PrometheusReceiverMetricNameList   MetricNameList = "prometheus-receiver"
)

var (
	AllMetricNameLists = []string{
		string(KubeletStatsReceiverMetricNameList),
		string(K8sClusterReceiverMetricNameList),
		string(PrometheusReceiverMetricNameList),
	}
)

type ExpectationResult struct {
	Success     bool   `json:"success"`
	Description string `json:"description,omitempty"`
}

func NewSuccess() ExpectationResult {
	return ExpectationResult{
		Success: true,
	}
}

func NewFailureWithDescription(description string) ExpectationResult {
	return ExpectationResult{
		Success:     false,
		Description: description,
	}
}
