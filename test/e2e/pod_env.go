// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
)

// readWorkloadContainerEnvVar returns the literal value of an environment variable on the named container of the
// given workload's pod template. Returns the empty string if the env var is not present.
func readWorkloadContainerEnvVar(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	containerName string,
	envVarName string,
) string {
	// For workloadType "pod", containers live at .spec.containers; for all other workload types they live under
	// .spec.template.spec.containers. The helper supports the common Deployment/DaemonSet/StatefulSet/ReplicaSet case,
	// which is the only one we use here.
	podSpecPath := ".spec.template.spec"
	if workloadType.workloadTypeString == "pod" {
		podSpecPath = ".spec"
	}
	jsonpath := fmt.Sprintf(
		`jsonpath={range %s.containers[?(@.name=="%s")]}{range .env[?(@.name=="%s")]}{.value}{end}{end}`,
		podSpecPath,
		containerName,
		envVarName,
	)
	value, err := run(exec.Command(
		"kubectl",
		"get",
		workloadType.workloadTypeString,
		workloadName(runtime, workloadType),
		"--namespace",
		namespace,
		"-o",
		jsonpath,
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(value)
}

// verifyPodEnvVar asserts that the named container of the given workload's pod template has the given env var set
// to expectedValue. If expectedValue is nil, the env var is expected to be absent.
func verifyPodEnvVar(
	g Gomega,
	namespace string,
	runtime runtimeType,
	workloadType workloadType,
	containerName string,
	envVarName string,
	expectedValue *string,
) {
	actual := readWorkloadContainerEnvVar(g, namespace, runtime, workloadType, containerName, envVarName)
	if expectedValue == nil {
		g.Expect(actual).To(BeEmpty(), "expected env var %s to be absent on %s %q, container %q, got %q",
			envVarName, workloadType.workloadTypeString, workloadName(runtime, workloadType), containerName, actual)
	} else {
		g.Expect(actual).To(Equal(*expectedValue), "env var %s on %s %q, container %q",
			envVarName, workloadType.workloadTypeString, workloadName(runtime, workloadType), containerName)
	}
}
