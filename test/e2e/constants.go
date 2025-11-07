// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import "fmt"

const (
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"

	runtimeTypeLabelNodeJs = "Node.js"
	applicationPathNodeJs  = "test-resources/node.js/express"
	workloadNameNodeJs     = "dash0-operator-nodejs-20-express-test"
	releaseNameNodeJs      = "test-app-nodejs"

	runtimeTypeLabelJvm = "JVM"
	applicationPathJvm  = "test-resources/jvm/spring-boot"
	workloadNameJvm     = "dash0-operator-jvm-spring-boot-test"
	releaseNameJvm      = "test-app-jvm"

	runtimeTypeLabelDotnet = ".NET"
	applicationPathDotnet  = "test-resources/dotnet"
	workloadNameDotnet     = "dash0-operator-dotnet-test"
	releaseNameDotnet      = "test-app-dotnet"
)

var (
	chartPathNodeJs = fmt.Sprintf("%s/dash0-operator-test-app-nodejs", applicationPathNodeJs)
	chartPathJvm    = fmt.Sprintf("%s/dash0-operator-test-app-jvm", applicationPathJvm)
	chartPathDotnet = fmt.Sprintf("%s/dash0-operator-test-app-dotnet", applicationPathDotnet)
)
