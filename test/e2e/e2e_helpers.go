// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"

	"github.com/google/uuid"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/dash0hq/dash0-operator/internal/util"

	testUtil "github.com/dash0hq/dash0-operator/test/util"
)

const (
	certmanagerVersion             = "v1.14.5"
	operatorHelmReleaseName        = "e2e-tests-operator-helm-release"
	tracesJsonMaxLineLength        = 1_048_576
	verifyTelemetryTimeout         = 90 * time.Second
	verifyTelemetryPollingInterval = 500 * time.Millisecond
	dash0CustomResourceName        = "dash0-sample"
)

var (
	traceUnmarshaller = &ptrace.JSONUnmarshaler{}
	requiredPorts     = []int{1207, 4317, 4318}
)

func CheckIfRequiredPortsAreBlocked() {
	portsCurrentlyInUseByKubernetesServices, err := Run(
		exec.Command(
			"kubectl",
			"get",
			"svc",
			"--all-namespaces",
			"-o",
			"go-template='{{range .items}}{{range.spec.ports}}{{if .port}}{{.port}}{{\"\\n\"}}{{end}}{{end}}{{end}}'",
		))
	Expect(err).NotTo(HaveOccurred())
	portsCurrentlyInUseArray := GetNonEmptyLines(portsCurrentlyInUseByKubernetesServices)
	messages := make([]string, 0)
	foundBlockedPort := false
	for _, usedPortStr := range portsCurrentlyInUseArray {
		usedPort, err := strconv.Atoi(usedPortStr)
		if err != nil {
			continue
		}
		for _, requiredPort := range requiredPorts {
			if usedPort == requiredPort {
				messages = append(messages,
					fmt.Sprintf(
						"Port %d is required by the test suite, but it is already in use by a Kubernetes "+
							"service. Please check for conflicting deployed serivces.",
						requiredPort,
					))
				foundBlockedPort = true
			}
		}
	}
	if foundBlockedPort {
		messages = append(messages,
			"Note: If you have used the scripts for manual testing in test-resources, running "+
				"test-resources/bin/test-cleanup.sh might help removing all left-over Kubernetes objects.")
		Fail(strings.Join(messages, "\n"))
	}
}

func RenderTemplates() {
	By("render yaml templates")
	Expect(RunAndIgnoreOutput(exec.Command("test-resources/bin/render-templates.sh"))).To(Succeed())
}

func SetKubeContext(kubeContextForTest string) (bool, string) {
	By("reading current kubectx")
	kubectxOutput, err := Run(exec.Command("kubectx", "-c"))
	Expect(err).NotTo(HaveOccurred())
	originalKubeContext := strings.TrimSpace(kubectxOutput)

	if originalKubeContext != kubeContextForTest {
		By("switching to kubectx docker-desktop, previous context " + originalKubeContext + " will be restored later")
		Expect(RunAndIgnoreOutput(exec.Command("kubectx", "docker-desktop"))).To(Succeed())
		return true, originalKubeContext
	} else {
		return false, originalKubeContext
	}
}

func RevertKubeCtx(originalKubeContext string) {
	By("switching back to original kubectx " + originalKubeContext)
	output, err := Run(exec.Command("kubectx", originalKubeContext))
	if err != nil {
		fmt.Fprint(GinkgoWriter, err.Error())
	}
	fmt.Fprint(GinkgoWriter, output)
}

func EnsureCertManagerIsInstalled() bool {
	err := RunAndIgnoreOutput(exec.Command("kubectl", "get", "ns", "cert-manager"), false)
	if err != nil {
		By("installing the cert-manager")
		fmt.Fprint(GinkgoWriter,
			"Hint: To get a faster feedback cycle on e2e tests, deploy cert-manager once via "+
				"test-resources/cert-manager/deploy.sh. If the e2e tests find an existing cert-manager namespace, they "+
				"will not deploy cert-manager and they will also not undeploy it after running the test suite.\n",
		)
		Expect(installCertManager()).To(Succeed())
		return true
	} else {
		fmt.Fprint(GinkgoWriter,
			"The cert-manager namespace exists, assuming cert-manager has been deployed already.\n",
		)
	}
	return false
}

