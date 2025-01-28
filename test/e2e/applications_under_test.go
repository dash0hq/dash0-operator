// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"

	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"

	applicationPathNodeJs = "test-resources/node.js/express"
	workloadNameNodeJs    = "dash0-operator-nodejs-20-express-test"

	applicationPathJvm = "test-resources/jvm/spring-boot"
	workloadNameJvm    = "dash0-operator-jvm-spring-boot-test"
)

var (
	temporaryManifestFiles []string

	workloadTypeCronjob = workloadType{
		workloadTypeString: "cronjob",
		basePort:           1205,
		isBatch:            true,
		waitCommand:        nil,
	}
	workloadTypeDaemonSet = workloadType{
		workloadTypeString: "daemonset",
		basePort:           1206,
		isBatch:            false,
		waitCommand: func(namespace string, runtime runtimeType) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"rollout",
				"status",
				"daemonset",
				fmt.Sprintf("%s-daemonset", runtime.workloadName),
				"--namespace",
				namespace,
				"--timeout",
				"60s",
			)
		},
	}
	workloadTypeDeployment = workloadType{
		workloadTypeString: "deployment",
		isBatch:            false,
		basePort:           1207,
		waitCommand: func(namespace string, runtime runtimeType) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"wait",
				fmt.Sprintf("deployment.apps/%s-deployment", runtime.workloadName),
				"--for",
				"condition=Available",
				"--namespace",
				namespace,
				"--timeout",
				"60s",
			)
		},
	}
	workloadTypeJob = workloadType{
		workloadTypeString: "job",
		basePort:           1208,
		isBatch:            true,
		waitCommand:        nil,
	}
	workloadTypePod = workloadType{
		workloadTypeString: "pod",
		basePort:           1211,
		isBatch:            false,
		waitCommand: func(namespace string, runtime runtimeType) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"wait",
				"pod",
				"--namespace",
				namespace,
				"--selector",
				fmt.Sprintf("app=%s-pod-app", runtime.workloadName),
				"--for",
				"condition=ContainersReady",
				"--timeout",
				"60s",
			)
		},
	}
	workloadTypeReplicaSet = workloadType{
		workloadTypeString: "replicaset",
		basePort:           1209,
		isBatch:            false,
		waitCommand: func(namespace string, runtime runtimeType) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"wait",
				"pod",
				"--namespace",
				namespace,
				"--selector",
				fmt.Sprintf("app=%s-replicaset-app", runtime.workloadName),
				"--for",
				"condition=ContainersReady",
				"--timeout",
				"60s",
			)
		},
	}
	workloadTypeStatefulSet = workloadType{
		workloadTypeString: "statefulset",
		basePort:           1210,
		isBatch:            false,
		waitCommand: func(namespace string, runtime runtimeType) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"rollout",
				"status",
				"statefulset",
				fmt.Sprintf("%s-statefulset", runtime.workloadName),
				"--namespace",
				namespace,
				"--timeout",
				"60s",
			)
		},
	}

	runtimeTypeNodeJs = runtimeType{
		runtimeTypeLabel: "Node.js",
		portOffset:       0,
		workloadName:     workloadNameNodeJs,
		applicationPath:  applicationPathNodeJs,
	}
	runtimeTypeJvm = runtimeType{
		runtimeTypeLabel: "JVM",
		portOffset:       100,
		workloadName:     workloadNameJvm,
		applicationPath:  applicationPathJvm,
	}
)

func rebuildAppUnderTestContainerImages() {
	rebuildNodeJsApplicationContainerImage()
	rebuildJvmApplicationContainerImage()
}

func rebuildNodeJsApplicationContainerImage() {
	rebuildApplicationContainerImage(workloadNameNodeJs+"-app", applicationPathNodeJs)
}

func rebuildJvmApplicationContainerImage() {
	rebuildApplicationContainerImage(workloadNameJvm+"-app", applicationPathJvm)
}

func rebuildApplicationContainerImage(imageName string, applicationPath string) {
	By(fmt.Sprintf("building the %s image", imageName))
	Expect(
		runAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				applicationPath,
				"-t",
				imageName,
			))).To(Succeed())

	loadImageToKindClusterIfRequired(
		ImageSpec{
			repository: imageName,
			tag:        "latest",
		}, nil,
	)
}

func uninstallNodeJsCronJob(namespace string) error {
	return runKubectlDelete(namespace, "cronjob", runtimeTypeNodeJs)
}

