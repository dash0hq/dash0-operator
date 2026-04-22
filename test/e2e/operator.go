// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/dash0hq/dash0-operator/internal/startup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type helmSearchResult struct {
	Version string `json:"version"`
}

const (
	localHelmChart          = "helm-chart/dash0-operator"
	operatorHelmReleaseName = "e2e-tests-operator-hr"

	publishedChart    = "dash0-operator/dash0-operator"
	publishedChartUrl = "https://dash0hq.github.io/dash0-operator"
)

var (
	operatorHelmChart    = localHelmChart
	operatorHelmChartUrl = ""
	operatorNamespace    = "e2e-operator-namespace"
)

func determineOperatorHelmChart() {
	operatorHelmChart = getEnvOrDefault("OPERATOR_HELM_CHART", operatorHelmChart)
	operatorHelmChartUrl = getEnvOrDefault("OPERATOR_HELM_CHART_URL", operatorHelmChartUrl)
}

func isLocalHelmChart(chart string) bool {
	return chart == localHelmChart
}

func deployOperatorWithDefaultAutoOperationConfiguration(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	operatorHelmChartVersion string,
	images *Images,
	selfMonitoringEnabled bool,
	additionalHelmParameters map[string]string,
) {
	err := deployOperator(
		operatorNamespace,
		operatorHelmChart,
		operatorHelmChartUrl,
		operatorHelmChartVersion,
		images,
		&startup.OperatorConfigurationValues{
			Endpoint:              defaultEndpoint,
			ApiEndpoint:           dash0ApiMockServiceBaseUrl,
			Token:                 defaultToken,
			SelfMonitoringEnabled: selfMonitoringEnabled,
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			CollectPodLabelsAndAnnotationsEnabled:            true,
			PrometheusCrdSupportEnabled:                      false,
			TelemetryCollectionEnabled:                       true,
		},
		additionalHelmParameters,
	)
	Expect(err).ToNot(HaveOccurred())
}

//nolint:unparam
func deployOperatorWithoutAutoOperationConfiguration(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	operatorHelmChartVersion string,
	images *Images,
	additionalHelmParameters map[string]string,
) {
	err := deployOperator(
		operatorNamespace,
		operatorHelmChart,
		operatorHelmChartUrl,
		operatorHelmChartVersion,
		images,
		nil,
		additionalHelmParameters,
	)
	Expect(err).ToNot(HaveOccurred())
}

func deployOperator(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	operatorHelmChartVersion string,
	images *Images,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	additionalHelmParameters map[string]string,
) error {
	return executeOperatorHelmChart(
		"install",
		operatorNamespace,
		operatorHelmChart,
		operatorHelmChartUrl,
		operatorHelmChartVersion,
		images,
		operatorConfigurationValues,
		additionalHelmParameters,
	)
}

// deployPreviousOperatorRelease deploys the "previous" chart release from the published Helm chart and returns the
// version it has deployed. The term "previous" has a slightly different meaning, depending on whether the test suite as
// a whole is running with the local Helm chart or the published Helm chart.
//   - When running the test suite with the local Helm chart, it will install the most recent release. For the
//     "operator upgrade" test suite, this verifies that users running the currently published release can upgrade
//     to the next release, which will be built from the current state of local source code.
//   - When running the test suite with the published Helm chart, it will install the release before the most recent
//     release. For the "operator upgrade" test suite, this verifies that users running the previous release can upgrade
//     to the current release.
//
// The version of the installed release is returned as a string.
func deployPreviousOperatorRelease() string {
	version := ""
	// check whether the test suite generally runs with the local Helm chart the published Helm chart
	if isLocalHelmChart(operatorHelmChart) {
		// The test suite is running against a local Helm chart. The "previous release" is the most recently
		// published chart happens to be.
		version = readLatestPublishedChartVersion(publishedChart, publishedChartUrl)
	} else {
		// The test suite is already running against the published chart. We need to install the version before the most
		// recently published version.
		version = readPreviousPublishedChartVersion(publishedChart, publishedChartUrl)
	}

	By("installing version " + version + " of the operator Helm chart")
	deployOperatorWithDefaultAutoOperationConfiguration(
		operatorNamespace,
		publishedChart,
		publishedChartUrl,
		version,
		nil, // no image overrides, use the images from the published Helm chart
		true,
		nil,
	)
	return version
}

