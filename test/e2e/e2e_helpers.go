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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	corev1 "k8s.io/api/core/v1"

	"github.com/dash0hq/dash0-operator/internal/dash0/util"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"

	testUtil "github.com/dash0hq/dash0-operator/test/util"
)

const (
	certmanagerVersion             = "v1.14.5"
	localHelmChart                 = "helm-chart/dash0-operator"
	operatorHelmReleaseName        = "e2e-tests-operator-helm-release"
	tracesJsonMaxLineLength        = 1_048_576
	verifyTelemetryTimeout         = 90 * time.Second
	verifyTelemetryPollingInterval = 500 * time.Millisecond
	dash0MonitoringResourceName    = "dash0-monitoring-resource"
	additionalImageTag             = "e2e-test"
)

var (
	traceUnmarshaller               = &ptrace.JSONUnmarshaler{}
	requiredPorts                   = []int{1207, 4317, 4318}
	collectorDaemonSetName          = fmt.Sprintf("%s-opentelemetry-collector-agent", operatorHelmReleaseName)
	collectorDaemonSetNameQualified = fmt.Sprintf("daemonset/%s", collectorDaemonSetName)
	collectorConfigMapName          = fmt.Sprintf("%s-opentelemetry-collector-agent", operatorHelmReleaseName)
	collectorConfigMapNameQualified = fmt.Sprintf("configmap/%s", collectorConfigMapName)
)

type ImageSpec struct {
	repository string
	tag        string
	digest     string
	pullPolicy string
}

type Images struct {
	operator              ImageSpec
	instrumentation       ImageSpec
	collector             ImageSpec
	configurationReloader ImageSpec
}

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
		Expect(
			RunAndIgnoreOutput(exec.Command("kubectl", "delete", "ns", namespace))).To(Succeed())
		Expect(
			RunAndIgnoreOutput(
				exec.Command("kubectl", "wait", "--for=delete", "ns", namespace, "--timeout=60s"))).To(Succeed())
	}

	Expect(
		RunAndIgnoreOutput(exec.Command("kubectl", "create", "ns", namespace))).To(Succeed())
}

func RebuildOperatorControllerImage(operatorImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(operatorImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the operator controller image %s, this looks like a remote image",
				renderFullyQualifiedImageName(operatorImage),
			))
		return
	}

	By(fmt.Sprintf("building the operator controller image: %s", renderFullyQualifiedImageName(operatorImage)))
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"make",
				"docker-build",
				fmt.Sprintf("CONTROLLER_IMG_REPOSITORY=%s", operatorImage.repository),
				fmt.Sprintf("CONTROLLER_IMG_TAG=%s", operatorImage.tag),
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: operatorImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(operatorImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func RebuildInstrumentationImage(instrumentationImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(instrumentationImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the instrumenation image %s, this looks like a remote image",
				renderFullyQualifiedImageName(instrumentationImage),
			))
		return
	}

	By(fmt.Sprintf("building the instrumentation image: %s", renderFullyQualifiedImageName(instrumentationImage)))
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"images/instrumentation/build.sh",
				instrumentationImage.repository,
				instrumentationImage.tag,
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: instrumentationImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(instrumentationImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func RebuildCollectorImage(collectorImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(collectorImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the collector image %s, this looks like a remote image",
				renderFullyQualifiedImageName(collectorImage),
			))
		return
	}

	By(fmt.Sprintf("building the collector image: %s", renderFullyQualifiedImageName(collectorImage)))
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"images/collector",
				"-t",
				collectorImage.tag,
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: collectorImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(collectorImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func RebuildConfigurationReloaderImage(configurationReloaderImage ImageSpec, buildImageLocally bool) {
	if !buildImageLocally {
		return
	}
	if strings.Contains(configurationReloaderImage.repository, "/") {
		By(
			fmt.Sprintf(
				"not rebuilding the configuration reloader image %s, this looks like a remote image",
				renderFullyQualifiedImageName(configurationReloaderImage),
			))
		return
	}

	By(fmt.Sprintf("building the configuration reloader image: %s",
		renderFullyQualifiedImageName(configurationReloaderImage)))
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"build",
				"images/configreloader",
				"-t",
				configurationReloaderImage.tag,
			))).To(Succeed())

	additionalTag := ImageSpec{
		repository: configurationReloaderImage.repository,
		tag:        additionalImageTag,
	}
	Expect(
		RunAndIgnoreOutput(
			exec.Command(
				"docker",
				"tag",
				renderFullyQualifiedImageName(configurationReloaderImage),
				renderFullyQualifiedImageName(additionalTag),
			))).To(Succeed())
}