func uninstallJvmCronJob(namespace string) error {
	return runKubectlDelete(namespace, "cronjob", runtimeTypeJvm)
}

func installNodeJsDaemonSetWithOptOutLabel(namespace string) error {
	return runKubectlApply(
		namespace,
		manifest(runtimeTypeNodeJs, "daemonset.opt-out"),
		"daemonset",
		workloadTypeDaemonSet.waitCommand(namespace, runtimeTypeNodeJs),
	)
}

func uninstallNodeJsDaemonSet(namespace string) error {
	return runKubectlDelete(namespace, "daemonset", runtimeTypeNodeJs)
}

func uninstallJvmDaemonSet(namespace string) error {
	return runKubectlDelete(namespace, "daemonset", runtimeTypeJvm)
}

//nolint:unparam
func installNodeJsDeployment(namespace string) error {
	return installNodeJsWorkload(
		workloadTypeDeployment,
		namespace,
		"",
	)
}

func uninstallNodeJsDeployment(namespace string) error {
	return runKubectlDelete(namespace, "deployment", runtimeTypeNodeJs)
}

func uninstallJvmDeployment(namespace string) error {
	return runKubectlDelete(namespace, "deployment", runtimeTypeJvm)
}

func installNodeJsJob(namespace string, testId string) error {
	return installNodeJsWorkload(
		workloadTypeJob,
		namespace,
		testId,
	)
}

func uninstallNodeJsJob(namespace string) error {
	return runKubectlDelete(namespace, "job", runtimeTypeNodeJs)
}

func uninstallJvmJob(namespace string) error {
	return runKubectlDelete(namespace, "job", runtimeTypeJvm)
}

func installNodeJsPod(namespace string) error {
	return installNodeJsWorkload(
		workloadTypePod,
		namespace,
		"",
	)
}

func uninstallNodeJsPod(namespace string) error {
	return runKubectlDelete(namespace, "pod", runtimeTypeNodeJs)
}

func uninstallJvmPod(namespace string) error {
	return runKubectlDelete(namespace, "pod", runtimeTypeJvm)
}

func uninstallNodeJsReplicaSet(namespace string) error {
	return runKubectlDelete(namespace, "replicaset", runtimeTypeNodeJs)
}

func uninstallJvmReplicaSet(namespace string) error {
	return runKubectlDelete(namespace, "replicaset", runtimeTypeJvm)
}

func installNodeJsStatefulSet(namespace string) error {
	return installNodeJsWorkload(
		workloadTypeStatefulSet,
		namespace,
		"",
	)
}

func uninstallNodeJsStatefulSet(namespace string) error {
	return runKubectlDelete(namespace, "statefulset", runtimeTypeNodeJs)
}

func uninstallJvmStatefulSet(namespace string) error {
	return runKubectlDelete(namespace, "statefulset", runtimeTypeJvm)
}

func installNodeJsWorkload(workloadType workloadType, namespace string, testId string) error {
	return installWorkload(runtimeTypeNodeJs, workloadType, namespace, testId)
}

func installWorkload(runtime runtimeType, workloadType workloadType, namespace string, testId string) error {
	manifestFile := manifest(runtime, workloadType.workloadTypeString)
	if workloadType.isBatch {
		switch workloadType.workloadTypeString {
		case "cronjob":
			manifestFile = addTestIdToCronjobManifest(runtime, testId)
		case "job":
			manifestFile = addTestIdToJobManifest(runtime, testId)
		default:
			return fmt.Errorf("unsupported batch workload type %s", workloadType.workloadTypeString)
		}
	}

	var waitCommand *exec.Cmd
	if workloadType.waitCommand != nil {
		waitCommand = workloadType.waitCommand(namespace, runtime)
	}
	return runKubectlApply(
		namespace,
		manifestFile,
		workloadType.workloadTypeString,
		waitCommand,
	)
}

func runKubectlApply(
	namespace string,
	manifestFile string,
	workloadTypeString string,
	waitCommand *exec.Cmd,
) error {
	err := runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--namespace",
		namespace,
		"-f",
		manifestFile,
	))
	if err != nil {
		return err
	}
	if waitCommand == nil {
		return nil
	}
	return waitForApplicationToBecomeReady(workloadTypeString, waitCommand)
}