func readLatestPublishedChartVersion(chart string, chartUrl string) string {
	return readHelmChartVersion(chart, chartUrl, 0)
}

func readPreviousPublishedChartVersion(chart string, chartUrl string) string {
	return readHelmChartVersion(chart, chartUrl, 1)
}

func readHelmChartVersion(chart string, chartUrl string, index int64) string {
	// "helm search repo" searches local repositories; and "helm search repo $chart --versions --output json" will
	// return process exit code 0 and simply output "[]" if the repository is not installed locally. We need to make
	// sure the dash0-operator repository has been installed locally.
	ensureDash0OperatorHelmRepoIsInstalledAndUpToDate(chart, chartUrl)
	output, err := run(exec.Command("helm", "search", "repo", chart, "--versions", "--output", "json"))
	Expect(err).ToNot(HaveOccurred())
	var results []helmSearchResult
	Expect(json.Unmarshal([]byte(output), &results)).To(
		Succeed(),
		"cannot parse helm search repo output for %s\n%s",
		chart,
		output,
	)
	Expect(len(results)).To(
		BeNumerically(">=", index+1),
		"expected at least %d published version(s) of %s to be available, but found only %d; full output of helm "+
			"repository search command: %s",
		index+1,
		chart,
		len(results),
		output,
	)

	// helm search repo returns versions sorted by semver descending; index 0 is the latest, index 1 is the previous.
	version := results[index].Version
	e2ePrint("found helm chart version %s.\n", version)
	Expect(version).ToNot(BeEmpty())
	Expect(version).To(MatchRegexp("\\d+\\.\\d+\\.\\d+"))
	return version
}

func upgradeOperator(
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	operatorHelmChartVersion string,
	images *Images,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	additionalHelmParameters map[string]string,
) error {
	return executeOperatorHelmChart(
		"upgrade",
		operatorNamespace,
		operatorHelmChart,
		operatorHelmChartUrl,
		operatorHelmChartVersion,
		images,
		operatorConfigurationValues,
		additionalHelmParameters,
	)
}

func executeOperatorHelmChart(
	helmCommand string,
	operatorNamespace string,
	operatorHelmChart string,
	operatorHelmChartUrl string,
	operatorHelmChartVersion string,
	images *Images,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	additionalHelmParameters map[string]string,
) error {
	ensureDash0OperatorHelmRepoIsInstalledAndUpToDate(operatorHelmChart, operatorHelmChartUrl)

	By(
		fmt.Sprintf(
			"running helm %s for the operator helm chart, deploying to namespace %s",
			helmCommand,
			operatorNamespace,
		))
	arguments := []string{
		helmCommand,
		"--wait",
		"--namespace",
		operatorNamespace,
	}
	if helmCommand == "install" {
		arguments = append(arguments, "--create-namespace")
	}
	arguments = append(arguments, "--set", "operator.developmentMode=true")
	if images != nil {
		arguments = addHelmParametersForImages(arguments, *images)
	}

	if operatorConfigurationValues != nil {
		arguments = setHelmParameter(arguments, "operator.dash0Export.enabled", "true")
		arguments = setIfNotEmpty(arguments, "operator.dash0Export.endpoint", operatorConfigurationValues.Endpoint)
		arguments = setIfNotEmpty(arguments, "operator.dash0Export.token", operatorConfigurationValues.Token)
		arguments = setIfNotEmpty(arguments, "operator.dash0Export.apiEndpoint", operatorConfigurationValues.ApiEndpoint)
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
		arguments = setHelmParameter(arguments, "operator.selfMonitoringEnabled",
			operatorConfigurationValues.SelfMonitoringEnabled)
		arguments = setHelmParameter(arguments, "operator.telemetryCollectionEnabled",
			operatorConfigurationValues.TelemetryCollectionEnabled)
		if operatorConfigurationValues.ProfilingEnabled {
			arguments = setHelmParameter(arguments, "operator.profilingEnabled", "true")
		}
	}

	if additionalHelmParameters != nil {
		arguments = setAdditionalHelmParameters(arguments, additionalHelmParameters)
	}
	arguments = append(arguments, operatorHelmReleaseName)
	arguments = append(arguments, operatorHelmChart)
	if operatorHelmChartVersion != "" {
		arguments = append(arguments, "--version", operatorHelmChartVersion)
	}

	output, err := run(exec.Command("helm", arguments...))
	if err != nil {
		return err
	}

	e2ePrint("output of helm %s:\n%s\n", helmCommand, output)

	if (operatorConfigurationValues != nil && operatorConfigurationValues.TelemetryCollectionEnabled) ||
		helmCommand == "upgrade" {
		// If an operatorConfigurationValues has been provided with telemetry collection enabled, collectors will be
		// deployed, and we should wait until they are ready. If this is a helm upgrade, the collectors should already
		// be running anyway.
		waitForCollectorToStart(operatorNamespace, operatorHelmChart)
	}

	return nil
}

