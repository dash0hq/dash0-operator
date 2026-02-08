// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

type runtimeType struct {
	runtimeTypeLabel string
	workloadName     string
	helmChartPath    string
	helmReleaseName  string
}

var (
	runtimeTypeDotnet = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelDotnet,
		workloadName:     workloadNameDotnet,
		helmChartPath:    chartPathDotnet,
		helmReleaseName:  releaseNameDotnet,
	}
	runtimeTypeJvm = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelJvm,
		workloadName:     workloadNameJvm,
		helmChartPath:    chartPathJvm,
		helmReleaseName:  releaseNameJvm,
	}
	runtimeTypeNodeJs = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelNodeJs,
		workloadName:     workloadNameNodeJs,
		helmChartPath:    chartPathNodeJs,
		helmReleaseName:  releaseNameNodeJs,
	}
	runtimeTypePython = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelPython,
		workloadName:     workloadNamePython,
		helmChartPath:    chartPathPython,
		helmReleaseName:  releaseNamePython,
	}
)
