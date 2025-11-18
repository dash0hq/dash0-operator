// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testAppHelmInstallTimeout = "60s"
)

var (
	workloadTypeCronjob = workloadType{
		workloadTypeString: "cronjob",
		isBatch:            true,
	}
	workloadTypeDaemonSet = workloadType{
		workloadTypeString: "daemonset",
		isBatch:            false,
	}
	workloadTypeDeployment = workloadType{
		workloadTypeString: "deployment",
		isBatch:            false,
	}
	workloadTypeJob = workloadType{
		workloadTypeString: "job",
		isBatch:            true,
	}
	workloadTypePod = workloadType{
		workloadTypeString: "pod",
		isBatch:            false,
	}
	workloadTypeReplicaSet = workloadType{
		workloadTypeString: "replicaset",
		isBatch:            false,
	}
	workloadTypeStatefulSet = workloadType{
		workloadTypeString: "statefulset",
		isBatch:            false,
	}

	runtimeTypeNodeJs = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelNodeJs,
		workloadName:     workloadNameNodeJs,
		helmChartPath:    chartPathNodeJs,
		helmReleaseName:  releaseNameNodeJs,
	}
	runtimeTypeJvm = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelJvm,
		workloadName:     workloadNameJvm,
		helmChartPath:    chartPathJvm,
		helmReleaseName:  releaseNameJvm,
	}
	runtimeTypeDotnet = runtimeType{
		runtimeTypeLabel: runtimeTypeLabelDotnet,
		workloadName:     workloadNameDotnet,
		helmChartPath:    chartPathDotnet,
		helmReleaseName:  releaseNameDotnet,
	}

	testAppImages = make(map[runtimeType]ImageSpec)
)

func determineTestAppImages() {
	repositoryPrefix, imageTag, pullPolicy := determineTestAppImageDefaults()
	testAppImages[runtimeTypeDotnet] =
		determineContainerImage(
			"TEST_APP_DOTNET",
			repositoryPrefix,
			"dash0-operator-dotnet-test-app",
			imageTag,
			pullPolicy,
		)
	testAppImages[runtimeTypeJvm] =
		determineContainerImage(
			"TEST_APP_JVM",
			repositoryPrefix,
			"dash0-operator-jvm-spring-boot-test-app",
			imageTag,
			pullPolicy,
		)
	testAppImages[runtimeTypeNodeJs] =
		determineContainerImage(
			"TEST_APP_NODEJS",
			repositoryPrefix,
			"dash0-operator-nodejs-20-express-test-app",
			imageTag,
			pullPolicy,
		)
}

//nolint:unparam
func installNodeJsDaemonSet(namespace string) error {
	return installTestAppWorkload(
		runtimeTypeNodeJs,
		workloadTypeDaemonSet,
		namespace,
		"",
		nil,
	)
}

func installNodeJsDaemonSetWithOptOutLabel(namespace string) error {
	return installNodeJsDaemonSetWithExtraLabels(
		namespace,
		map[string]string{
			"dash0.com/enable": "false",
		},
	)
}

func installNodeJsDaemonSetWithExtraLabels(namespace string, extraLabels map[string]string) error {
	return installTestAppWorkload(
		runtimeTypeNodeJs,
		workloadTypeDaemonSet,
		namespace,
		"",
		extraLabels,
	)
}

//nolint:unparam
func installNodeJsDeployment(namespace string) error {
	return installTestAppWorkload(
		runtimeTypeNodeJs,
		workloadTypeDeployment,
		namespace,
		"",
		nil,
	)
}

func installNodeJsJob(namespace string, testId string) error {
	return installTestAppWorkload(
		runtimeTypeNodeJs,
		workloadTypeJob,
		namespace,
		testId,
		nil,
	)
}

func installNodeJsPod(namespace string) error {
	return installTestAppWorkload(
		runtimeTypeNodeJs,
		workloadTypePod,
		namespace,
		"",
		nil,
	)
}

func installNodeJsStatefulSet(namespace string) error {
	return installTestAppWorkload(
		runtimeTypeNodeJs,
		workloadTypeStatefulSet,
		namespace,
		"",
		nil,
	)
}