func installCertManager() error {
	repoList, err := Run(exec.Command("helm", "repo", "list"))
	if err != nil {
		return err
	}
	if !strings.Contains(repoList, "jetstack") {
		fmt.Fprintf(GinkgoWriter, "The helm repo for cert-manager has not been found, adding it now.\n")
		if err := RunAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				"jetstack",
				"https://charts.jetstack.io",
				"--force-update",
			)); err != nil {
			return err
		}
		fmt.Fprintf(GinkgoWriter, "Running helm repo update.\n")
		if err = RunAndIgnoreOutput(exec.Command("helm", "repo", "update")); err != nil {
			return err
		}
	}

	if err := RunAndIgnoreOutput(exec.Command(
		"helm",
		"install",
		"cert-manager",
		"jetstack/cert-manager",
		"--namespace",
		"cert-manager",
		"--create-namespace",
		"--version",
		certmanagerVersion,
		"--set",
		"installCRDs=true",
		"--timeout",
		"5m",
	)); err != nil {
		return err
	}

	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	if err := RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/cert-manager-webhook",
			"--for",
			"condition=Available",
			"--namespace",
			"cert-manager",
			"--timeout",
			"5m",
		)); err != nil {
		return err
	}
	if err := RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/cert-manager-cainjector",
			"--for",
			"condition=Available",
			"--namespace",
			"cert-manager",
			"--timeout",
			"60s",
		)); err != nil {
		return err
	}
	if err := RunAndIgnoreOutput(
		exec.Command(
			"kubectl",
			"wait",
			"deployment.apps/cert-manager-cainjector",
			"--for",
			"condition=Available",
			"--namespace",
			"cert-manager",
			"--timeout",
			"60s",
		)); err != nil {
		return err
	}
	return nil
}

func UninstallCertManagerIfApplicable(certManagerHasBeenInstalled bool) {
	if certManagerHasBeenInstalled {
		By("uninstalling the cert-manager bundle")
		uninstallCertManager()
	} else {
		fmt.Fprint(GinkgoWriter,
			"Note: The e2e test suite did not install cert-manager, thus it will also not uninstall it.\n",
		)
	}
}

func uninstallCertManager() {
	if err := RunAndIgnoreOutput(exec.Command(
		"helm",
		"uninstall",
		"cert-manager",
		"--namespace",
		"cert-manager",
		"--ignore-not-found",
	)); err != nil {
		warnError(err)
	}

	if err := RunAndIgnoreOutput(
		exec.Command(
			"kubectl", "delete", "namespace", "cert-manager", "--ignore-not-found")); err != nil {
		warnError(err)
	}
}

func RecreateNamespace(namespace string) {
	By(fmt.Sprintf("(re)creating namespace %s", namespace))
	output, err := Run(exec.Command("kubectl", "get", "ns", namespace))
	if err != nil {
		if strings.Contains(output, "(NotFound)") {
			// The namespace does not exist, that's fine, we will create it further down.
		} else {
			Fail(fmt.Sprintf("kubectl get ns %s failed with unexpected error: %v", namespace, err))
		}
	} else {
		ExpectWithOffset(1,
			RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace))).To(Succeed())
		ExpectWithOffset(1,
			RunAndIgnoreOutput(
				exec.Command("kubectl", "wait", "--for=delete", "ns", namespace, "--timeout=60s"))).To(Succeed())
	}

	ExpectWithOffset(1,
		RunAndIgnoreOutput(exec.Command("kubectl", "create", "ns", namespace))).To(Succeed())
}

func RebuildOperatorControllerImage(operatorImageRepository string, operatorImageTag string) bool {
	By("building the operator controller image")
	return ExpectWithOffset(1,
		RunAndIgnoreOutput(
			exec.Command(
				"make",
				"docker-build",
				fmt.Sprintf("IMG_REPOSITORY=%s", operatorImageRepository),
				fmt.Sprintf("IMG_TAG=%s", operatorImageTag),
			))).To(Succeed())
}