func runKubectlDelete(namespace string, workloadType string, runtime runtimeType) error {
	return runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"--ignore-not-found",
			"-f",
			manifest(runtime, workloadType),
		))
}

func removeAllTestApplications(namespace string) {
	By("uninstalling the test applications")
	Expect(uninstallNodeJsCronJob(namespace)).To(Succeed())
	Expect(uninstallJvmCronJob(namespace)).To(Succeed())
	Expect(uninstallNodeJsDaemonSet(namespace)).To(Succeed())
	Expect(uninstallJvmDaemonSet(namespace)).To(Succeed())
	Expect(uninstallNodeJsDeployment(namespace)).To(Succeed())
	Expect(uninstallJvmDeployment(namespace)).To(Succeed())
	Expect(uninstallNodeJsJob(namespace)).To(Succeed())
	Expect(uninstallJvmJob(namespace)).To(Succeed())
	Expect(uninstallNodeJsPod(namespace)).To(Succeed())
	Expect(uninstallJvmPod(namespace)).To(Succeed())
	Expect(uninstallNodeJsReplicaSet(namespace)).To(Succeed())
	Expect(uninstallJvmReplicaSet(namespace)).To(Succeed())
	Expect(uninstallNodeJsStatefulSet(namespace)).To(Succeed())
	Expect(uninstallJvmStatefulSet(namespace)).To(Succeed())
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

func addTestIdToCronjobManifest(runtime runtimeType, testId string) string {
	source := manifest(runtime, "cronjob")
	applicationManifestContentRaw, err := os.ReadFile(source)
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
	return writeManifest("cronjob", testId, updatedApplicationManifestContentRaw)
}

func addTestIdToJobManifest(runtime runtimeType, testId string) string {
	source := manifest(runtime, "job")
	applicationManifestContentRaw, err := os.ReadFile(source)
	Expect(err).ToNot(HaveOccurred())
	applicationManifestParsed := make(map[string]interface{})
	Expect(yaml.Unmarshal(applicationManifestContentRaw, &applicationManifestParsed)).To(Succeed())
	applicationManifestParsed["spec"] = addEnvVarToContainer(testId, applicationManifestParsed)
	updatedApplicationManifestContentRaw, err := yaml.Marshal(&applicationManifestParsed)
	Expect(err).ToNot(HaveOccurred())
	return writeManifest("job", testId, updatedApplicationManifestContentRaw)
}

func writeManifest(manifestFileName string, testId string, updatedApplicationManifestContentRaw []byte) string {
	target, err := os.CreateTemp(os.TempDir(), fmt.Sprintf("%s_%s.yaml", manifestFileName, testId))
	Expect(err).ToNot(HaveOccurred())
	targetName := target.Name()
	temporaryManifestFiles = append(temporaryManifestFiles, targetName)
	Expect(os.WriteFile(targetName, updatedApplicationManifestContentRaw, 0644)).To(Succeed())
	return targetName
}

func removeAllTemporaryManifests() {
	for _, tmpfile := range temporaryManifestFiles {
		_ = os.Remove(tmpfile)
	}
}

func addEnvVarToContainer(testId string, jobTemplateOrManifest map[string]interface{}) map[string]interface{} {
	jobSpec := (jobTemplateOrManifest["spec"]).(map[string]interface{})
	template := (jobSpec["template"]).(map[string]interface{})
	podSpec := (template["spec"]).(map[string]interface{})
	containers := (podSpec["containers"]).([]interface{})
	container := (containers[0]).(map[string]interface{})
	env := (container["env"]).([]interface{})

	for _, v := range env {
		envVar := (v).(map[string]interface{})
		if envVar["name"] == "TEST_ID" {
			// TEST_ID already present, we just need to update the value
			envVar["value"] = testId
			return jobSpec
		}
	}

	// no TEST_ID present, we need to add a new env var
	newEnvVar := make(map[string]string)
	newEnvVar["name"] = "TEST_ID"
	newEnvVar["value"] = testId
	env = append(env, newEnvVar)

	// since append does not modify the original slice, we need to update all the objects all the way up the hierarchy
	container["env"] = env
	containers[0] = container
	podSpec["containers"] = containers
	template["spec"] = podSpec
	jobSpec["template"] = template
	return jobSpec
}

func manifest(runtime runtimeType, manifestFileName string) string {
	return fmt.Sprintf("%s/%s.yaml", runtime.applicationPath, manifestFileName)
}
