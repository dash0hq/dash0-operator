// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dash0ApiResourceValues struct {
	Dash0ComEnabled string
}

const (
	syntheticCheckName  = "synthetic-check-e2e-test"
	viewName            = "view-e2e-test"
	persesDashboardName = "perses-dashboard-e2e-test"
	prometheusRuleName  = "prometheus-rules-e2e-test"
)

var (
	//go:embed dash0syntheticcheck.yaml.template
	syntheticCheckSource   string
	syntheticCheckTemplate *template.Template

	//go:embed dash0view.yaml.template
	viewSource   string
	viewTemplate *template.Template

	//go:embed persesdashboard.yaml.template
	persesDashboardSource   string
	persesDashboardTemplate *template.Template

	//go:embed prometheusrule.yaml.template
	prometheusRuleSource   string
	prometheusRuleTemplate *template.Template
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

func renderPersesDashboardTemplate(values dash0ApiResourceValues) string {
	persesDashboardTemplate = initTemplateOnce(
		persesDashboardTemplate,
		persesDashboardSource,
		"persesdashboard",
	)
	return renderResourceTemplate(persesDashboardTemplate, values, "persesdashboard")
}

func deployPersesDashboardResource(
	namespace string,
	values dash0ApiResourceValues,
) {
	renderedResourceFileName := renderPersesDashboardTemplate(values)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying a PersesDashboard resource to namespace %s with values %v", namespace, values))
	Expect(runAndIgnoreOutput(exec.Command(
		"kubectl",
		"apply",
		"-n",
		namespace,
		"-f",
		renderedResourceFileName,
	))).To(Succeed())
}

func setOptOutLabelInPersesDashboard(namespace string, value string) {
	By(fmt.Sprintf("setting the opt-out label in the Perses dashboard with value %s", value))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"label",
			"-n",
			namespace,
			"--overwrite",
			"PersesDashboard",
			persesDashboardName,
			fmt.Sprintf("dash0.com/enable=%s", value),
		)),
	).To(Succeed())
}

func removePersesDashboardResource(namespace string) {
	_ = runAndIgnoreOutput(exec.Command(
		"kubectl",
		"delete",
		"--ignore-not-found",
		"-n",
		namespace,
		"PersesDashboard",
		persesDashboardName,
	))
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

func removeDash0ApiSyncResources(namespace string) {
	removeSyntheticCheckResource(namespace)
	removePersesDashboardResource(namespace)
	removePrometheusRuleResource(namespace)
	removeViewResource(namespace)
}
