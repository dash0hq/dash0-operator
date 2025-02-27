// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/dash0hq/dash0-operator/internal/startup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	localHelmChart          = "helm-chart/dash0-operator"
	operatorHelmReleaseName = "e2e-tests-operator-helm-release"
)

var (
	operatorHelmChart    = localHelmChart
	operatorHelmChartUrl = ""
	operatorNamespace    = "dash0-system"
)

func isLocalHelmChart() bool {
	return operatorHelmChart == localHelmChart
}

func deployOperatorWithDefaultAutoOperationConfiguration(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	images Images,
) {
	err := deployOperator(
		operatorNamespace,
		operatorHelmChart,
		operatorHelmChartUrl,
		images,
		&startup.OperatorConfigurationValues{
			Endpoint:              defaultEndpoint,
			Token:                 defaultToken,
			SelfMonitoringEnabled: true,
			KubernetesInfrastructureMetricsCollectionEnabled: true,
		},
	)
	Expect(err).ToNot(HaveOccurred())
}

func deployOperatorWithoutAutoOperationConfiguration(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	images Images,
) {
	err := deployOperator(
		operatorNamespace,
		operatorHelmChart,
		operatorHelmChartUrl,
		images,
		nil,
	)
	Expect(err).ToNot(HaveOccurred())
}

func deployOperator(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	images Images,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
) error {
	ensureDash0OperatorHelmRepoIsInstalled(operatorHelmChart, operatorHelmChartUrl)

	By(
		fmt.Sprintf(
			"deploying the operator controller to namespace %s",
			operatorNamespace,
		))
	arguments := []string{
		"install",
		"--namespace",
		operatorNamespace,
		"--create-namespace",
		"--set", "operator.developmentMode=true",
	}
	arguments = addOptionalHelmParameters(arguments, images)

	if operatorConfigurationValues != nil {
		arguments = setHelmParameter(arguments, "operator.dash0Export.enabled", "true")
		arguments = setIfNotEmpty(arguments, "operator.dash0Export.endpoint", operatorConfigurationValues.Endpoint)
		arguments = setIfNotEmpty(arguments, "operator.dash0Export.token", operatorConfigurationValues.Token)
		arguments = setIfNotEmpty(
			arguments,
			"operator.dash0Export.secretRef.name",
			operatorConfigurationValues.SecretRef.Name,
		)
		arguments = setIfNotEmpty(
			arguments,
			"operator.dash0Export.secretRef.key",
			operatorConfigurationValues.SecretRef.Key,
		)
		arguments = setHelmParameter(arguments, "operator.clusterName", e2eKubernetesContext)
	}

	arguments = append(arguments, operatorHelmReleaseName)
	arguments = append(arguments, operatorHelmChart)

	output, err := run(exec.Command("helm", arguments...))
	if err != nil {
		return err
	}

	e2ePrint("output of helm install:\n%s", output)
	waitForManagerPodAndWebhookToStart(operatorNamespace)

	if operatorConfigurationValues != nil {
		waitForAutoOperatorConfigurationResourceToBecomeAvailable()
		waitForCollectorToStart(operatorNamespace, operatorHelmChart)
	}

	return nil
}

func addOptionalHelmParameters(arguments []string, images Images) []string {
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

	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSynchImage.repository",
		images.fileLogOffsetSynch.repository)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSynchImage.tag", images.fileLogOffsetSynch.tag)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSynchImage.digest",
		images.fileLogOffsetSynch.digest)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSynchImage.pullPolicy",
		images.fileLogOffsetSynch.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.repository", images.instrumentation.repository)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.tag", images.instrumentation.tag)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.digest", images.instrumentation.digest)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.pullPolicy", images.instrumentation.pullPolicy)

	return arguments
}

func setIfNotEmpty(arguments []string, key string, value string) []string {
	if value != "" {
		return setHelmParameter(arguments, key, value)
	}
	return arguments
}

func setHelmParameter(arguments []string, key string, value string) []string {
	arguments = append(arguments, "--set")
	arguments = append(arguments, fmt.Sprintf("%s=%s", key, value))
	return arguments
}

