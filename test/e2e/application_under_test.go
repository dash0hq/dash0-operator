// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type workloadType struct {
	workloadTypeString string
	port               int
	isBatch            bool
	waitCommand        func(string) *exec.Cmd
}

const (
	applicationUnderTestNamespace = "e2e-application-under-test-namespace"
	applicationPath               = "test-resources/node.js/express"
)

var (
	temporaryManifestFiles []string

	workloadTypeCronjob = workloadType{
		workloadTypeString: "cronjob",
		port:               1205,
		isBatch:            true,
		waitCommand:        nil,
	}
	workloadTypeDaemonSet = workloadType{
		workloadTypeString: "daemonset",
		port:               1206,
		isBatch:            false,
		waitCommand: func(namespace string) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"rollout",
				"status",
				"daemonset",
				"dash0-operator-nodejs-20-express-test-daemonset",
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
		port:               1207,
		waitCommand: func(namespace string) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"wait",
				"deployment.apps/dash0-operator-nodejs-20-express-test-deployment",
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
		port:               1208,
		isBatch:            true,
		waitCommand:        nil,
	}
	workloadTypePod = workloadType{
		workloadTypeString: "pod",
		port:               1211,
		isBatch:            false,
		waitCommand: func(namespace string) *exec.Cmd {
			return exec.Command(
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
			)
		},
	}
	workloadTypeReplicaSet = workloadType{
		workloadTypeString: "replicaset",
		port:               1209,
		isBatch:            false,
		waitCommand: func(namespace string) *exec.Cmd {
			return exec.Command(
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
			)
		},
	}
	workloadTypeStatefulSet = workloadType{
		workloadTypeString: "statefulset",
		port:               1210,
		isBatch:            false,
		waitCommand: func(namespace string) *exec.Cmd {
			return exec.Command(
				"kubectl",
				"rollout",
				"status",
				"statefulset",
				"dash0-operator-nodejs-20-express-test-statefulset",
				"--namespace",
				namespace,
				"--timeout",
				"60s",
			)
		},
	}
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

	loadImageToKindClusterIfRequired(
		ImageSpec{
			repository: "dash0-operator-nodejs-20-express-test-app",
			tag:        "latest",
		}, nil,
	)
}

func uninstallNodeJsCronJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "cronjob")
}

func installNodeJsDaemonSetWithOptOutLabel(namespace string) error {
	return installNodeJsApplication(
		namespace,
		manifest("daemonset.opt-out"),
		"daemonset",
		workloadTypeDaemonSet.waitCommand(namespace),
	)
}

func uninstallNodeJsDaemonSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "daemonset")
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
	return uninstallNodeJsApplication(namespace, "deployment")
}

func installNodeJsJob(namespace string, testId string) error {
	return installNodeJsWorkload(
		workloadTypeJob,
		namespace,
		testId,
	)
}

func uninstallNodeJsJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "job")
}

func installNodeJsPod(namespace string) error {
	return installNodeJsWorkload(
		workloadTypePod,
		namespace,
		"",
	)
}

func uninstallNodeJsPod(namespace string) error {
	return uninstallNodeJsApplication(namespace, "pod")
}

func uninstallNodeJsReplicaSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "replicaset")
}

func installNodeJsStatefulSet(namespace string) error {
	return installNodeJsWorkload(
		workloadTypeStatefulSet,
		namespace,
		"",
	)
}

func uninstallNodeJsStatefulSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "statefulset")
}

func installNodeJsWorkload(workloadType workloadType, namespace string, testId string) error {
	manifestFile := manifest(workloadType.workloadTypeString)
	if workloadType.isBatch {
		switch workloadType.workloadTypeString {
		case "cronjob":
			manifestFile = addTestIdToCronjobManifest(testId)
		case "job":
			manifestFile = addTestIdToJobManifest(testId)
		default:
			return fmt.Errorf("unsupported batch workload type %s", workloadType.workloadTypeString)
		}
	}

	var waitCommand *exec.Cmd
	if workloadType.waitCommand != nil {
		waitCommand = workloadType.waitCommand(namespace)
	}
	return installNodeJsApplication(
		namespace,
		manifestFile,
		workloadType.workloadTypeString,
		waitCommand,
	)
}

func installNodeJsApplication(
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

func addTestIdToCronjobManifest(testId string) string {
	source := manifest("cronjob")
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

func addTestIdToJobManifest(testId string) string {
	source := manifest("job")
	applicationManifestContentRaw, err := os.ReadFile(source)
	Expect(err).ToNot(HaveOccurred())
	applicationManifestParsed := make(map[string]interface{})
	Expect(yaml.Unmarshal(applicationManifestContentRaw, &applicationManifestParsed)).To(Succeed())
	applicationManifestParsed["spec"] = addEnvVarToContainer(testId, applicationManifestParsed)
	updatedApplicationManifestContentRaw, err := yaml.Marshal(&applicationManifestParsed)
	Expect(err).ToNot(HaveOccurred())
	return writeManifest("job", testId, updatedApplicationManifestContentRaw)
}

func writeManifest(workloadTypeString string, testId string, updatedApplicationManifestContentRaw []byte) string {
	target, err := os.CreateTemp(os.TempDir(), fmt.Sprintf("%s_%s.yaml", workloadTypeString, testId))
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

func manifest(workloadType string) string {
	return fmt.Sprintf("%s/%s.yaml", applicationPath, workloadType)
}

func sendRequest(g Gomega, workloadType string, port int, httpPathWithQuery string) {
	executeHttpRequest(
		g,
		workloadType,
		port,
		httpPathWithQuery,
		200,
		"We make Observability easy for every developer.",
	)
}

func sendReadyProbe(g Gomega, workloadType string, port int) {
	executeHttpRequest(
		g,
		workloadType,
		port,
		"/ready",
		204,
		"",
	)
}

func executeHttpRequest(
	g Gomega,
	workloadType string,
	port int,
	httpPathWithQuery string,
	expectedStatus int,
	expectedBody string,
) {
	url := fmt.Sprintf("http://localhost:%d%s", port, httpPathWithQuery)
	if isKindCluster() {
		url = fmt.Sprintf("http://%s/%s%s", kindClusterIngressIp, workloadType, httpPathWithQuery)
	}
	httpClient := http.Client{
		Timeout: 500 * time.Millisecond,
	}
	response, err := httpClient.Get(url)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = response.Body.Close()
	}()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		e2ePrint("could not read http response from %s: %s\n", url, err.Error())
	}
	g.Expect(err).NotTo(HaveOccurred())
	status := response.StatusCode
	if expectedBody != "" {
		g.Expect(
			string(responseBody)).To(
			ContainSubstring(expectedBody),
			fmt.Sprintf("unexpected response body for workload type %s at %s, HTTP %d", workloadType, url, status),
		)
	}
	if expectedStatus > 0 {
		g.Expect(status).To(
			Equal(expectedStatus),
			fmt.Sprintf("unexpected status for workload type %s at %s", workloadType, url),
		)
	}
}