func RebuildDash0InstrumentationImage() bool {
	By("building the dash0-instrumentation image")
	return Expect(RunAndIgnoreOutput(exec.Command("images/dash0-instrumentation/build.sh"))).To(Succeed())
}

func DeployOperatorWithCollectorAndClearExportedTelemetry(
	operatorNamespace string,
	imageRepository string,
	imageTag string,
) {
	By("removing old captured telemetry files")
	_ = os.Remove("test-resources/e2e-test-volumes/collector-received-data/traces.jsonl")
	_ = os.Remove("test-resources/e2e-test-volumes/collector-received-data/metrics.jsonl")
	_ = os.Remove("test-resources/e2e-test-volumes/collector-received-data/logs.jsonl")

	ensureOtelCollectorHelmRepoIsInstalled()

	By("deploying the controller-manager")
	output, err := Run(
		exec.Command(
			"helm",
			"install",
			"--namespace",
			operatorNamespace,
			"--create-namespace",
			"--values", "test-resources/helm/e2e.values.yaml",
			// The image repo, tag and pull policy are also defined in test-resources/helm/e2e.values.yaml, but we want
			// to keep them in sync with the args passed to make docker build in the BeforeAll hook when building the
			// container image, thus we pass them here explicitly.
			"--set", fmt.Sprintf("operator.image.repository=%s", imageRepository),
			"--set", fmt.Sprintf("operator.image.tag=%s", imageTag),
			"--set", "operator.developmentMode=true",
			operatorHelmReleaseName,
			"helm-chart/dash0-operator",
		))
	Expect(err).NotTo(HaveOccurred())
	fmt.Fprintf(GinkgoWriter, "output of helm install:\n%s", output)

	var controllerPodName string
	By("validating that the controller-manager pod is running as expected")
	verifyControllerUp := func() error {
		cmd := exec.Command("kubectl", "get",
			"pods", "-l", "control-plane=controller-manager",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", operatorNamespace,
		)

		podOutput, err := Run(cmd, false)
		Expect(err).NotTo(HaveOccurred())
		podNames := GetNonEmptyLines(podOutput)
		if len(podNames) != 1 {
			return fmt.Errorf("expect 1 controller pods running, but got %d -- %s", len(podNames), podOutput)
		}
		controllerPodName = podNames[0]
		Expect(controllerPodName).To(ContainSubstring("controller-manager"))

		cmd = exec.Command("kubectl", "get",
			"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
			"-n", operatorNamespace,
		)
		status, err := Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		if status != "Running" {
			return fmt.Errorf("controller pod in %s status", status)
		}
		return nil
	}

	EventuallyWithOffset(1, verifyControllerUp, 120*time.Second, time.Second).Should(Succeed())

	// verify that the OTel collector is also up and running
	ExpectWithOffset(1, RunAndIgnoreOutput(
		exec.Command("kubectl",
			"rollout",
			"status",
			"daemonset",
			fmt.Sprintf("%s-opentelemetry-collector-agent", operatorHelmReleaseName),
			"--namespace",
			operatorNamespace,
			"--timeout",
			"60s",
		))).To(Succeed())
}

func ensureOtelCollectorHelmRepoIsInstalled() {
	By("checking whether OpenTelemetry Helm charts have been installed")
	repoList, err := Run(exec.Command("helm", "repo", "list"))
	Expect(err).NotTo(HaveOccurred())
	if !strings.Contains(repoList, "open-telemetry") {
		fmt.Fprintf(GinkgoWriter, "The helm repo for open-telemetry has not been found, adding it now.\n")
		Expect(RunAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				"open-telemetry",
				"https://open-telemetry.github.io/opentelemetry-helm-charts",
				"--force-update",
			))).To(Succeed())

		Expect(RunAndIgnoreOutput(exec.Command("helm", "repo", "update"))).To(Succeed())
	} else {
		fmt.Fprintf(GinkgoWriter, "The helm repo for open-telemetry is already installed.\n")
	}
}

