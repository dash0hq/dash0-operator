// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"

	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

const (
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"
	applicationPath               = "test-resources/node.js/express"
)

func rebuildNodeJsApplicationContainerImage() {
	By("building the dash0-operator-nodejs-20-express-test-app image")
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				applicationPath,
				"-t",
				"dash0-operator-nodejs-20-express-test-app",
			))).To(Succeed())
}

func installNodeJsCronJob(namespace string, testId string) error {
	addTestIdToCronjobManifest(testId)
	return installNodeJsApplication(
		namespace,
		"cronjob",
		nil,
	)
}

func uninstallNodeJsCronJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "cronjob")
}

func installNodeJsDaemonSet(namespace string, testId string) error {
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

//nolint:unparam
func installNodeJsDaemonSetWithOptOutLabel(namespace string, testId string) error {
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

func installNodeJsDeployment(namespace string, testId string) error {
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

func installNodeJsJob(namespace string, testId string) error {
	addTestIdToJobManifest(testId)
	return installNodeJsApplication(
		namespace,
		"job",
		nil,
	)
}

func uninstallNodeJsJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "job")
}

//nolint:unparam
func installNodeJsPod(namespace string, testId string) error {
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

//nolint:unparam
func installNodeJsReplicaSet(namespace string, testId string) error {
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

//nolint:unparam
func installNodeJsStatefulSet(namespace string, testId string) error {
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
	workloadType string,
	waitCommand *exec.Cmd,
) error {
	err := runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--namespace",
		namespace,
		"-f",
		manifest(workloadType),
	))
	if err != nil {
		return err
	}
	if waitCommand == nil {
		return nil
	}
	return waitForApplicationToBecomeReady(workloadType, waitCommand)
}

func uninstallNodeJsApplication(namespace string, workloadType string) error {
	return runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"--ignore-not-found",
			"-f",
			manifest(workloadType),
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

func addTestIdToCronjobManifest(testId string) {
	filename := manifest("cronjob")
	applicationManifestContentRaw, err := os.ReadFile(filename)
	Expect(err).ToNot(HaveOccurred())
	applicationManifestParsed := make(map[string]interface{})
	Expect(yaml.Unmarshal(applicationManifestContentRaw, &applicationManifestParsed)).To(Succeed())
	cronjobSpec := (applicationManifestParsed["spec"]).(map[string]interface{})
	jobTemplate := (cronjobSpec["jobTemplate"]).(map[string]interface{})
	jobTemplate["spec"] = addEnvVarToContainer(testId, jobTemplate)
	cronjobSpec["jobTemplate"] = jobTemplate
	applicationManifestParsed["spec"] = cronjobSpec
	updatedApplicationManifestContentRaw, err := yaml.Marshal(&applicationManifestParsed)
	Expect(err).ToNot(HaveOccurred())
	Expect(os.WriteFile(filename, updatedApplicationManifestContentRaw, 0644)).To(Succeed())
}

func addTestIdToJobManifest(testId string) {
	filename := manifest("job")
	applicationManifestContentRaw, err := os.ReadFile(filename)
	Expect(err).ToNot(HaveOccurred())
	applicationManifestParsed := make(map[string]interface{})
	Expect(yaml.Unmarshal(applicationManifestContentRaw, &applicationManifestParsed)).To(Succeed())
	applicationManifestParsed["spec"] = addEnvVarToContainer(testId, applicationManifestParsed)
	updatedApplicationManifestContentRaw, err := yaml.Marshal(&applicationManifestParsed)
	Expect(err).ToNot(HaveOccurred())
	Expect(os.WriteFile(filename, updatedApplicationManifestContentRaw, 0644)).To(Succeed())
}

func addEnvVarToContainer(testId string, jobTemplateOrManifest map[string]interface{}) map[string]interface{} {
	jobSpec := (jobTemplateOrManifest["spec"]).(map[string]interface{})
	template := (jobSpec["template"]).(map[string]interface{})
	podSpec := (template["spec"]).(map[string]interface{})
	containers := (podSpec["containers"]).([]interface{})
	container := (containers[0]).(map[string]interface{})
	env := (container["env"]).([]interface{})
	newEnvVar := make(map[string]string)
	newEnvVar["name"] = "TEST_ID"
	newEnvVar["value"] = testId
	env = append(env, newEnvVar)
	container["env"] = env
	containers[0] = container
	podSpec["containers"] = containers
	template["spec"] = podSpec
	jobSpec["template"] = template
	return jobSpec
}

func manifest(workloadType string) string {
	return fmt.Sprintf("%s/%s.yaml", applicationPath, workloadType)
}