func DeployOperator(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	images Images,
	enableWebhook bool,
	workingDir string,
) {
	ensureDash0OperatorHelmRepoIsInstalled(operatorHelmChart, operatorHelmChartUrl)

	e2eTestExportDir :=
		fmt.Sprintf(
			"%s/test-resources/e2e-test-volumes/collector-received-data",
			workingDir,
		)
	By(
		fmt.Sprintf(
			"deploying the operator controller to namespace %s with telemetry export directory %s",
			operatorNamespace,
			e2eTestExportDir,
		))
	arguments := []string{
		"install",
		"--namespace",
		operatorNamespace,
		"--create-namespace",
		"--set", "operator.developmentMode=true",
		"--set", "operator.disableSecretCheck=true",
		"--set", "operator.disableOtlpEndpointCheck=true",
		"--set", fmt.Sprintf("operator.enableWebhook=%t", enableWebhook),
		"--set", "operator.e2eTestMode=true",
		"--set", fmt.Sprintf("operator.e2eTestExportDir=%s", e2eTestExportDir),
	}
	arguments = addOptionalHelmParameters(arguments, operatorHelmChart, images)

	output, err := Run(exec.Command("helm", arguments...))
	Expect(err).NotTo(HaveOccurred())
	fmt.Fprintf(GinkgoWriter, "output of helm install:\n%s", output)

	verifyThatControllerPodIsRunning(operatorNamespace)
}

func addOptionalHelmParameters(arguments []string, operatorHelmChart string, images Images) []string {
	arguments = setIfNotEmpty(arguments, "operator.image.repository", images.operator.repository)
	arguments = setIfNotEmpty(arguments, "operator.image.tag", images.operator.tag)
	arguments = setIfNotEmpty(arguments, "operator.image.digest", images.operator.digest)
	arguments = setIfNotEmpty(arguments, "operator.image.pullPolicy", images.operator.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.collectorImage.repository", images.collector.repository)
	arguments = setIfNotEmpty(arguments, "operator.collectorImage.tag", images.collector.tag)
	arguments = setIfNotEmpty(arguments, "operator.collectorImage.digest", images.collector.digest)
	arguments = setIfNotEmpty(arguments, "operator.collectorImage.pullPolicy", images.collector.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.configurationReloaderImage.repository",
		images.configurationReloader.repository)
	arguments = setIfNotEmpty(arguments, "operator.configurationReloaderImage.tag", images.configurationReloader.tag)
	arguments = setIfNotEmpty(arguments, "operator.configurationReloaderImage.digest",
		images.configurationReloader.digest)
	arguments = setIfNotEmpty(arguments, "operator.configurationReloaderImage.pullPolicy",
		images.configurationReloader.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.repository", images.instrumentation.repository)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.tag", images.instrumentation.tag)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.digest", images.instrumentation.digest)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.pullPolicy", images.instrumentation.pullPolicy)

	arguments = append(arguments, operatorHelmReleaseName)
	arguments = append(arguments, operatorHelmChart)
	return arguments
}

func setIfNotEmpty(arguments []string, key string, value string) []string {
	if value != "" {
		arguments = append(arguments, "--set")
		arguments = append(arguments, fmt.Sprintf("%s=%s", key, value))
	}
	return arguments
}

func ensureDash0OperatorHelmRepoIsInstalled(operatorHelmChart string, operatorHelmChartUrl string) {
	if operatorHelmChart == localHelmChart && operatorHelmChartUrl == "" {
		// installing from local Helm chart sources, no action required
		return
	} else if operatorHelmChart == localHelmChart && operatorHelmChartUrl != "" {
		Fail("Invalid test setup: When setting a URL for the Helm chart (OPERATOR_HELM_CHART_URL), you also need to " +
			"provide a custom name (OPERATOR_HELM_CHART).")
	} else if operatorHelmChart != localHelmChart && operatorHelmChartUrl == "" {
		Fail("Invalid test setup: When setting a non-standard name for the operator Helm chart " +
			"(OPERATOR_HELM_CHART), you also need to provide a URL from where to install it (OPERATOR_HELM_CHART_URL).")
	} else if operatorHelmChartUrl != "" && !strings.Contains(operatorHelmChart, "/") {
		Fail("Invalid test setup: When using a Helm chart URL (OPERATOR_HELM_CHART_URL), the provided Helm chart " +
			"name (OPERATOR_HELM_CHART) needs to have the format ${repository}/${chart_name}.")
	}
	repositoryName := operatorHelmChart[:strings.LastIndex(operatorHelmChart, "/")]
	By(fmt.Sprintf("checking whether the operator Helm chart repo %s (%s) has been installed",
		repositoryName, operatorHelmChartUrl))
	repoList, err := Run(exec.Command("helm", "repo", "list"))
	Expect(err).NotTo(HaveOccurred())
	if !regexp.MustCompile(
		fmt.Sprintf("%s\\s+%s", repositoryName, operatorHelmChartUrl)).MatchString(repoList) {
		fmt.Fprintf(
			GinkgoWriter,
			"The helm repo %s (%s) has not been found, adding it now.\n",
			repositoryName,
			operatorHelmChartUrl,
		)
		Expect(RunAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				repositoryName,
				operatorHelmChartUrl,
				"--force-update",
			))).To(Succeed())
		Expect(RunAndIgnoreOutput(exec.Command("helm", "repo", "update"))).To(Succeed())
	} else {
		fmt.Fprintf(
			GinkgoWriter,
			"The helm repo %s (%s) is already installed.\n",
			repositoryName,
			operatorHelmChartUrl,
		)
	}
}