// installTestAppWorkload runs helm install for a single workload, that is, for one particular runtime (Node.js, JVM,
// ...) and one particular workload type (i.e. Deployment, DaemonSet, ...).
func installTestAppWorkload(
	runtime runtimeType,
	workloadType workloadType,
	namespace string,
	testId string,
	extraLabels map[string]string,
) error {
	helmSetParams := compileWorkloadTypeSetParams(workloadType)
	helmSetParams = addTestAppImageSetParams(runtime, helmSetParams)

	// Add testId for batch workloads
	if workloadType.isBatch && testId != "" {
		helmSetParams = append(helmSetParams, "--set", fmt.Sprintf("%s.testId=%s", workloadType.workloadTypeString, testId))
	}
	if len(extraLabels) > 0 {
		labelsJson, err := json.Marshal(extraLabels)
		Expect(err).ToNot(HaveOccurred())
		helmSetParams = append(
			helmSetParams,
			"--set-json",
			fmt.Sprintf("%s.labels=%s", workloadType.workloadTypeString, string(labelsJson)),
		)
	}

	return runTestAppHelmInstall(
		namespace,
		runtime,
		helmSetParams,
	)
}

// installTestAppWorkloads runs helm install for a single Helm chart, deploying multiple workload types for one
// particular runtime at once; for exapmle deploy the Node.js Deployment, DaemonSet and CronJob in one go. Workloads for
// different runtimes cannot be installed with a single installTestAppWorkloads call, since they are deployed via
// different Helm charts.
func installTestAppWorkloads(
	runtime runtimeType,
	workloadTypes []workloadType,
	namespace string,
	testIds testIdMap,
) error {
	helmSetParams := compileWorkloadTypeSetParams(workloadTypes...)
	helmSetParams = addTestAppImageSetParams(runtime, helmSetParams)

	// Add testIds for batch workloads
	for _, wt := range workloadTypes {
		if wt.isBatch {
			if testId := getTestIdFromMap(testIds, runtime, wt); testId != "" {
				helmSetParams =
					append(
						helmSetParams,
						"--set",
						fmt.Sprintf("%s.testId=%s", wt.workloadTypeString, testId),
					)
			}
		}
	}

	return runTestAppHelmInstall(
		namespace,
		runtime,
		helmSetParams,
	)
}

func runTestAppHelmInstall(
	namespace string,
	runtime runtimeType,
	setValues []string,
) error {
	args := []string{
		"install",
		"--namespace",
		namespace,
		"--wait",
		"--timeout",
		testAppHelmInstallTimeout,
		runtime.helmReleaseName,
		runtime.helmChartPath,
	}
	args = append(args, setValues...)

	return runAndIgnoreOutput(exec.Command("helm", args...))
}

func runTestAppHelmUninstall(namespace string, releaseName string) error {
	return runAndIgnoreOutput(
		exec.Command(
			"helm",
			"uninstall",
			releaseName,
			"--namespace",
			namespace,
			"--ignore-not-found",
		), false)
}

func uninstallDotnetRelease(namespace string) error {
	return runTestAppHelmUninstall(namespace, runtimeTypeDotnet.helmReleaseName)
}

func uninstallJvmRelease(namespace string) error {
	return runTestAppHelmUninstall(namespace, runtimeTypeJvm.helmReleaseName)
}

func uninstallNodeJsRelease(namespace string) error {
	return runTestAppHelmUninstall(namespace, runtimeTypeNodeJs.helmReleaseName)
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
	Expect(uninstallDotnetRelease(namespace)).To(Succeed())
	Expect(uninstallJvmRelease(namespace)).To(Succeed())
	Expect(uninstallNodeJsRelease(namespace)).To(Succeed())
}

// compileWorkloadTypeSetParams creates a list of Helm --set flags for the test app Helm chart to deploy a specific set
// of workload types.
func compileWorkloadTypeSetParams(workloadTypes ...workloadType) []string {
	setValues := make([]string, 0, len(workloadTypes)*2)
	for _, wt := range workloadTypes {
		setValues = append(setValues, "--set", fmt.Sprintf("%s.enabled=true", wt.workloadTypeString))
	}
	return setValues
}

// addTestAppImageSetParams adds image-related --set parameters according to TEST_IMAGE_REPOSITORY_PREFIX,
// TEST_IMAGE_TAG, TEST_IMAGE_PULL_POLICY, or TEST_APP_NODEJS_IMAGE_REPOSITORY etc.
func addTestAppImageSetParams(runtime runtimeType, helmSetParams []string) []string {
	testAppImageSpec, ok := testAppImages[runtime]
	Expect(ok).To(BeTrue())
	helmSetParams = append(helmSetParams, "--set", fmt.Sprintf("image.repository=%s", testAppImageSpec.repository))
	helmSetParams = append(helmSetParams, "--set", fmt.Sprintf("image.tag=%s", testAppImageSpec.tag))
	helmSetParams = append(helmSetParams, "--set", fmt.Sprintf("image.pullPolicy=%s", testAppImageSpec.pullPolicy))
	return helmSetParams
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