func addHelmParametersForImages(arguments []string, images Images) []string {
	arguments = setIfNotEmpty(arguments, "operator.image.repository", images.operator.repository)
	arguments = setIfNotEmpty(arguments, "operator.image.tag", images.operator.tag)
	arguments = setIfNotEmpty(arguments, "operator.image.digest", images.operator.digest)
	arguments = setIfNotEmpty(arguments, "operator.image.pullPolicy", images.operator.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.repository", images.instrumentation.repository)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.tag", images.instrumentation.tag)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.digest", images.instrumentation.digest)
	arguments = setIfNotEmpty(arguments, "operator.initContainerImage.pullPolicy", images.instrumentation.pullPolicy)

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

	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSyncImage.repository",
		images.fileLogOffsetSync.repository)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSyncImage.tag", images.fileLogOffsetSync.tag)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSyncImage.digest",
		images.fileLogOffsetSync.digest)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetSyncImage.pullPolicy",
		images.fileLogOffsetSync.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetVolumeOwnershipImage.repository",
		images.fileLogOffsetVolumeOwnership.repository)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetVolumeOwnershipImage.tag",
		images.fileLogOffsetVolumeOwnership.tag)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetVolumeOwnershipImage.digest",
		images.fileLogOffsetVolumeOwnership.digest)
	arguments = setIfNotEmpty(arguments, "operator.filelogOffsetVolumeOwnershipImage.pullPolicy",
		images.fileLogOffsetVolumeOwnership.pullPolicy)

	arguments = setIfNotEmpty(arguments, "operator.targetAllocatorImage.repository", images.targetAllocator.repository)
	arguments = setIfNotEmpty(arguments, "operator.targetAllocatorImage.tag", images.targetAllocator.tag)
	arguments = setIfNotEmpty(arguments, "operator.targetAllocatorImage.digest", images.targetAllocator.digest)
	arguments = setIfNotEmpty(arguments, "operator.targetAllocatorImage.pullPolicy", images.targetAllocator.pullPolicy)

	return arguments
}

func setAdditionalHelmParameters(arguments []string, additionalHelmParameters map[string]string) []string {
	for key, value := range additionalHelmParameters {
		arguments = setIfNotEmpty(arguments, key, value)
	}
	return arguments
}

func setIfNotEmpty(arguments []string, key string, value string) []string {
	if value != "" {
		return setHelmParameter(arguments, key, value)
	}
	return arguments
}

func setHelmParameter(arguments []string, key string, value interface{}) []string {
	arguments = append(arguments, "--set")
	arguments = append(arguments, fmt.Sprintf("%s=%v", key, value))
	return arguments
}