func verifyThatControllerPodIsRunning(operatorNamespace string) {
	var controllerPodName string
	By("validating that the controller-manager pod is running as expected")
	verifyControllerUp := func(g Gomega) error {
		cmd := exec.Command("kubectl", "get",
			"pods", "-l", "control-plane=controller-manager",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", operatorNamespace,
		)

		podOutput, err := Run(cmd, false)
		g.Expect(err).NotTo(HaveOccurred())
		podNames := GetNonEmptyLines(podOutput)
		if len(podNames) != 1 {
			return fmt.Errorf("expect 1 controller pods running, but got %d -- %s", len(podNames), podOutput)
		}
		controllerPodName = podNames[0]
		g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

		cmd = exec.Command("kubectl", "get",
			"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
			"-n", operatorNamespace,
		)
		status, err := Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		if status != "Running" {
			return fmt.Errorf("controller pod in %s status", status)
		}
		return nil
	}

	Eventually(verifyControllerUp, 120*time.Second, time.Second).Should(Succeed())
}

func verifyThatOTelCollectorIsRunning(operatorNamespace string) {
	By("validating that the OpenTelemetry collector has been created and is running as expected")
	verifyCollectorIsUp := func(g Gomega) {
		// Even though this command comes with its own timeout, we still have to wrap it in an Eventually block, since
		// it will fail outright if the daemonset has not been created yet.
		g.Expect(RunAndIgnoreOutput(
			exec.Command("kubectl",
				"rollout",
				"status",
				"daemonset",
				collectorDaemonSetName,
				"--namespace",
				operatorNamespace,
				"--timeout",
				"20s",
			))).To(Succeed())
	}

	Eventually(verifyCollectorIsUp, 60*time.Second, time.Second).Should(Succeed())
}

func VerifyThatOTelCollectorHasBeenRemoved(operatorNamespace string) {
	By("validating that the OpenTelemetry collector has been removed")
	verifyCollectorIsGone := func(g Gomega) {
		g.Expect(RunAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"get",
				"daemonset",
				"--namespace",
				operatorNamespace,
				collectorDaemonSetName,
			))).ToNot(Succeed())
	}
	Eventually(verifyCollectorIsGone, 60*time.Second, time.Second).Should(Succeed())
}

func UndeployOperator(operatorNamespace string) {
	By("undeploying the operator controller")
	Expect(
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
	Expect(
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
	g.Expect(RunAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--for=delete",
		fmt.Sprintf("namespace/%s", operatorNamespace),
		"--timeout=60s",
	))).To(Succeed())
}

func UpgradeOperator(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	images Images,
	enableWebhook bool,
) {
	ensureDash0OperatorHelmRepoIsInstalled(operatorHelmChart, operatorHelmChartUrl)

	By("upgrading the operator controller")
	arguments := []string{
		"upgrade",
		"--namespace",
		operatorNamespace,
		"--set", "operator.developmentMode=true",
		"--set", "operator.disableSecretCheck=true",
		"--set", "operator.disableOtlpEndpointCheck=true",
		"--set", fmt.Sprintf("operator.enableWebhook=%t", enableWebhook),
	}
	arguments = addOptionalHelmParameters(arguments, operatorHelmChart, images)

	output, err := Run(exec.Command("helm", arguments...))
	Expect(err).NotTo(HaveOccurred())
	fmt.Fprintf(GinkgoWriter, "output of helm upgrade:\n%s", output)

	By("waiting shortly, to give the operator time to restart after helm upgrade")
	time.Sleep(5 * time.Second)

	verifyThatControllerPodIsRunning(operatorNamespace)

	verifyThatOTelCollectorIsRunning(operatorNamespace)
}

