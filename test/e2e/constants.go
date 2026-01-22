// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import "fmt"

const (
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"

	runtimeTypeLabelDotnet = ".NET"
	applicationPathDotnet  = "test-resources/dotnet"
	workloadNameDotnet     = "dash0-operator-dotnet-test"
	releaseNameDotnet      = "test-app-dotnet"

	runtimeTypeLabelJvm = "JVM"
	applicationPathJvm  = "test-resources/jvm/spring-boot"
	workloadNameJvm     = "dash0-operator-jvm-spring-boot-test"
	releaseNameJvm      = "test-app-jvm"

	runtimeTypeLabelNodeJs = "Node.js"
	applicationPathNodeJs  = "test-resources/node.js/express"
	workloadNameNodeJs     = "dash0-operator-nodejs-20-express-test"
	releaseNameNodeJs      = "test-app-nodejs"

	runtimeTypeLabelPython = "Python"
	applicationPathPython  = "test-resources/python/flask"
	workloadNamePython     = "dash0-operator-python-flask-test"
	releaseNamePython      = "test-app-python"

	defaultIngressPort = "8080"
)

var (
	chartPathDotnet = fmt.Sprintf("%s/helm-chart", applicationPathDotnet)
	chartPathJvm    = fmt.Sprintf("%s/helm-chart", applicationPathJvm)
	chartPathNodeJs = fmt.Sprintf("%s/helm-chart", applicationPathNodeJs)
	chartPathPython = fmt.Sprintf("%s/helm-chart", applicationPathPython)
)