func ensureDash0OperatorHelmRepoIsInstalledAndUpToDate(
	chart string,
	chartUrl string,
) {
	isLocal := isLocalHelmChart(chart)
	if isLocal && chartUrl == "" {
		// local Helm chart sources, no further action required
		return
	} else if isLocal {
		Fail("Invalid test setup: When setting a URL for the Helm chart (OPERATOR_HELM_CHART_URL), you also need to " +
			"provide a custom name (OPERATOR_HELM_CHART).")
	} else if chartUrl == "" {
		Fail("Invalid test setup: When setting a non-standard name for the operator Helm chart " +
			"(OPERATOR_HELM_CHART), you also need to provide a URL from where to install it (OPERATOR_HELM_CHART_URL).")
	} else if !strings.Contains(chart, "/") {
		Fail("Invalid test setup: When using a Helm chart URL (OPERATOR_HELM_CHART_URL), the provided Helm chart " +
			"name (OPERATOR_HELM_CHART) needs to have the format ${repository}/${chart_name}.")
	}

	repositoryName := chart[:strings.LastIndex(chart, "/")]
	By(fmt.Sprintf("checking whether the operator Helm chart repo %s (%s) has been installed",
		repositoryName, chartUrl))
	repoList, err := run(exec.Command("helm", "repo", "list"))
	Expect(err).To(Or(Not(HaveOccurred()), MatchError(ContainSubstring("no repositories to show"))))
	if !regexp.MustCompile(
		fmt.Sprintf("%s\\s+%s", repositoryName, chartUrl)).MatchString(repoList) {
		e2ePrint(
			"The helm repo %s (%s) has not been found, adding it now.\n",
			repositoryName,
			chartUrl,
		)
		Expect(runAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"add",
				repositoryName,
				chartUrl,
				"--force-update",
			))).To(Succeed())
		Expect(runAndIgnoreOutput(exec.Command("helm", "repo", "update"))).To(Succeed())
	} else {
		e2ePrint(
			"The helm repo %s (%s) is already installed, updating it now.\n",
			repositoryName,
			chartUrl,
		)
		Expect(runAndIgnoreOutput(
			exec.Command(
				"helm",
				"repo",
				"update",
			))).To(Succeed())
	}
}

