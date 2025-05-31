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
		runtimeTypeLabel: runtimeTypeLabelNodeJs,
		portOffset:       0,
		workloadName:     workloadNameNodeJs,
		applicationPath:  applicationPathNodeJs,
	}
	runtimeTypeJvm = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelJvm,
		portOffset:       100,
		workloadName:     workloadNameJvm,
		applicationPath:  applicationPathJvm,
	}
	runtimeTypeDotnet = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelDotnet,
		portOffset:       200,
		workloadName:     workloadNameDotnet,
		applicationPath:  applicationPathDotnet,
	}
)

func rebuildAppUnderTestContainerImages() {
	rebuildNodeJsApplicationContainerImage()
	rebuildJvmApplicationContainerImage()
	rebuildDotnetApplicationContainerImage()
}

func rebuildNodeJsApplicationContainerImage() {
	rebuildApplicationContainerImage(workloadNameNodeJs+"-app", applicationPathNodeJs)
}

func rebuildJvmApplicationContainerImage() {
	rebuildApplicationContainerImage(workloadNameJvm+"-app", applicationPathJvm)
}

func rebuildDotnetApplicationContainerImage() {
	rebuildApplicationContainerImage(workloadNameDotnet+"-app", applicationPathDotnet)
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

func installNodeJsDaemonSetWithOptOutLabel(namespace string) error {
	return runKubectlApply(
		namespace,
		manifest(runtimeTypeNodeJs, "daemonset.opt-out"),
		runtimeTypeNodeJs.runtimeTypeLabel,
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

func uninstallDotnetDaemonSet(namespace string) error {
	return runKubectlDelete(namespace, "daemonset", runtimeTypeDotnet)
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

func uninstallDotnetDeployment(namespace string) error {
	return runKubectlDelete(namespace, "deployment", runtimeTypeDotnet)
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

func uninstallDotnetPod(namespace string) error {
	return runKubectlDelete(namespace, "pod", runtimeTypeDotnet)
}

func uninstallNodeJsReplicaSet(namespace string) error {
	return runKubectlDelete(namespace, "replicaset", runtimeTypeNodeJs)
}

func uninstallJvmReplicaSet(namespace string) error {
	return runKubectlDelete(namespace, "replicaset", runtimeTypeJvm)
}

func uninstallDotnetReplicaSet(namespace string) error {
	return runKubectlDelete(namespace, "replicaset", runtimeTypeDotnet)
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

func uninstallDotnetStatefulSet(namespace string) error {
	return runKubectlDelete(namespace, "statefulset", runtimeTypeDotnet)
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
		runtime.runtimeTypeLabel,
		workloadType.workloadTypeString,
		waitCommand,
	)
}

func runKubectlApply(
	namespace string,
	manifestFile string,
	runtimeLabel string,
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
	return waitForApplicationToBecomeReady(runtimeLabel, workloadTypeString, waitCommand)
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
		), false)
}

func killBatchJobsAndPods(namespace string) {
	By("deleting job spawned by a cronjob")
	Expect(runAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"--ignore-not-found",
			"job",
			"-l",
			"app.kubernetes.io/name=test-cronjob",
			"--wait",
		))).To(Succeed())
	By("deleting cronjob/job pods")
	selectors := []string{
		"app.kubernetes.io/name=test-cronjob",
		"app.kubernetes.io/name=test-job",
	}
	for _, selector := range selectors {
		Expect(runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"delete",
				"--namespace",
				namespace,
				"--ignore-not-found",
				"pod",
				"-l",
				selector,
				"--wait",
			))).To(Succeed())
	}
}

func removeAllTestApplications(namespace string) {
	By("uninstalling the test applications")
	Expect(uninstallNodeJsCronJob(namespace)).To(Succeed())
	Expect(uninstallNodeJsDaemonSet(namespace)).To(Succeed())
	Expect(uninstallJvmDaemonSet(namespace)).To(Succeed())
	Expect(uninstallDotnetDaemonSet(namespace)).To(Succeed())
	Expect(uninstallNodeJsDeployment(namespace)).To(Succeed())
	Expect(uninstallJvmDeployment(namespace)).To(Succeed())
	Expect(uninstallDotnetDeployment(namespace)).To(Succeed())
	Expect(uninstallNodeJsJob(namespace)).To(Succeed())
	Expect(uninstallNodeJsPod(namespace)).To(Succeed())
	Expect(uninstallJvmPod(namespace)).To(Succeed())
	Expect(uninstallDotnetPod(namespace)).To(Succeed())
	Expect(uninstallNodeJsReplicaSet(namespace)).To(Succeed())
	Expect(uninstallJvmReplicaSet(namespace)).To(Succeed())
	Expect(uninstallDotnetReplicaSet(namespace)).To(Succeed())
	Expect(uninstallNodeJsStatefulSet(namespace)).To(Succeed())
	Expect(uninstallJvmStatefulSet(namespace)).To(Succeed())
	Expect(uninstallDotnetStatefulSet(namespace)).To(Succeed())
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
