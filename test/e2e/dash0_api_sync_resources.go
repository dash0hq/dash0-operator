// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dash0ApiResourceValues struct {
	Dash0ComEnabled string
}

const (
	syntheticCheckName        = "synthetic-check-e2e-test"
	viewName                  = "view-e2e-test"
	persesDashboardNamePrefix = "perses-dashboard-e2e-test"
	persesDashboardV1Alpha1   = "v1alpha1"
	persesDashboardV1Alpha2   = "v1alpha2"
	prometheusRuleName        = "prometheus-rules-e2e-test"
	notificationChannelName   = "notification-channel-e2e-test"
	samplingRuleName          = "sampling-rule-e2e-test"
	signalToMetricsName       = "signal-to-metrics-e2e-test"
	spamFilterName            = "spam-filter-e2e-test"
)

var (
	//go:embed dash0syntheticcheck.yaml.template
	syntheticCheckSource   string
	syntheticCheckTemplate *template.Template

	//go:embed dash0view.yaml.template
	viewSource   string
	viewTemplate *template.Template

	//go:embed persesdashboard.v1alpha1.yaml.template
	persesDashboardV1Alpha1Source   string
	persesDashboardV1Alpha1Template *template.Template

	//go:embed persesdashboard.v1alpha2.yaml.template
	persesDashboardV1Alpha2Source   string
	persesDashboardV1Alpha2Template *template.Template

	//go:embed prometheusrule.yaml.template
	prometheusRuleSource   string
	prometheusRuleTemplate *template.Template

	//go:embed dash0notificationchannel.yaml.template
	notificationChannelSource   string
	notificationChannelTemplate *template.Template

	//go:embed dash0samplingrule.yaml.template
	samplingRuleSource   string
	samplingRuleTemplate *template.Template

	//go:embed dash0signaltometrics.yaml.template
	signalToMetricsSource   string
	signalToMetricsTemplate *template.Template

	//go:embed dash0spamfilter.yaml.template
	spamFilterSource   string
	spamFilterTemplate *template.Template
)

func deployThirdPartyCrds(cleanupSteps *neccessaryCleanupSteps) {
	By("deploying PersesDashboard and PrometheusRule CRDs")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		"test/util/crds/perses.dev_persesdashboards.yaml",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		"test/util/crds/monitoring.coreos.com_prometheusrules.yaml",
	))).To(Succeed())
	cleanupSteps.removeThirdPartyCrds = true
}

func removeThirdPartyCrds(cleanupSteps *neccessaryCleanupSteps) {
	if !cleanupSteps.removeThirdPartyCrds {
		return
	}
	By("removing PersesDashboard and PrometheusRule CRDs")
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-f",
		"test/util/crds/perses.dev_persesdashboards.yaml",
	))).To(Succeed())
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-f",
		"test/util/crds/monitoring.coreos.com_prometheusrules.yaml",
	))).To(Succeed())
}

func renderSyntheticCheckTemplate(values dash0ApiResourceValues) string {
	syntheticCheckTemplate = initTemplateOnce(
		syntheticCheckTemplate,
		syntheticCheckSource,
		"syntheticcheck",
	)
	return renderResourceTemplate(syntheticCheckTemplate, values, "syntheticcheck")
}

func deploySyntheticCheckResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderSyntheticCheckTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a SyntheticCheck resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInSyntheticCheck(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the synthetic check with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"Dash0SyntheticCheck",
			syntheticCheckName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removeSyntheticCheckResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"Dash0SyntheticCheck",
		syntheticCheckName,
	))
}

func renderViewTemplate(values dash0ApiResourceValues) string {
	viewTemplate = initTemplateOnce(
		viewTemplate,
		viewSource,
		"dash0view",
	)
	return renderResourceTemplate(viewTemplate, values, "dash0view")
}

func deployViewResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderViewTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a Dash0View resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInView(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the view with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"Dash0View",
			viewName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removeViewResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"Dash0View",
		viewName,
	))
}

func renderPersesDashboardTemplate(version string, values dash0ApiResourceValues) string {
	switch version {
	case persesDashboardV1Alpha1:
		persesDashboardV1Alpha1Template = initTemplateOnce(
			persesDashboardV1Alpha1Template,
			persesDashboardV1Alpha1Source,
			"persesdashboard-v1alpha1",
		)
		return renderResourceTemplate(persesDashboardV1Alpha1Template, values, "persesdashboard-v1alpha1")
	case persesDashboardV1Alpha2:
		persesDashboardV1Alpha2Template = initTemplateOnce(
			persesDashboardV1Alpha2Template,
			persesDashboardV1Alpha2Source,
			"persesdashboard-v1alpha2",
		)
		return renderResourceTemplate(persesDashboardV1Alpha2Template, values, "persesdashboard-v1alpha2")
	default:
		Fail(fmt.Sprintf("unsupported PersesDashboard template version %q (must be v1alpha1 or v1alpha2)", version))
		return ""
	}
}

func deployPersesDashboardResource(
	namespace string,
	version string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderPersesDashboardTemplate(version, values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a PersesDashboard %s resource to namespace %s with values %v",
		version, namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInPersesDashboard(namespace string, version string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the Perses dashboard with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"PersesDashboard",
			persesDashboardName(version),
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removePersesDashboardResource(namespace string, version string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"PersesDashboard",
		persesDashboardName(version),
	))
}

func persesDashboardName(version string) string {
	return fmt.Sprintf("%s-%s", persesDashboardNamePrefix, version)
}

func renderPrometheusRuleTemplate(values dash0ApiResourceValues) string {
	prometheusRuleTemplate = initTemplateOnce(
		prometheusRuleTemplate,
		prometheusRuleSource,
		"prometheusrule",
	)
	return renderResourceTemplate(prometheusRuleTemplate, values, "prometheusrule")
}

func deployPrometheusRuleResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderPrometheusRuleTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a PrometheusRule resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInPrometheusRule(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the Prometheus rule resource with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"PrometheusRule",
			prometheusRuleName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removePrometheusRuleResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"PrometheusRule",
		prometheusRuleName,
	))
}

