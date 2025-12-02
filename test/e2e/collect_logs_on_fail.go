// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
)

const (
	collectedLogsBaseDir = "test-resources/e2e/logs"
)

func createDirAndDeleteOldCollectedLogs() {
	By("deleting old collected Kubernetes logs")
	_ = os.RemoveAll(collectedLogsBaseDir)
	_ = os.MkdirAll(collectedLogsBaseDir, 0755)
}

func collectPodInfoAndLogs(specReport SpecReport) {
	allTestNodeTexts := append(specReport.ContainerHierarchyTexts, specReport.LeafNodeText)
	fullyQualifiedTestName := strings.Join(allTestNodeTexts, " - ")
	outputPath := path.Join(collectedLogsBaseDir, fullyQualifiedTestName)
	By(
		fmt.Sprintf(
			"!! A failure has occurred. Collecting information about pods and their logs in \"%s\"",
			outputPath,
		))
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogs, cannot create directory \"%s\": %s\n",
			outputPath,
			err.Error(),
		)
		return
	}

	writeToFile([]byte(specReport.Failure.Message), outputPath, "_failure-message.txt")
	serializeToFile(specReport.Failure.Location, outputPath, "_failure-location.txt")
	serializeToFile(specReport.Failure.FailureNodeLocation, outputPath, "_failure-node-location.txt")

	for _, namespace := range []string{operatorNamespace,
		applicationUnderTestNamespace,
		"otlp-sink",
		"dash0-api",
	} {
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s get pods", namespace), outputPath)
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s describe pods", namespace), outputPath)
		getPodLogs(namespace, outputPath)
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s get configmaps", namespace), outputPath)
		executeCommandAndStoreOutput(fmt.Sprintf("kubectl -n %s describe configmaps", namespace), outputPath)
	}
	executeCommandAndStoreOutput("kubectl get --all-namespaces dash0monitorings.operator.dash0.com", outputPath)
	executeCommandAndStoreOutput("kubectl describe --all-namespaces dash0monitorings.operator.dash0.com", outputPath)
	executeCommandAndStoreOutput("kubectl get dash0operatorconfigurations.operator.dash0.com", outputPath)
	executeCommandAndStoreOutput("kubectl describe dash0operatorconfigurations.operator.dash0.com", outputPath)
	By(fmt.Sprintf(
		"!! Information about pods and their logs have been collected in \"%s\"\n",
		outputPath,
	))
}

func getPodLogs(namespace string, outputPath string) {
	podNames := getPodNames(namespace)
	for _, podName := range podNames {
		executeCommandAndStoreOutput(
			fmt.Sprintf(
				"kubectl -n %s logs %s --all-containers=true",
				namespace,
				podName,
			),
			outputPath,
		)
	}
}

func getPodNames(namespace string) []string {
	podsJson, err := run(exec.Command("kubectl", "-n", namespace, "get", "pods", "--output=json"))
	if err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogs when running kubectl get pods to fetch pod names: %s\n",
			err.Error(),
		)
		return nil
	}
	var parsedOutput map[string]interface{}
	if err = json.Unmarshal([]byte(podsJson), &parsedOutput); err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogs when parsing the output of kubectl get pods to fetch pod names: %s\n",
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

func executeCommandAndStoreOutput(fullCommandLine string, outputPath string) {
	commandParts := strings.Split(fullCommandLine, " ")
	fileName := strings.Join(commandParts, "_")

	output, err := run(exec.Command(commandParts[0], commandParts[1:]...), false)
	if err != nil {
		e2ePrint("Error in collectPodInfoAndLogs for command: \"%s\": %s\n", fullCommandLine, err.Error())
		return
	}

	content := slices.Concat(
		[]byte(fmt.Sprintf("output of command \"%s\":\n\n", fullCommandLine)),
		[]byte(output),
	)
	writeToFile(content, outputPath, fileName)
}

func serializeToFile(value any, outputPath string, filename string) {
	if content, err := json.Marshal(value); err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogs when serializing content for file \"%s\": %s",
			filename,
			err.Error(),
		)
	} else {
		writeToFile(content, outputPath, filename)
	}
}

func writeToFile(content []byte, outputPath string, filename string) {
	fullFileName := path.Join(outputPath, filename)
	if err := os.WriteFile(fullFileName, content, 0644); err != nil {
		e2ePrint(
			"Error in collectPodInfoAndLogs when writing to file \"%s\": %s",
			fullFileName,
			err.Error(),
		)
	}
}
