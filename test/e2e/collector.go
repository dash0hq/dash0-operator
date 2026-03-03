// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	collectorDaemonSetName = fmt.Sprintf(
		"%s-opentelemetry-collector-agent-daemonset",
		operatorHelmReleaseName,
	)
	collectorDaemonSetNameQualified = fmt.Sprintf(
		"daemonset/%s",
		collectorDaemonSetName,
	)
	collectorDeploymentName = fmt.Sprintf(
		"%s-cluster-metrics-collector-deployment",
		operatorHelmReleaseName,
	)
	collectorDeploymentNameQualified = fmt.Sprintf(
		"deployment/%s",
		collectorDeploymentName,
	)
	collectorDaemonSetConfigMapName = fmt.Sprintf(
		"%s-opentelemetry-collector-agent-cm",
		operatorHelmReleaseName,
	)
	collectorDaemonSetConfigMapNameQualified = fmt.Sprintf(
		"configmap/%s",
		collectorDaemonSetConfigMapName,
	)
	collectorDeploymentConfigMapName = fmt.Sprintf(
		"%s-cluster-metrics-collector-cm",
		operatorHelmReleaseName,
	)
	collectorDeploymentConfigMapNameQualified = fmt.Sprintf(
		"configmap/%s",
		collectorDeploymentConfigMapName,
	)

	collectorReadyLogMessage = "Everything is ready. Begin running and processing data."
)

func waitForCollectorToStart(operatorNamespace string, operatorHelmChart string) {
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

// verifyThatCollectorIsRemovedEventually assumes that the collector definitely has been deployed before this function
// is called, otherwise the way the verification is implemented is not valid.
func verifyThatCollectorIsRemovedEventually() {
	By("validating that the OpenTelemetry collector is removed eventually")
	Eventually(verifyCollectorDaemonSetIsNotPresent, 60*time.Second, time.Second).Should(Succeed())
	Eventually(verifyCollectorDeploymentIsNotPresent, 60*time.Second, time.Second).Should(Succeed())
}

func verifyThatCollectorIsNotPresentConsistently() {
	By("validating that the OpenTelemetry collector is not present consistently")
	Consistently(verifyCollectorDaemonSetIsNotPresent, 30*time.Second, time.Second).Should(Succeed())
	Consistently(verifyCollectorDeploymentIsNotPresent, 30*time.Second, time.Second).Should(Succeed())
}

func verifyCollectorDaemonSetIsNotPresent(g Gomega) {
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

func verifyCollectorDeploymentIsNotPresent(g Gomega) {
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

func verifyDaemonSetCollectorConfigMapContainsString(operatorNamespace string, s string) {
	verifyConfigMapContainsString(operatorNamespace, collectorDaemonSetConfigMapNameQualified, s)
}

// nolint:unused
func verifyDeploymentCollectorConfigMapContainsString(operatorNamespace string, s string) {
	verifyConfigMapContainsString(operatorNamespace, collectorDeploymentConfigMapNameQualified, s)
}

func verifyDaemonSetCollectorConfigMapDoesNotContainStrings(operatorNamespace string, s string) {
	verifyConfigMapDoesNotContainStrings(operatorNamespace, collectorDaemonSetConfigMapNameQualified, s)
}

func verifyDeploymentCollectorConfigMapDoesNotContainStrings(operatorNamespace string, s string) {
	verifyConfigMapDoesNotContainStrings(operatorNamespace, collectorDeploymentConfigMapNameQualified, s)
}

func verifyConfigMapContainsString(operatorNamespace string, configMapNameQualified, s string) {
	verifyCommandOutputContainsStrings(
		exec.Command(
			"kubectl",
			"get",
			"-n",
			operatorNamespace,
			configMapNameQualified,
			"-o",
			`jsonpath="{.data.config\.yaml}"`,
		),
		20*time.Second,
		s,
	)
}

func verifyConfigMapDoesNotContainStrings(operatorNamespace string, configMapNameQualified, s string) {
	verifyCommandOutputDoesNotContainStrings(
		exec.Command(
			"kubectl",
			"get",
			"-n",
			operatorNamespace,
			configMapNameQualified,
			"-o",
			`jsonpath="{.data.config\.yaml}"`,
		),
		s,
	)
}

func findMostRecentCollectorReadyLogLine(g Gomega, collectorName string) time.Time {
	allCollectorReadyLogLines := getMatchingLogLinesFomCollectorContainerLog(
		g,
		operatorNamespace,
		collectorName,
		"opentelemetry-collector",
		collectorReadyLogMessage,
	)
	g.Expect(allCollectorReadyLogLines).ToNot(BeEmpty(),
		"Expected to find a log line containing the string \"%s\", but no such line has been found.",
		collectorReadyLogMessage)

	mostRecentCollectorReadyLogLine := allCollectorReadyLogLines[len(allCollectorReadyLogLines)-1]
	timeStampRaw := strings.Split(mostRecentCollectorReadyLogLine, "\t")[0]
	timeStamp, err := time.Parse(time.RFC3339, timeStampRaw)
	Expect(err).ToNot(HaveOccurred(), "could not parse time stamp from collector startup log line")
	return timeStamp
}

func getMatchingLogLinesFomCollectorContainerLog(
	g Gomega,
	operatorNamespace string,
	collectorName string,
	containerName string,
	needle string,
) []string {
	matchingLines := []string{}
	command := getLogsViaKubectl(operatorNamespace, collectorName, containerName)
	logs, err := run(command, false)
	g.Expect(err).ToNot(HaveOccurred())
	lines := strings.Split(logs, "\n")
	for _, line := range lines {
		if strings.Contains(line, needle) {
			matchingLines = append(matchingLines, line)
		}
	}
	return matchingLines
}

func getLogsViaKubectl(namespace string, workloadName string, containerName string) *exec.Cmd {
	return exec.Command(
		"kubectl",
		"logs",
		"-n",
		namespace,
		workloadName,
		"-c",
		containerName,
	)
}

// getDaemonSetCollectorPodRestartCounts returns the restart counts of the opentelemetry-collector container
// for all pods belonging to the collector daemonset, with the pod name as the key.
func getDaemonSetCollectorPodRestartCounts(operatorNamespace string) map[string]int {
	return getCollectorPodRestartCounts(
		operatorNamespace,
		"app.kubernetes.io/name=opentelemetry-collector,app.kubernetes.io/component=agent-collector",
	)
}

// getDeploymentCollectorPodRestartCounts returns the restart counts of the opentelemetry-collector container
// for all pods belonging to the collector deployment, with the pod name as the key.
func getDeploymentCollectorPodRestartCounts(operatorNamespace string) map[string]int {
	return getCollectorPodRestartCounts(
		operatorNamespace,
		"app.kubernetes.io/name=opentelemetry-collector,app.kubernetes.io/component=cluster-metrics-collector",
	)
}

func getCollectorPodRestartCounts(operatorNamespace string, labelSelector string) map[string]int {
	output, err := run(
		exec.Command(
			"kubectl",
			"get",
			"pods",
			"--namespace",
			operatorNamespace,
			"-l",
			labelSelector,
			"-o",
			"json",
		),
		false,
	)
	Expect(err).ToNot(HaveOccurred())

	var podList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				ContainerStatuses []struct {
					Name         string `json:"name"`
					RestartCount int    `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	Expect(json.Unmarshal([]byte(output), &podList)).To(Succeed())

	restartCounts := map[string]int{}
	for _, pod := range podList.Items {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == "opentelemetry-collector" {
				restartCounts[pod.Metadata.Name] = containerStatus.RestartCount
				break
			}
		}
	}
	return restartCounts
}