func DeployDash0MonitoringResource(namespace string, operatorNamespace string) {
	TruncateExportedTelemetry()
	By(fmt.Sprintf(
		"deploying the Dash0 monitoring resource to namespace %s, operator namespace is %s",
		namespace, operatorNamespace))
	Expect(
		RunAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-n",
			namespace,
			"-f",
			"test-resources/customresources/dash0monitoring/dash0monitoring.token.yaml",
		))).To(Succeed())

	// Deploying the Dash0 monitoring resource will trigger creating the default OpenTelemetry collecor instance.
	verifyThatOTelCollectorIsRunning(operatorNamespace)
}

func UpdateDash0MonitoringResource(namespace string, newIngressEndpoint string) bool {
	return Expect(
		RunAndIgnoreOutput(exec.Command(
			"kubectl",
			"patch",
			"-n",
			namespace,
			"Dash0Monitoring",
			dash0MonitoringResourceName,
			"--type",
			"merge",
			"-p",
			"{\"spec\":{\"ingressEndpoint\":\""+newIngressEndpoint+"\"}}",
		))).To(Succeed())
}

func UndeployDash0MonitoringResource(namespace string) {
	TruncateExportedTelemetry()
	By(fmt.Sprintf("Removing the Dash0 monitoring resource from namespace %s", namespace))
	Expect(
		RunAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"-f",
			"test-resources/customresources/dash0monitoring/dash0monitoring.token.yaml",
			"--ignore-not-found",
		))).To(Succeed())
}