func UndeployOperatorAndCollector(operatorNamespace string) {
	By("undeploying the controller-manager")
	ExpectWithOffset(1,
		RunAndIgnoreOutput(
			exec.Command(
				"helm",
				"uninstall",
				"--namespace",
				operatorNamespace,
				"--ignore-not-found",
				operatorHelmReleaseName,
			))).To(Succeed())

	// We need to delete the operator namespace and wait until the namespace is really gone, otherwise the next test
	// case/suite that tries to create the operator will run into issues when trying to recreate the namespace which is
	// still in the process of being deleted.
	ExpectWithOffset(1,
		RunAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"delete",
				"ns",
				"--ignore-not-found",
				operatorNamespace,
			))).To(Succeed())

	VerifyDash0OperatorReleaseIsNotInstalled(Default, operatorNamespace)
}

func VerifyDash0OperatorReleaseIsNotInstalled(g Gomega, operatorNamespace string) {
	g.ExpectWithOffset(1, RunAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--for=delete",
		fmt.Sprintf("namespace/%s", operatorNamespace),
		"--timeout=60s",
	))).To(Succeed())
}

func DeployDash0CustomResource(namespace string) {
	ExpectWithOffset(1,
		RunAndIgnoreOutput(exec.Command(
			"kubectl", "apply", "-n", namespace, "-k", "config/samples"))).To(Succeed())
}

func UndeployDash0CustomResource(namespace string) {
	// remove the resource
	ExpectWithOffset(1,
		RunAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"-k",
			"config/samples",
			"--ignore-not-found",
		))).To(Succeed())
}

func VerifyDash0CustomResourceDoesNotExist(g Gomega, namespace string) {
	output, err := Run(exec.Command(
		"kubectl",
		"get",
		"--namespace",
		namespace,
		"--ignore-not-found",
		"dash0",
		dash0CustomResourceName,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(output).NotTo(ContainSubstring(dash0CustomResourceName))

}

func RebuildNodeJsApplicationContainerImage() {
	By("building the dash0-operator-nodejs-20-express-test-app image")
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"test-resources/node.js/express",
				"-t",
				"dash0-operator-nodejs-20-express-test-app",
			))).To(Succeed())
}

func InstallNodeJsCronJob(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"cronjob",
		nil,
	)
}

func UninstallNodeJsCronJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "cronjob")
}

func InstallNodeJsDaemonSet(namespace string) error {
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

func UninstallNodeJsDaemonSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "daemonset")
}

func InstallNodeJsDeployment(namespace string) error {
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

func UninstallNodeJsDeployment(namespace string) error {
	return uninstallNodeJsApplication(namespace, "deployment")
}

func InstallNodeJsJob(namespace string) error {
	return installNodeJsApplication(
		namespace,
		"job",
		nil,
	)
}

func UninstallNodeJsJob(namespace string) error {
	return uninstallNodeJsApplication(namespace, "job")
}

func InstallNodeJsPod(namespace string) error {
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

func UninstallNodeJsPod(namespace string) error {
	return uninstallNodeJsApplication(namespace, "pod")
}

func InstallNodeJsReplicaSet(namespace string) error {
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

func UninstallNodeJsReplicaSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "replicaset")
}

func InstallNodeJsStatefulSet(namespace string) error {
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

func UninstallNodeJsStatefulSet(namespace string) error {
	return uninstallNodeJsApplication(namespace, "statefulset")
}

func RemoveAllTestApplications(namespace string) {
	By("uninstalling the test applications")
	Expect(UninstallNodeJsCronJob(namespace)).To(Succeed())
	Expect(UninstallNodeJsDaemonSet(namespace)).To(Succeed())
	Expect(UninstallNodeJsDeployment(namespace)).To(Succeed())
	Expect(UninstallNodeJsJob(namespace)).To(Succeed())
	Expect(UninstallNodeJsPod(namespace)).To(Succeed())
	Expect(UninstallNodeJsReplicaSet(namespace)).To(Succeed())
	Expect(UninstallNodeJsStatefulSet(namespace)).To(Succeed())
}

func installNodeJsApplication(namespace string, kind string, waitCommand *exec.Cmd) error {
	err := RunAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"--namespace",
		namespace,
		"-f",
		fmt.Sprintf("test-resources/node.js/express/%s.yaml", kind),
	))
	if err != nil {
		return err
	}
	if waitCommand == nil {
		return nil
	}
	return RunAndIgnoreOutput(waitCommand)
}

