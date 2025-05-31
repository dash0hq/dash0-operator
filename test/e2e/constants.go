// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

const (
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"

	runtimeTypeLabelNodeJs = "Node.js"
	applicationPathNodeJs  = "test-resources/node.js/express"
	workloadNameNodeJs     = "dash0-operator-nodejs-20-express-test"

	runtimeTypeLabelJvm = "JVM"
	applicationPathJvm  = "test-resources/jvm/spring-boot"
	workloadNameJvm     = "dash0-operator-jvm-spring-boot-test"

	runtimeTypeLabelDotnet = ".NET"
	applicationPathDotnet  = "test-resources/dotnet"
	workloadNameDotnet     = "dash0-operator-dotnet-test"
)