func VerifyDash0MonitoringResourceDoesNotExist(g Gomega, namespace string) {
	output, err := Run(exec.Command(
		"kubectl",
		"get",
		"--namespace",
		namespace,
		"--ignore-not-found",
		"dash0monitoring",
		dash0MonitoringResourceName,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(output).NotTo(ContainSubstring(dash0MonitoringResourceName))
}

func TruncateExportedTelemetry() {
	By("truncating old captured telemetry files")
	_ = os.Truncate("test-resources/e2e-test-volumes/collector-received-data/traces.jsonl", 0)
	_ = os.Truncate("test-resources/e2e-test-volumes/collector-received-data/metrics.jsonl", 0)
	_ = os.Truncate("test-resources/e2e-test-volumes/collector-received-data/logs.jsonl", 0)
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

func InstallNodeJsDaemonSetWithOptOutLabel(namespace string) error {
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

func installNodeJsApplication(
	namespace string,
	templateName string,
	waitCommand *exec.Cmd,
) error {
	err := RunAndIgnoreOutput(exec.Command(
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
	By(fmt.Sprintf("waiting for %s to become ready", templateName))
	err = RunAndIgnoreOutput(waitCommand)
	if err == nil {
		By(fmt.Sprintf("%s is ready", templateName))
	}
	return err
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

func AddOptOutLabel(namespace string, workloadType string, workloadName string) error {
	return RunAndIgnoreOutput(
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

func RemoveOptOutLabel(namespace string, workloadType string, workloadName string) error {
	return RunAndIgnoreOutput(
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

func DeleteTestIdFiles() {
	_ = os.Remove("test-resources/e2e-test-volumes/test-uuid/cronjob.test.id")
	_ = os.Remove("test-resources/e2e-test-volumes/test-uuid/job.test.id")
}

func VerifyThatWorkloadHasBeenInstrumented(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	images Images,
	instrumentationBy string,
) string {
	By(fmt.Sprintf("%s: waiting for the workload to get instrumented (polling its labels and events to check)",
		workloadType))
	Eventually(func(g Gomega) {
		VerifyLabels(
			g,
			namespace,
			workloadType,
			true,
			images,
			instrumentationBy,
		)
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
	verifyThatInstrumentationHasBeenReverted(
		namespace,
		workloadType,
		port,
		isBatch,
		testId,
		instrumentationBy,
		false,
	)
}

func VerifyThatInstrumentationHasBeenRevertedAfterAddingOptOutLabel(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	instrumentationBy string,
) {
	verifyThatInstrumentationHasBeenReverted(
		namespace,
		workloadType,
		port,
		isBatch,
		testId,
		instrumentationBy,
		true,
	)
}

func verifyThatInstrumentationHasBeenReverted(
	namespace string,
	workloadType string,
	port int,
	isBatch bool,
	testId string,
	instrumentationBy string,
	expectOptOutLabel bool,
) {
	By(fmt.Sprintf(
		"%s: waiting for the instrumentation to get removed from the workload (polling its labels and events to check)",
		workloadType))
	Eventually(func(g Gomega) {
		if expectOptOutLabel {
			VerifyOnlyOptOutLabelIsPresent(g, namespace, workloadType)
		} else {
			VerifyNoDash0Labels(g, namespace, workloadType)
		}
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

func VerifyThatFailedInstrumentationAttemptLabelsHaveBeenRemoved(namespace string, workloadType string) {
	By("waiting for the labels to get removed from the workload")
	Eventually(func(g Gomega) {
		VerifyNoDash0Labels(g, namespace, workloadType)
	}, verifyTelemetryTimeout, verifyTelemetryPollingInterval).Should(Succeed())
}

func VerifyLabels(
	g Gomega,
	namespace string,
	kind string,
	successful bool,
	images Images,
	instrumentationBy string,
) {
	instrumented := readLabel(g, namespace, kind, "dash0.com/instrumented")
	g.Expect(instrumented).To(Equal(strconv.FormatBool(successful)))
	operatorImage := readLabel(g, namespace, kind, "dash0.com/operator-image")
	verifyImageLabel(g, operatorImage, images.operator, "ghcr.io/dash0hq/operator-controller:")
	initContainerImage := readLabel(g, namespace, kind, "dash0.com/init-container-image")
	verifyImageLabel(g, initContainerImage, images.instrumentation, "ghcr.io/dash0hq/instrumentation:")
	instrumentedBy := readLabel(g, namespace, kind, "dash0.com/instrumented-by")
	g.Expect(instrumentedBy).To(Equal(instrumentationBy))
	dash0Enable := readLabel(g, namespace, kind, "dash0.com/enable")
	g.Expect(dash0Enable).To(Equal(""))
}

func verifyImageLabel(g Gomega, labelValue string, image ImageSpec, defaultImageNamePrefix string) {
	expectedLabel, expectFullLabel := expectedImageLabel(image, defaultImageNamePrefix)
	if expectFullLabel {
		g.Expect(labelValue).To(Equal(expectedLabel))
	} else {
		g.Expect(labelValue).To(ContainSubstring(expectedLabel))
	}
}

func expectedImageLabel(image ImageSpec, defaultImageName string) (string, bool) {
	// If the repository has been unset explicitly via the respective environment variable, the default image from the
	// helm chart will be used, so we need to test against that.
	if image.repository == "" {
		return util.ImageNameToLabel(defaultImageName), false
	}
	return util.ImageNameToLabel(renderFullyQualifiedImageName(image)), true
}

func renderFullyQualifiedImageName(image ImageSpec) string {
	return fmt.Sprintf("%s:%s", image.repository, image.tag)
}

func VerifyNoDash0Labels(g Gomega, namespace string, kind string) {
	verifyNoDash0Labels(g, namespace, kind, false)
}

func VerifyOnlyOptOutLabelIsPresent(g Gomega, namespace string, kind string) {
	verifyNoDash0Labels(g, namespace, kind, true)
}

func verifyNoDash0Labels(g Gomega, namespace string, kind string, expectOptOutLabel bool) {
	instrumented := readLabel(g, namespace, kind, "dash0.com/instrumented")
	g.Expect(instrumented).To(Equal(""))
	operatorVersion := readLabel(g, namespace, kind, "dash0.com/operator-image")
	g.Expect(operatorVersion).To(Equal(""))
	initContainerImageVersion := readLabel(g, namespace, kind, "dash0.com/init-container-image")
	g.Expect(initContainerImageVersion).To(Equal(""))
	instrumentedBy := readLabel(g, namespace, kind, "dash0.com/instrumented-by")
	g.Expect(instrumentedBy).To(Equal(""))
	dash0Enable := readLabel(g, namespace, kind, "dash0.com/enable")
	if expectOptOutLabel {
		g.Expect(dash0Enable).To(Equal("false"))
	} else {
		g.Expect(dash0Enable).To(Equal(""))
	}
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
	spansFound := sendRequestAndFindMatchingSpans(g, isBatch, workloadType, port, httpPathWithQuery, nil)
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
	timestampLowerBound *time.Time,
) bool {
	if !isBatch {
		sendRequest(g, port, httpPathWithQuery)
	}
	return fileHasMatchingSpan(g, workloadType, httpPathWithQuery, timestampLowerBound)
}

func sendRequest(g Gomega, port int, httpPathWithQuery string) {
	url := fmt.Sprintf("http://localhost:%d%s", port, httpPathWithQuery)
	client := http.Client{
		Timeout: 500 * time.Millisecond,
	}
	response, err := client.Get(url)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = response.Body.Close()
	}()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "could not read http response from %s: %s\n", url, err.Error())
	}
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(responseBody).To(ContainSubstring("We make Observability easy for every developer."))
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
