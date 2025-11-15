// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package shared

const (
	// common telemetry-matcher query parameters

	QueryParamExpectationMode        = "expectation-mode"
	QueryParamRuntime                = "runtime"
	QueryParamRuntimeWorkloadName    = "runtime-workload-name"
	QueryParamWorkloadType           = "workload-type"
	QueryParamTimestampLowerBoundStr = "timestamp-lower-bound"
	QueryParamClusterName            = "cluster"
	QueryParamOperatorNamespace      = "operator-namespace"

	// query parameters for matching spans

	QueryParamCheckResourceAttributes = "check-resource-attributes"
	QueryParamRoute                   = "route"
	QueryParamQuery                   = "query"
	QueryParamTarget                  = "target"

	// query parameters for matching logs

	QueryParamLogsResourceMatcherMode = "logs-resource-matcher"
	QueryParamServiceVersion          = "service-version"
	QueryParamLogBodyEquals           = "log-body-equals"
	QueryParamLogBodyContains         = "log-body-contains"

	// query parameters for matching metrics

	QueryParamMetricsMatchMode            = "metrics-match-mode"
	QueryParamDeploymentName              = "deployment-name"
	QueryParamExpectPodUid                = "expect-pod-uid"
	QueryParamFailOnNamespaceOtherThan    = "fail-on-namespace-other-than"
	QueryParamFailOnNamespaceScopedMetric = "fail-on-namespace-scoped-metric"
	QueryParamMetricNameList              = "metric-name-list"
)