func uninstallNodeJsApplication(namespace string, kind string) error {
	return RunAndIgnoreOutput(
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

func DeleteTestIdFiles() {
	_ = os.Remove("test-resources/e2e-test-volumes/test-uuid/cronjob.test.id")
	_ = os.Remove("test-resources/e2e-test-volumes/test-uuid/job.test.id")
}

func VerifyThatWorkloadHasBeenInstrumented(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	instrumentationBy string,
) string {
	By(fmt.Sprintf("%s: waiting for the workload to get instrumented (polling its labels and events to check)",
		workloadType))
	Eventually(func(g Gomega) {
		VerifyLabels(g, namespace, workloadType, true, instrumentationBy)
		VerifySuccessfulInstrumentationEvent(g, namespace, workloadType, instrumentationBy)
	}, 20*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

	By(fmt.Sprintf("%s: waiting for spans to be captured", workloadType))
	var testId string
	if isBatch {
		testIdTimeoutSeconds := 30
		if workloadType == "cronjob" {
			// Cronjob pods are only scheduled once a minute, so we might need to wait a while for a job to be started
			// and for the ID to become available, hence increasing the timeout for the surrounding "Eventually".
			testIdTimeoutSeconds = 100
		}
		By(fmt.Sprintf("waiting for the test ID file to be written by the %s under test", workloadType))
		Eventually(func(g Gomega) {
			// For resource types like batch jobs/cron jobs, the application under test generates the test ID and writes it
			// to a volume that maps to a host path. We read the test ID from the host path and use it to verify the spans.
			testIdBytes, err := os.ReadFile(fmt.Sprintf("test-resources/e2e-test-volumes/test-uuid/%s.test.id", workloadType))
			g.Expect(err).NotTo(HaveOccurred())
			testId = string(testIdBytes)
			By(fmt.Sprintf("%s: test ID file is available (test ID: %s)", workloadType, testId))

		}, time.Duration(testIdTimeoutSeconds)*time.Second, 200*time.Millisecond).Should(Succeed())
	} else {
		// For resource types that are available as a service (daemonset, deployment etc.) we send HTTP requests with
		// a unique ID as a query parameter. When checking the produced spans that the OTel collector writes to disk via
		// the file exporter, we can verify that the span is actually from the currently running test case by inspecting
		// the http.target span attribute. This guarantees that we do not accidentally pass the test due to a span from
		// a previous test case.
		testIdUuid := uuid.New()
		testId = testIdUuid.String()
		By(fmt.Sprintf("%s: test ID: %s", workloadType, testId))
	}

	httpPathWithQuery := fmt.Sprintf("/dash0-k8s-operator-test?id=%s", testId)
	Eventually(func(g Gomega) {
		verifySpans(g, isBatch, workloadType, port, httpPathWithQuery)
	}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
	By(fmt.Sprintf("%s: matching spans have been received", workloadType))
	return testId
}

func VerifyThatInstrumentationHasBeenReverted(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	instrumentationBy string,
) {
	By(fmt.Sprintf(
		"%s: waiting for the instrumentation to get removed from the workload (polling its labels and events to check)",
		workloadType))
	Eventually(func(g Gomega) {
		VerifyNoDash0Labels(g, namespace, workloadType)
		VerifySuccessfulUninstrumentationEvent(g, namespace, workloadType, instrumentationBy)
	}, 30*time.Second, verifyTelemetryPollingInterval).Should(Succeed())

	// Add some buffer time between the workloads being restarted and verifying that no spans are produced/captured.
	time.Sleep(10 * time.Second)

	secondsToCheckForSpans := 20
	if workloadType == "cronjob" {
		// Pod for cron jobs only get scheduled once a minute, since the cronjob schedule format does not allow for jobs
		// starting every second. Thus, to make the test valid, we need to monitor for spans a little bit longer than
		// for other workload types.
		secondsToCheckForSpans = 80
	}
	httpPathWithQuery := fmt.Sprintf("/dash0-k8s-operator-test?id=%s", testId)
	By(
		fmt.Sprintf("%s: verifying that spans are no longer being captured (checking for %d seconds)",
			workloadType,
			secondsToCheckForSpans,
		))
	Consistently(func(g Gomega) {
		verifyNoSpans(isBatch, workloadType, port, httpPathWithQuery)
	}, time.Duration(secondsToCheckForSpans)*time.Second, 1*time.Second).Should(Succeed())

	By(fmt.Sprintf("%s: matching spans are no longer captured", workloadType))
}

func VerifyThatFailedInstrumentationAttemptLabelsHaveBeenRemovedRemoved(namespace string, workloadType string) {
	By("waiting for the labels to get removed from the workload")
	Eventually(func(g Gomega) {
		VerifyNoDash0Labels(g, namespace, workloadType)
	}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
}

func VerifyLabels(g Gomega, namespace string, kind string, successful bool, instrumentationBy string) {
	instrumented := readLabel(g, namespace, kind, "dash0.com/instrumented")
	g.Expect(instrumented).To(Equal(strconv.FormatBool(successful)))
	operatorVersion := readLabel(g, namespace, kind, "dash0.com/operator-image")
	g.Expect(operatorVersion).To(Or(
		Equal("dash0-operator-controller_latest"),
		MatchRegexp("dash0-operator-controller_\\d+\\.\\d+\\.\\d+"),
	))
	initContainerImageVersion := readLabel(g, namespace, kind, "dash0.com/init-container-image")
	g.Expect(initContainerImageVersion).To(MatchRegexp("dash0-instrumentation_\\d+\\.\\d+\\.\\d+"))
	instrumentedBy := readLabel(g, namespace, kind, "dash0.com/instrumented-by")
	g.Expect(instrumentedBy).To(Equal(instrumentationBy))
	dash0Enable := readLabel(g, namespace, kind, "dash0.com/enable")
	g.Expect(dash0Enable).To(Equal(""))
}

func VerifyNoDash0Labels(g Gomega, namespace string, kind string) {
	instrumented := readLabel(g, namespace, kind, "dash0.com/instrumented")
	g.Expect(instrumented).To(Equal(""))
	operatorVersion := readLabel(g, namespace, kind, "dash0.com/operator-image")
	g.Expect(operatorVersion).To(Equal(""))
	initContainerImageVersion := readLabel(g, namespace, kind, "dash0.com/init-container-image")
	g.Expect(initContainerImageVersion).To(Equal(""))
	instrumentedBy := readLabel(g, namespace, kind, "dash0.com/instrumented-by")
	g.Expect(instrumentedBy).To(Equal(""))
	dash0Enable := readLabel(g, namespace, kind, "dash0.com/enable")
	g.Expect(dash0Enable).To(Equal(""))
}

func readLabel(g Gomega, namespace string, kind string, labelKey string) string {
	labelValue, err := Run(exec.Command(
		"kubectl",
		"get",
		kind,
		"--namespace",
		namespace,
		fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", kind),
		"-o",
		fmt.Sprintf("jsonpath={.metadata.labels['%s']}", strings.ReplaceAll(labelKey, ".", "\\.")),
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	return labelValue
}

func VerifySuccessfulInstrumentationEvent(
	g Gomega,
	namespace string,
	workloadType string,
	eventSource string,
) {
	verifyEvent(
		g,
		namespace,
		workloadType,
		util.ReasonSuccessfulInstrumentation,
		fmt.Sprintf("Dash0 instrumentation of this workload by the %s has been successful.", eventSource),
	)
}

func VerifyFailedInstrumentationEvent(
	g Gomega,
	namespace string,
	workloadType string,
	message string,
) {
	verifyEvent(
		g,
		namespace,
		workloadType,
		util.ReasonFailedInstrumentation,
		message,
	)
}

func VerifySuccessfulUninstrumentationEvent(
	g Gomega,
	namespace string,
	workloadType string,
	eventSource string,
) {
	verifyEvent(
		g,
		namespace,
		workloadType,
		util.ReasonSuccessfulUninstrumentation,
		fmt.Sprintf("The %s successfully removed the Dash0 instrumentation from this workload.", eventSource),
	)
}

func VerifyFailedUninstrumentationEvent(
	g Gomega,
	namespace string,
	workloadType string,
	message string,
) {
	verifyEvent(
		g,
		namespace,
		workloadType,
		util.ReasonFailedUninstrumentation,
		message,
	)
}

func verifyEvent(
	g Gomega,
	namespace string,
	workloadType string,
	reason util.Reason,
	message string,
) {
	resourceName := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
	eventsJson, err := Run(exec.Command(
		"kubectl",
		"events",
		"-ojson",
		"--namespace",
		namespace,
		"--for",
		fmt.Sprintf("%s/%s", workloadType, resourceName),
	), false)
	g.Expect(err).NotTo(HaveOccurred())
	var events corev1.EventList
	err = json.Unmarshal([]byte(eventsJson), &events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events.Items).To(
		ContainElement(
			testUtil.MatchEvent(
				namespace,
				resourceName,
				reason,
				message,
			)))
}

func verifySpans(g Gomega, isBatch bool, workloadType string, port int, httpPathWithQuery string) {
	spansFound := sendRequestAndFindMatchingSpans(g, isBatch, workloadType, port, httpPathWithQuery, true, nil)
	g.Expect(spansFound).To(BeTrue(),
		fmt.Sprintf("%s: expected to find at least one matching HTTP server span", workloadType))
}

func verifyNoSpans(isBatch bool, workloadType string, port int, httpPathWithQuery string) {
	timestampLowerBound := time.Now()
	spansFound := sendRequestAndFindMatchingSpans(
		Default,
		isBatch,
		"",
		port,
		httpPathWithQuery,
		false,
		&timestampLowerBound,
	)
	Expect(spansFound).To(BeFalse(), fmt.Sprintf("%s: expected to find no matching HTTP server span", workloadType))
}

func sendRequestAndFindMatchingSpans(
	g Gomega,
	isBatch bool,
	workloadType string,
	port int,
	httpPathWithQuery string,
	requestsMustNotFail bool,
	timestampLowerBound *time.Time,
) bool {
	sendRequest(g, isBatch, port, httpPathWithQuery, requestsMustNotFail)
	return fileHasMatchingSpan(g, workloadType, httpPathWithQuery, timestampLowerBound)
}

func sendRequest(g Gomega, isBatch bool, port int, httpPathWithQuery string, mustNotFail bool) {
	if !isBatch {
		response, err := http.Get(fmt.Sprintf("http://localhost:%d%s", port, httpPathWithQuery))
		if mustNotFail {
			g.Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = response.Body.Close()
			}()
			responseBody, err := io.ReadAll(response.Body)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(responseBody).To(ContainSubstring("We make Observability easy for every developer."))
		}
	}
}

func fileHasMatchingSpan(g Gomega, workloadType string, httpPathWithQuery string, timestampLowerBound *time.Time) bool {
	fileHandle, err := os.Open("test-resources/e2e-test-volumes/collector-received-data/traces.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = fileHandle.Close()
	}()
	scanner := bufio.NewScanner(fileHandle)
	scanner.Buffer(make([]byte, tracesJsonMaxLineLength), tracesJsonMaxLineLength)

	var resourceMatchFn func(span ptrace.ResourceSpans) bool
	if workloadType != "" {
		resourceMatchFn = resourceSpansHaveExpectedResourceAttributes(workloadType)
	}

	// read file line by line
	spansFound := false
	for scanner.Scan() {
		resourceSpanBytes := scanner.Bytes()
		traces, err := traceUnmarshaller.UnmarshalTraces(resourceSpanBytes)
		if err != nil {
			// ignore lines that cannot be parsed
			continue
		}
		if spansFound = hasMatchingSpans(
			traces,
			resourceMatchFn,
			isHttpServerSpanWithHttpTarget(httpPathWithQuery),
			timestampLowerBound,
		); spansFound {
			break
		}
	}

	g.Expect(scanner.Err()).NotTo(HaveOccurred())

	return spansFound
}

func hasMatchingSpans(
	traces ptrace.Traces,
	resourceMatchFn func(span ptrace.ResourceSpans) bool,
	spanMatchFn func(span ptrace.Span) bool,
	timestampLowerBound *time.Time,
) bool {
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		resourceSpan := traces.ResourceSpans().At(i)
		if resourceMatchFn != nil {
			if !resourceMatchFn(resourceSpan) {
				continue
			}
		}
		for j := 0; j < resourceSpan.ScopeSpans().Len(); j++ {
			scopeSpan := resourceSpan.ScopeSpans().At(j)
			for k := 0; k < scopeSpan.Spans().Len(); k++ {
				span := scopeSpan.Spans().At(k)
				if (timestampLowerBound == nil || span.StartTimestamp().AsTime().After(*timestampLowerBound)) &&
					spanMatchFn(span) {
					return true
				}
			}
		}
	}
	return false
}

func resourceSpansHaveExpectedResourceAttributes(workloadType string) func(span ptrace.ResourceSpans) bool {
	return func(resourceSpans ptrace.ResourceSpans) bool {
		attributes := resourceSpans.Resource().Attributes()
		attributes.Range(func(k string, v pcommon.Value) bool {
			return true
		})

		workloadAttributeFound := false
		if workloadType == "replicaset" {
			// There is no k8s.replicaset.name attribute.
			workloadAttributeFound = true
		} else {
			workloadKey := fmt.Sprintf("k8s.%s.name", workloadType)
			expectedWorkloadValue := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
			workloadAttribute, hasWorkloadAttribute := attributes.Get(workloadKey)
			if hasWorkloadAttribute {
				if workloadAttribute.Str() == expectedWorkloadValue {
					workloadAttributeFound = true
				}
			}
		}

		podKey := "k8s.pod.name"
		expectedPodName := fmt.Sprintf("dash0-operator-nodejs-20-express-test-%s", workloadType)
		expectedPodPrefix := fmt.Sprintf("%s-", expectedPodName)
		podAttributeFound := false
		podAttribute, hasPodAttribute := attributes.Get(podKey)
		if hasPodAttribute {
			if workloadType == "pod" {
				if podAttribute.Str() == expectedPodName {
					podAttributeFound = true
				}
			} else {
				if strings.Contains(podAttribute.Str(), expectedPodPrefix) {
					podAttributeFound = true
				}
			}
		}

		return workloadAttributeFound && podAttributeFound
	}
}

func isHttpServerSpanWithHttpTarget(expectedTarget string) func(span ptrace.Span) bool {
	return func(span ptrace.Span) bool {
		if span.Kind() == ptrace.SpanKindServer {
			target, hasTarget := span.Attributes().Get("http.target")
			if hasTarget {
				if target.Str() == expectedTarget {
					return true
				}
			}
		}
		return false
	}
}

func RunAndIgnoreOutput(cmd *exec.Cmd, logCommandArgs ...bool) error {
	_, err := Run(cmd, logCommandArgs...)
	return err
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd, logCommandArgs ...bool) (string, error) {
	var logCommand bool
	var alwaysLogOutput bool
	if len(logCommandArgs) >= 1 {
		logCommand = logCommandArgs[0]
	} else {
		logCommand = true
	}
	if len(logCommandArgs) >= 2 {
		alwaysLogOutput = logCommandArgs[1]
	} else {
		alwaysLogOutput = false
	}

	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	if logCommand {
		fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	}
	output, err := cmd.CombinedOutput()
	if alwaysLogOutput {
		fmt.Fprintf(GinkgoWriter, "output: %s\n", string(output))
	}
	if err != nil {
		return string(output), fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return string(output), nil
}

func warnError(err error) {
	fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}
