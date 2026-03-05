// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import "fmt"

const (
	applicationUnderTestNamespace = "e2e-test-ns"

	monitoringAutoResourceName = "dash0-monitoring-auto-resource"

	runtimeTypeLabelDotnet  = ".NET"
	applicationPathDotnet   = "test-resources/dotnet"
	workloadNameDotnet      = "dash0-operator-dotnet-test"
	releaseNameDotnetPrefix = "dotnet"

	runtimeTypeLabelJvm  = "JVM"
	applicationPathJvm   = "test-resources/jvm/spring-boot"
	workloadNameJvm      = "dash0-operator-jvm-spring-boot-test"
	releaseNameJvmPrefix = "jvm"

	runtimeTypeLabelNodeJs  = "Node.js"
	applicationPathNodeJs   = "test-resources/node.js/express"
	workloadNameNodeJs      = "dash0-operator-nodejs-20-express-test"
	releaseNameNodeJsPrefix = "nodejs"

	runtimeTypeLabelPython  = "Python"
	applicationPathPython   = "test-resources/python/flask"
	workloadNamePython      = "dash0-operator-python-flask-test"
	releaseNamePythonPrefix = "python"

	defaultIngressPort = "8080"
)

var (
	chartPathDotnet = fmt.Sprintf("%s/helm-chart", applicationPathDotnet)
	chartPathJvm    = fmt.Sprintf("%s/helm-chart", applicationPathJvm)
	chartPathNodeJs = fmt.Sprintf("%s/helm-chart", applicationPathNodeJs)
	chartPathPython = fmt.Sprintf("%s/helm-chart", applicationPathPython)
)