func ensureDash0OperatorHelmRepoIsInstalled(
	operatorHelmChart string,
	operatorHelmChartUrl string,
) {
	if isLocalHelmChart() && operatorHelmChartUrl == "" {
		// installing from local Helm chart sources, no action required
		return
	} else if isLocalHelmChart() && operatorHelmChartUrl != "" {
		Fail("Invalid test setup: When setting a URL for the Helm chart (OPERATOR_HELM_CHART_URL), you also need to " +
			"provide a custom name (OPERATOR_HELM_CHART).")
	} else if !isLocalHelmChart() && operatorHelmChartUrl == "" {
		Fail("Invalid test setup: When setting a non-standard name for the operator Helm chart " +
			"(OPERATOR_HELM_CHART), you also need to provide a URL from where to install it (OPERATOR_HELM_CHART_URL).")
	} else if operatorHelmChartUrl != "" && !strings.Contains(operatorHelmChart, "/") {
		Fail("Invalid test setup: When using a Helm chart URL (OPERATOR_HELM_CHART_URL), the provided Helm chart " +
			"name (OPERATOR_HELM_CHART) needs to have the format ${repository}/${chart_name}.")
	}
	repositoryName := operatorHelmChart[:strings.LastIndex(operatorHelmChart, "/")]
	By(fmt.Sprintf("checking whether the operator Helm chart repo %s (%s) has been installed",
		repositoryName, operatorHelmChartUrl))
	repoList, err := run(exec.Command("helm", "repo", "list"))
	Expect(err).NotTo(HaveOccurred())
	if !regexp.MustCompile(
		fmt.Sprintf("%s\\s+%s", repositoryName, operatorHelmChartUrl)).MatchString(repoList) {
		e2ePrint(
			"The helm repo %s (%s) has not been found, adding it now.\n",
			repositoryName,
			operatorHelmChartUrl,
		)
		Expect(runAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				repositoryName,
				operatorHelmChartUrl,
				"--force-update",
			))).To(Succeed())
		Expect(runAndIgnoreOutput(exec.Command("helm", "repo", "update"))).To(Succeed())
	} else {
		e2ePrint(
			"The helm repo %s (%s) is already installed, updating it now.\n",
			repositoryName,
			operatorHelmChartUrl,
		)
		Expect(runAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"update",
			))).To(Succeed())
	}
}

func waitForManagerPodAndWebhookToStart(operatorNamespace string) {
	var managerPodName string
	By("validating that the manager pod is running as expected")
	verifyControllerUp := func(g Gomega) error {
		cmd := exec.Command("kubectl", "get",
			"pods", "-l", "app.kubernetes.io/name=dash0-operator",
			"-l", "app.kubernetes.io/component=controller",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", operatorNamespace,
		)

		podOutput, err := run(cmd, false)
		g.Expect(err).NotTo(HaveOccurred())
		podNames := getNonEmptyLines(podOutput)
		if len(podNames) != 1 {
			return fmt.Errorf("expect 1 controller pods running, but got %d -- %s", len(podNames), podOutput)
		}
		managerPodName = podNames[0]
		g.Expect(managerPodName).To(ContainSubstring("controller"))

		cmd = exec.Command("kubectl", "get",
			"pods", managerPodName, "-o", "jsonpath={.status.phase}",
			"-n", operatorNamespace,
		)
		status, err := run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		if status != "Running" {
			return fmt.Errorf("controller pod in %s status", status)
		}
		return nil
	}

	Eventually(verifyControllerUp, 120*time.Second, time.Second).Should(Succeed())

	By("wait for webhook endpoint to become available")
	Eventually(func(g Gomega) {
		endpointsOutput, err := run(exec.Command(
			"kubectl",
			"get",
			"endpoints",
			"--namespace",
			operatorNamespace,
			"dash0-operator-webhook-service",
		), false)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(endpointsOutput).To(ContainSubstring("dash0-operator-webhook-service"))
		g.Expect(endpointsOutput).To(ContainSubstring(":9443"))
	}, 20*time.Second, 200*time.Millisecond).Should(Succeed())
}

func undeployOperator(operatorNamespace string) {
	By("undeploying the operator controller")
	Expect(
		runAndIgnoreOutput(
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
		runAndIgnoreOutput(
			exec.Command(
				"kubectl",
				"delete",
				"ns",
				"--ignore-not-found",
				operatorNamespace,
			))).To(Succeed())

	verifyDash0OperatorReleaseIsNotInstalled(Default, operatorNamespace)
}

func verifyDash0OperatorReleaseIsNotInstalled(g Gomega, operatorNamespace string) {
	g.Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"wait",
		"--for=delete",
		fmt.Sprintf("namespace/%s", operatorNamespace),
		"--timeout=60s",
	))).To(Succeed())
}

func upgradeOperator(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	images Images,
) {
	ensureDash0OperatorHelmRepoIsInstalled(operatorHelmChart, operatorHelmChartUrl)

	By("upgrading the operator controller")
	arguments := []string{
		"upgrade",
		"--namespace",
		operatorNamespace,
		"--set", "operator.developmentMode=true",
	}
	arguments = addOptionalHelmParameters(arguments, images)

	arguments = append(arguments, operatorHelmReleaseName)
	arguments = append(arguments, operatorHelmChart)

	output, err := run(exec.Command("helm", arguments...))
	Expect(err).NotTo(HaveOccurred())
	e2ePrint("output of helm upgrade:\n%s", output)

	By("waiting shortly, to give the operator time to restart after helm upgrade")
	time.Sleep(5 * time.Second)

	waitForManagerPodAndWebhookToStart(operatorNamespace)

	waitForCollectorToStart(operatorNamespace, operatorHelmChart)
}