func renderNotificationChannelTemplate(values dash0ApiResourceValues) string {
	notificationChannelTemplate = initTemplateOnce(
		notificationChannelTemplate,
		notificationChannelSource,
		"dash0notificationchannel",
	)
	return renderResourceTemplate(notificationChannelTemplate, values, "dash0notificationchannel")
}

func deployNotificationChannelResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderNotificationChannelTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a Dash0NotificationChannel resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInNotificationChannel(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the notification channel with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"Dash0NotificationChannel",
			notificationChannelName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removeNotificationChannelResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"Dash0NotificationChannel",
		notificationChannelName,
	))
}

func renderSamplingRuleTemplate(values dash0ApiResourceValues) string {
	samplingRuleTemplate = initTemplateOnce(
		samplingRuleTemplate,
		samplingRuleSource,
		"dash0samplingrule",
	)
	return renderResourceTemplate(samplingRuleTemplate, values, "dash0samplingrule")
}

// Dash0SamplingRule is cluster-scoped, so the helpers below do not take a namespace.

func deploySamplingRuleResource(values dash0ApiResourceValues) {
	renderedResourceFileName := renderSamplingRuleTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a Dash0SamplingRule resource with values %v", values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func renderSpamFilterTemplate(values dash0ApiResourceValues) string {
	spamFilterTemplate = initTemplateOnce(
		spamFilterTemplate,
		spamFilterSource,
		"dash0spamfilter",
	)
	return renderResourceTemplate(spamFilterTemplate, values, "dash0spamfilter")
}

func deploySpamFilterResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderSpamFilterTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a Dash0SpamFilter resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInSamplingRule(value string) {
	By(fmt.Sprintf("setting the opt-out label in the sampling rule with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"--overwrite",
			"Dash0SamplingRule",
			samplingRuleName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func setOptOutLabelInSpamFilter(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the spam filter with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"Dash0SpamFilter",
			spamFilterName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removeSamplingRuleResource() {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"Dash0SamplingRule",
		samplingRuleName,
	))
}

func removeSpamFilterResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"Dash0SpamFilter",
		spamFilterName,
	))
}

func renderSignalToMetricsTemplate(values dash0ApiResourceValues) string {
	signalToMetricsTemplate = initTemplateOnce(
		signalToMetricsTemplate,
		signalToMetricsSource,
		"signaltometrics",
	)
	return renderResourceTemplate(signalToMetricsTemplate, values, "signaltometrics")
}

func deploySignalToMetricsResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderSignalToMetricsTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a Dash0SignalToMetrics resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInSignalToMetrics(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the signal-to-metrics rule with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"Dash0SignalToMetrics",
			signalToMetricsName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removeSignalToMetricsResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"Dash0SignalToMetrics",
		signalToMetricsName,
	))
}

func removeDash0ApiSyncResources(namespace string) {
	removeSyntheticCheckResource(namespace)
	removePersesDashboardResource(namespace, persesDashboardV1Alpha1)
	removePersesDashboardResource(namespace, persesDashboardV1Alpha2)
	removePrometheusRuleResource(namespace)
	removeViewResource(namespace)
	removeNotificationChannelResource(namespace)
	removeSamplingRuleResource()
	removeSignalToMetricsResource(namespace)
	removeSpamFilterResource(namespace)
}

// verifyPersesDashboardCrdConversionWebhookConfigured polls the perses.dev PersesDashboard CRD and asserts that the Dash0
// operator has patched its spec.conversion stanza to point at the  operator's webhook. The operator applies the patch at startup;
// this verification proves the patch landed before any dashboard write reaches the API server. Without it, v1alpha1 dashboards
// would be silently pruned to the v1alpha2 storage schema.
func verifyPersesDashboardCrdConversionWebhookConfigured(operatorNamespace string) {
	By("verifying the Dash0 operator has patched the perses.dev PersesDashboard CRD's conversion webhook")
	Eventually(func(g Gomega) {
		output, err := run(exec.Command(
			"kubectl",
			"get",
			"crd",
			"persesdashboards.perses.dev",
			"-o",
			"jsonpath={.spec.conversion.strategy}|"+
				"{.spec.conversion.webhook.clientConfig.service.namespace}|"+
				"{.spec.conversion.webhook.clientConfig.service.name}|"+
				"{.spec.conversion.webhook.clientConfig.service.path}|"+
				"{.spec.conversion.webhook.clientConfig.service.port}",
		))
		g.Expect(err).NotTo(HaveOccurred())
		parts := strings.Split(strings.TrimSpace(output), "|")
		g.Expect(parts).To(HaveLen(5), "unexpected jsonpath output: %q", output)
		g.Expect(parts[0]).To(Equal("Webhook"), "expected conversion strategy Webhook, got %q (full output: %q)", parts[0], output)
		g.Expect(parts[1]).To(Equal(operatorNamespace), "expected service namespace %q, got %q", operatorNamespace, parts[1])
		g.Expect(parts[2]).To(Equal("dash0-operator-webhook-service"))
		g.Expect(parts[3]).To(Equal("/convert-persesdashboard"))
		g.Expect(parts[4]).To(Equal("443"))
	}, 30*time.Second, 1*time.Second).Should(Succeed())
}
