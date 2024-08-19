// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

func rebuildNodeJsApplicationContainerImage() {
	By("building the dash0-operator-nodejs-20-express-test-app image")
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"test-resources/node.js/express",
				"-t",
				"dash0-operator-nodejs-20-express-test-app",
			))).To(Succeed())
}

func installNodeJsCronJob(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"cronjob",
		nil,
	)
}

func uninstallNodeJsCronJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "cronjob")
}

func installNodeJsDaemonSet(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"daemonset",
		exec.Command(
			"kubectl",
			"rollout",
			"status",
			"daemonset",
			"dash0-operator-nodejs-20-express-test-daemonset",
			"--namespace",
			namespace,
			"--timeout",
			"60s",
		),
	)
}

func installNodeJsDaemonSetWithOptOutLabel(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"daemonset.opt-out",
		exec.Command(
			"kubectl",
			"rollout",
			"status",
			"daemonset",
			"dash0-operator-nodejs-20-express-test-daemonset",
			"--namespace",
			namespace,
			"--timeout",
			"60s",
		),
	)
}

func uninstallNodeJsDaemonSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "daemonset")
}

func installNodeJsDeployment(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"deployment",
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/dash0-operator-nodejs-20-express-test-deployment",
			"--for",
			"condition=Available",
			"--namespace",
			namespace,
			"--timeout",
			"60s",
		),
	)
}

func uninstallNodeJsDeployment(namespace string) error {
	return uninstallNodeJsApplication(namespace, "deployment")
}

func installNodeJsJob(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"job",
		nil,
	)
}

func uninstallNodeJsJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "job")
}

func installNodeJsPod(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"pod",
		exec.Command(
			"kubectl",
			"wait",
			"pod",
			"--namespace",
			namespace,
			"--selector",
			"app=dash0-operator-nodejs-20-express-test-pod-app",
			"--for",
			"condition=ContainersReady",
			"--timeout",
			"60s",
		),
	)
}

func uninstallNodeJsPod(namespace string) error {
	return uninstallNodeJsApplication(namespace, "pod")
}

func installNodeJsReplicaSet(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"replicaset",
		exec.Command(
			"kubectl",
			"wait",
			"pod",
			"--namespace",
			namespace,
			"--selector",
			"app=dash0-operator-nodejs-20-express-test-replicaset-app",
			"--for",
			"condition=ContainersReady",
			"--timeout",
			"60s",
		),
	)
}

func uninstallNodeJsReplicaSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "replicaset")
}

func installNodeJsStatefulSet(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"statefulset",
		exec.Command(
			"kubectl",
			"rollout",
			"status",
			"statefulset",
			"dash0-operator-nodejs-20-express-test-statefulset",
			"--namespace",
			namespace,
			"--timeout",
			"60s",
		),
	)
}

func uninstallNodeJsStatefulSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "statefulset")
}

func removeAllTestApplications(namespace string) {
	By("uninstalling the test applications")
	Expect(uninstallNodeJsCronJob(namespace)).To(Succeed())
	Expect(uninstallNodeJsDaemonSet(namespace)).To(Succeed())
	Expect(uninstallNodeJsDeployment(namespace)).To(Succeed())
	Expect(uninstallNodeJsJob(namespace)).To(Succeed())
	Expect(uninstallNodeJsPod(namespace)).To(Succeed())
	Expect(uninstallNodeJsReplicaSet(namespace)).To(Succeed())
	Expect(uninstallNodeJsStatefulSet(namespace)).To(Succeed())
}

func installNodeJsApplication(
	namespace string,
	templateName string,
	waitCommand *exec.Cmd,
) error {
	err := runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--namespace",
		namespace,
		"-f",
		fmt.Sprintf("test-resources/node.js/express/%s.yaml", templateName),
	))
	if err != nil {
		return err
	}
	if waitCommand == nil {
		return nil
	}
	return waitForApplicationToBecomeReady(templateName, waitCommand)
}

func uninstallNodeJsApplication(namespace string, kind string) error {
	return runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"--ignore-not-found",
			"-f",
			fmt.Sprintf("test-resources/node.js/express/%s.yaml", kind),
		))
}

func addOptOutLabel(namespace string, workloadType string, workloadName string) error {
	return runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"label",
			"--namespace",
			namespace,
			"--overwrite",
			workloadType,
			workloadName,
			"dash0.com/enable=false",
		))
}

func removeOptOutLabel(namespace string, workloadType string, workloadName string) error {
	return runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"label",
			"--namespace",
			namespace,
			workloadType,
			workloadName,
			"dash0.com/enable-",
		))
}

func deleteTestIdFiles() {
	_ = os.Remove("test-resources/e2e-test-volumes/test-uuid/cronjob.test.id")
	_ = os.Remove("test-resources/e2e-test-volumes/test-uuid/job.test.id")
}
