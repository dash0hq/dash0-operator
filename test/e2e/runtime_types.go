// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

type runtimeType struct {
	runtimeTypeLabel  string
	workloadName      string
	helmChartPath     string
	helmReleasePrefix string
}

var (
	runtimeTypeDotnet = runtimeType{
		runtimeTypeLabel:  runtimeTypeLabelDotnet,
		workloadName:      workloadNameDotnet,
		helmChartPath:     chartPathDotnet,
		helmReleasePrefix: releaseNameDotnetPrefix,
	}
	runtimeTypeJvm = runtimeType{
		runtimeTypeLabel:  runtimeTypeLabelJvm,
		workloadName:      workloadNameJvm,
		helmChartPath:     chartPathJvm,
		helmReleasePrefix: releaseNameJvmPrefix,
	}
	runtimeTypeNodeJs = runtimeType{
		runtimeTypeLabel:  runtimeTypeLabelNodeJs,
		workloadName:      workloadNameNodeJs,
		helmChartPath:     chartPathNodeJs,
		helmReleasePrefix: releaseNameNodeJsPrefix,
	}
	runtimeTypePython = runtimeType{
		runtimeTypeLabel:  runtimeTypeLabelPython,
		workloadName:      workloadNamePython,
		helmChartPath:     chartPathPython,
		helmReleasePrefix: releaseNamePythonPrefix,
	}
)