func undeployOperator(operatorNamespace string) {
	By("undeploying the operator")
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

func verifyOperatorManagerPodMemoryUsageIsReasonable() {
	var memoryUsage int64
	Eventually(func(g Gomega) {
		memoryUsage = readOperatorManagerMemoryUsage(g)
	}, 60*time.Second, 1*time.Second).Should(Succeed())
	e2ePrint("operator manager pod memory usage: %d", memoryUsage)
	Expect(memoryUsage).Should(
		BeNumerically("<=", int64(128_000_000)),
		"The operator manager pod is using more memory than expected. Check for a potential memory leak.",
	)
}

func readOperatorManagerMemoryUsage(g Gomega) int64 {
	By("getting operator manager memory usage")
	metricsOutput, err := run(exec.Command(
		"kubectl",
		"get",
		"--namespace",
		operatorNamespace,
		"pods.metrics.k8s.io",
		"--selector", "app.kubernetes.io/name=dash0-operator",
		"--selector", "app.kubernetes.io/component=controller",
		"--output",
		"json",
	))
	g.Expect(err).NotTo(HaveOccurred())
	var metrics map[string]interface{}
	g.Expect(
		json.Unmarshal([]byte(metricsOutput), &metrics)).To(
		Succeed(),
		"cannot parse metrics output\n%s",
		metricsOutput,
	)
	g.Expect(metrics).To(HaveKey("items"), "unexpected metrics output (no items)\n%s", metricsOutput)
	itemsRaw := metrics["items"]
	items, ok := itemsRaw.([]interface{})
	g.Expect(ok).To(BeTrue(), "unexpected metrics output (items is not an array)\n%s", metricsOutput)
	g.Expect(items).To(HaveLen(1), "unexpected metrics output (zero items or more than one item)\n%s", metricsOutput)

	itemRaw := items[0]
	item, ok := itemRaw.(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "unexpected metrics output (items[0] is not a map)\n%s", metricsOutput)
	g.Expect(item).To(
		HaveKey("containers"), "unexpected metrics output (items[0] has no containers)\n%s", metricsOutput)
	containersRaw := item["containers"]
	containers, ok := containersRaw.([]interface{})
	g.Expect(ok).To(BeTrue(), "unexpected metrics output (items[0].containers is not an array)\n%s", metricsOutput)
	for i, containerRaw := range containers {
		container, ok := containerRaw.(map[string]interface{})
		g.Expect(ok).To(BeTrue(),
			"unexpected metrics output (items[0].containers[%d] is not a map)\n%s", i, metricsOutput)
		g.Expect(container).To(
			HaveKey("name"),
			"unexpected metrics output (items[0].containers[%d] has no name)\n%s", i, metricsOutput)
		if container["name"] == "manager" {
			g.Expect(container).To(
				HaveKey("usage"),
				"unexpected metrics output (items[0].containers[%d] has no usage)\n%s", i, metricsOutput)
			usage, ok := container["usage"].(map[string]interface{})
			g.Expect(ok).To(BeTrue(),
				"unexpected metrics output (items[0].containers[%d].usage is not a map)\n%s", i, metricsOutput)
			g.Expect(usage).To(
				HaveKey("memory"),
				"unexpected metrics output (items[0].containers[%d].usage has no memory)\n%s", i, metricsOutput)
			memoryUsageStr, ok := usage["memory"].(string)
			g.Expect(ok).To(
				BeTrue(),
				"unexpected metrics output (items[0].containers[%d].usage.memory is not a string)\n%s",
				i,
				metricsOutput,
			)
			memoryUsage, err := resource.ParseQuantity(memoryUsageStr)
			g.Expect(err).To(
				Succeed(),
				"unexpected metrics output (cannot parse operator manager pod memory usage: \"%s\")\n%s",
				memoryUsageStr,
				metricsOutput,
			)
			return memoryUsage.Value()
		}
	}
	g.Expect(false).To(BeTrue(), "could not read the operator manager pod's memory usage\n")
	return -1
}

func failOnPodCrashOrOOMKill(cleanupSteps *neccessaryCleanupSteps) chan bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	stop := make(chan bool, 1)
	e2ePrint("Starting pod crash/oom kill detection.\n")
	go func() {
		defer GinkgoRecover()
		for {
			select {
			case <-ticker.C:
				verifyNoPodCrashOrOOMKill()
			case <-stop:
				e2ePrint("Stopping pod crash/oom kill detection.\n")
				ticker.Stop()
				return
			}
		}
	}()
	cleanupSteps.stopOOMDetection = true
	return stop
}

func verifyNoPodCrashOrOOMKill() {
	output, err := run(exec.Command(
		"kubectl",
		"--namespace",
		operatorNamespace,
		"get",
		"pods",
	), false)
	Expect(err).ToNot(HaveOccurred())
	if strings.Contains(output, "OOMKilled") {
		Fail("A pod has been OOMKilled:\n" + output)
	}
	lines := getNonEmptyLines(output)
	for _, line := range lines {
		if strings.Contains(line, "CrashLoopBackOff") {
			podName := strings.Split(line, " ")[0]
			// The collector pods are deliberately restarted when a config change occurs, sometimes they show the status
			// CrashLoopBackOff for a short time due to that. We need to find a better way to detect actual collector
			// crashes, for example by inspecting the last exit code. If it is 143 (terminated by external signal), it
			// is probably fine, for other exit codes we still might want to fail the test suite.
			if !strings.Contains(podName, "collector") {
				Fail("Pod %s is in CrashLoopBackOff:\n" + output)
			}
		}
	}
}
