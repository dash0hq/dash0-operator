// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	collectedLogsDir = "test-resources/e2e/logs"
)

func createDirAndDeleteOldCollectedLogs() {
	By("deleting old collected Kubernetes logs")
	_ = os.RemoveAll(collectedLogsDir)
	_ = os.MkdirAll(collectedLogsDir, 0755)
}

func collectPodInfoAndLogsFailWrapper(message string, callerSkip ...int) {
	// Reset the fail handler to its default, so failures in collectPodInfoAndLogs() do not trigger
	// collectPodInfoAndLogs again.
	RegisterFailHandler(Fail)
	if len(callerSkip) == 1 {
		callerSkip[0]++
	}
	collectPodInfoAndLogs()
	Fail(message, callerSkip...)
}

func collectPodInfoAndLogs() {
	By(
		fmt.Sprintf(
			"!! A failure has occurred. Collecting information about pods and their logs in %s",
			collectedLogsDir,
		))
	for _, namespace := range []string{operatorNamespace,
		applicationUnderTestNamespace,
		"otlp-sink",
		"dash0-api",
	} {
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s get pods", namespace))
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s describe pods", namespace))
		getPodLogs(namespace)
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s get configmaps", namespace))
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s describe configmaps", namespace))
	}
	executeCommandAndStoreOutput("kubectl get --all-namespaces dash0monitorings.operator.dash0.com")
	executeCommandAndStoreOutput("kubectl describe --all-namespaces dash0monitorings.operator.dash0.com")
	executeCommandAndStoreOutput("kubectl get dash0operatorconfigurations.operator.dash0.com")
	executeCommandAndStoreOutput("kubectl describe dash0operatorconfigurations.operator.dash0.com")
	By(fmt.Sprintf(
		"!! Information about pods and their logs have been collected in %s\n",
		collectedLogsDir,
	))
}

func getPodLogs(namespace string) {
	podNames := getPodNames(namespace)
	for _, podName := range podNames {
		executeCommandAndStoreOutput(
			fmt.Sprintf(
				"kubectl -n %s logs %s --all-containers=true",
				namespace,
				podName,
			),
		)
	}
}

func getPodNames(namespace string) []string {
	podsJson, err := run(exec.Command("kubectl", "-n", namespace, "get", "pods", "--output=json"))
	if err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogsFailWrapper when running kubectl get pods to fetch pod names: %s\n",
			err.Error(),
		)
		return nil
	}
	var parsedOutput map[string]interface{}
	if err = json.Unmarshal([]byte(podsJson), &parsedOutput); err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogsFailWrapper when parsing the output of kubectl get pods to fetch pod names: %s\n",
			err.Error(),
		)
		return nil
	}
	podItems, ok := parsedOutput["items"].([]interface{})
	if !ok {
		e2ePrint(
			"Unexpected JSON structure for output of kubectl get pods to fetch pod names:\n%s\n",
			podsJson,
		)
		return nil
	}

	podNames := make([]string, 0, len(podItems))
	for podIdx, podItemRaw := range podItems {
		podItem, ok := podItemRaw.(map[string]interface{})
		if !ok {
			e2ePrint(
				"Unexpected JSON structure for pod item %d when fetching pod names:\n%s\n",
				podIdx,
				podsJson,
			)
			continue
		}
		podMetadata, ok := podItem["metadata"].(map[string]interface{})
		if !ok {
			e2ePrint(
				"Unexpected JSON structure for pod metadata at index %d when fetching pod names:\n%s\n",
				podIdx,
				podsJson,
			)
			continue
		}
		podNameRaw := podMetadata["name"]
		if podNameRaw == nil {
			e2ePrint(
				"Pod metadate for item %d does not have a name attribute:\n%s\n",
				podIdx,
				podsJson,
			)
			continue
		}
		podName, ok := podNameRaw.(string)
		if !ok {
			e2ePrint(
				"Pod name for item %d is not a string:\n%s\n",
				podIdx,
				podsJson,
			)
			continue
		}
		podNames = append(podNames, podName)
	}
	return podNames
}

func executeCommandAndStoreOutput(fullCommandLine string) {
	commandParts := strings.Split(fullCommandLine, " ")
	fileName := strings.Join(commandParts, "_")
	fullFileName := fmt.Sprintf("%s/%s", collectedLogsDir, fileName)
	output, err := run(exec.Command(commandParts[0], commandParts[1:]...), false)
	if err != nil {
		e2ePrint("Error in collectPodInfoAndLogsFailWrapper for command: %s: %s\n", fullCommandLine, err.Error())
		return
	}

	content := slices.Concat(
		[]byte(fmt.Sprintf("output of command \"%s\":\n\n", fullCommandLine)),
		[]byte(output),
	)
	if err = os.WriteFile(fullFileName, content, 0644); err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogsFailWrapper when writing command output to file \"%s\": %s\n%s",
			fullCommandLine,
			err.Error(),
			output,
		)
	}
}
