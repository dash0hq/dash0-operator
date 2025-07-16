// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dash0MonitoringValues struct {
	InstrumentWorkloadsMode dash0common.InstrumentWorkloadsMode
	Endpoint                string
	Token                   string
	Filter                  string
	Transform               string
}

const (
	dash0MonitoringResourceName = "dash0-monitoring-resource-e2e"
)

var (
	//go:embed dash0monitoring.e2e.yaml.template
	dash0MonitoringResourceSource   string
	dash0MonitoringResourceTemplate *template.Template

	//go:embed dash0monitoring.e2e.v1alpha1.yaml.template
	dash0MonitoringResourceV1Alpha1Source   string
	dash0MonitoringResourceV1Alpha1Template *template.Template

	dash0MonitoringValuesDefault = dash0MonitoringValues{
		InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
	}

	dash0MonitoringValuesWithExport = dash0MonitoringValues{
		InstrumentWorkloadsMode: dash0common.InstrumentWorkloadsModeAll,
		Endpoint:                defaultEndpoint,
		Token:                   defaultToken,
	}
)

func renderDash0MonitoringResourceTemplate(dash0MonitoringValues dash0MonitoringValues) string {
	By("rendering Dash0Monitoring resource template")
	dash0MonitoringResourceTemplate = initTemplateOnce(
		dash0MonitoringResourceTemplate,
		dash0MonitoringResourceSource,
		"dash0monitoring",
	)
	return renderResourceTemplate(dash0MonitoringResourceTemplate, dash0MonitoringValues, "dash0monitoring")
}

func renderDash0MonitoringResourceTemplateV1Alpha1(dash0MonitoringValues dash0MonitoringValues) string {
	By("rendering Dash0Monitoring resource template")
	dash0MonitoringResourceTemplate = initTemplateOnce(
		dash0MonitoringResourceV1Alpha1Template,
		dash0MonitoringResourceV1Alpha1Source,
		"dash0monitoring",
	)
	return renderResourceTemplate(dash0MonitoringResourceTemplate, dash0MonitoringValues, "dash0monitoring-v1alpha1")
}

func deployDash0MonitoringResource(
	namespace string,
	dash0MonitoringValues dash0MonitoringValues,
	operatorNamespace string,
) {
	renderedResourceFileName := renderDash0MonitoringResourceTemplate(dash0MonitoringValues)
	deployRenderedMonitoringResource(
		namespace,
		dash0MonitoringValues,
		operatorNamespace,
		renderedResourceFileName,
	)
}

func deployDash0MonitoringResourceV1Alpha1(
	namespace string,
	dash0MonitoringValues dash0MonitoringValues,
	operatorNamespace string,
) {
	renderedResourceFileName := renderDash0MonitoringResourceTemplateV1Alpha1(dash0MonitoringValues)
	deployRenderedMonitoringResource(
		namespace,
		dash0MonitoringValues,
		operatorNamespace,
		renderedResourceFileName,
	)
}

func deployRenderedMonitoringResource(
	namespace string,
	dash0MonitoringValues dash0MonitoringValues,
	operatorNamespace string,
	renderedResourceFileName string,
) {
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()
	retryLogger := zap.New()
	By(fmt.Sprintf(
		"deploying the Dash0 monitoring resource to namespace %s with values %v from file %s, operator namespace is %s",
		namespace, dash0MonitoringValues, renderedResourceFileName, operatorNamespace))
	err := util.RetryWithCustomBackoff("deploying the Dash0 monitoring resource to namespace", func() error {
		return runAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-n",
			namespace,
			"-f",
			renderedResourceFileName,
		))
	},
		wait.Backoff{
			Duration: 10 * time.Second,
			Steps:    3,
		},
		true,
		&retryLogger,
	)
	Expect(err).ToNot(HaveOccurred())

	waitForMonitoringResourceToBecomeAvailable(namespace)

	if dash0MonitoringValues.Endpoint != "" {
		// Deploying the Dash0 monitoring with an export will trigger creating the OpenTelemetry collector resources,
		// assuming there is no operator configuration resource with an export.
		waitForCollectorToStart(operatorNamespace, operatorHelmChart)
	}
}

func waitForMonitoringResourceToBecomeAvailable(namespace string) {
	By("waiting for the Dash0 monitoring resource to become available")
	Eventually(func(g Gomega) {
		g.Expect(
			runAndIgnoreOutput(exec.Command(
				"kubectl",
				"get",
				"--namespace",
				namespace,
				"dash0monitorings.operator.dash0.com/dash0-monitoring-resource-e2e",
			))).To(Succeed())
	}, 60*time.Second, 1*time.Second).Should(Succeed())
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"wait",
			"--namespace",
			namespace,
			"dash0monitorings.operator.dash0.com/dash0-monitoring-resource-e2e",
			"--for",
			"condition=Available",
			"--timeout",
			"30s",
		))).To(Succeed())
}

func updateInstrumentWorkloadsModeOfDash0MonitoringResource(
	namespace string,
	instrumentWorkloadsMode dash0common.InstrumentWorkloadsMode,
) {
	updateDash0MonitoringResource(
		namespace,
		fmt.Sprintf(`
{
  "spec": {
    "instrumentWorkloads": {
      "mode": "%s"
    }
  }
}
`, instrumentWorkloadsMode),
	)
}

func updateDash0MonitoringResource(
	namespace string,
	jsonPatch string,
) {
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"patch",
			"-n",
			namespace,
			"Dash0Monitoring",
			dash0MonitoringResourceName,
			"--type",
			"merge",
			"-p",
			jsonPatch,
		))).To(Succeed())
}

func undeployDash0MonitoringResource(namespace string) {
	By(fmt.Sprintf("removing the Dash0 monitoring resource from namespace %s", namespace))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"dash0monitoring",
			dash0MonitoringResourceName,
			"--ignore-not-found",
		))).To(Succeed())
}

func verifyDash0MonitoringResourceDoesNotExist(g Gomega, namespace string) {
	output, err := run(exec.Command(
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
