// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

var (
	collectorDaemonSetName           = fmt.Sprintf("%s-opentelemetry-collector-agent-daemonset", operatorHelmReleaseName)
	collectorDaemonSetNameQualified  = fmt.Sprintf("daemonset/%s", collectorDaemonSetName)
	collectorDeploymentName          = fmt.Sprintf("%s-cluster-metrics-collector-deployment", operatorHelmReleaseName)
	collectorDeploymentNameQualified = fmt.Sprintf("deployment/%s", collectorDeploymentName)
	collectorConfigMapName           = fmt.Sprintf("%s-opentelemetry-collector-agent-cm", operatorHelmReleaseName)
	collectorConfigMapNameQualified  = fmt.Sprintf("configmap/%s", collectorConfigMapName)
)

func verifyThatCollectorIsRunning(operatorNamespace string, operatorHelmChart string) {
	By("validating that the OpenTelemetry collector has been created and is running as expected")
	verifyCollectorIsUp := func(g Gomega) {
		// Even though this command comes with its own timeout, we still have to wrap it in an Eventually block, since
		// it will fail outright if the daemonset has not been created yet.
		g.Expect(runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"daemonset",
				collectorDaemonSetName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
		g.Expect(runAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"deployment",
				collectorDeploymentName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
	}

	Eventually(verifyCollectorIsUp, 60*time.Second, time.Second).Should(Succeed())

	verifyCollectorHasOwnerReference(operatorNamespace, operatorHelmChart)
}

func verifyCollectorHasOwnerReference(operatorNamespace string, operatorHelmChart string) {
	chartNameParts := strings.Split(operatorHelmChart, "/")
	chartName := chartNameParts[len(chartNameParts)-1]
	controllerDeploymentName := fmt.Sprintf("%s-controller", chartName)

	output, err := run(
		exec.Command(
			"kubectl",
			"get",
			"--namespace",
			operatorNamespace,
			collectorDaemonSetNameQualified,
			"-o",
			"jsonpath={.metadata.ownerReferences[0].name}",
		))
	Expect(err).NotTo(HaveOccurred())
	Expect(output).To(Equal(controllerDeploymentName))

	output, err = run(
		exec.Command(
			"kubectl",
			"get",
			"--namespace",
			operatorNamespace,
			collectorDeploymentNameQualified,
			"-o",
			"jsonpath={.metadata.ownerReferences[0].name}",
		))
	Expect(err).NotTo(HaveOccurred())
	Expect(output).To(Equal(controllerDeploymentName))
}

func verifyThatCollectorHasBeenRemoved(operatorNamespace string) {
	By("validating that the OpenTelemetry collector has been removed")
	verifyCollectorDaemonSetIsGone := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"get",
				"daemonset",
				"--namespace",
				operatorNamespace,
				collectorDaemonSetName,
			))).ToNot(Succeed())
	}
	Eventually(verifyCollectorDaemonSetIsGone, 60*time.Second, time.Second).Should(Succeed())
	verifyCollectorDeploymentIsGone := func(g Gomega) {
		g.Expect(runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"get",
				"deployment",
				"--namespace",
				operatorNamespace,
				collectorDeploymentName,
			))).ToNot(Succeed())
	}
	Eventually(verifyCollectorDeploymentIsGone, 60*time.Second, time.Second).Should(Succeed())
}

func verifyConfigMapContainsString(operatorNamespace string, s string) {
	verifyCommandOutputContainsStrings(
		exec.Command(
			"kubectl",
			"get",
			"-n",
			operatorNamespace,
			collectorConfigMapNameQualified,
			"-o",
			"json",
		),
		s,
	)
}

func verifyCollectorContainerLogContainsStrings(
	operatorNamespace string,
	containerName string,
	needles ...string,
) {
	verifyCommandOutputContainsStrings(
		exec.Command(
			"kubectl",
			"logs",
			"-n",
			operatorNamespace,
			collectorDaemonSetNameQualified,
			"-c",
			containerName,
		),
		needles...,
	)
}
