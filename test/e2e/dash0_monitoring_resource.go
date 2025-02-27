// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dash0MonitoringValues struct {
	InstrumentWorkloads dash0v1alpha1.InstrumentWorkloadsMode
	Endpoint            string
	Token               string
}

const (
	dash0MonitoringResourceName = "dash0-monitoring-resource-e2e"
)

var (
	//go:embed dash0monitoring.e2e.yaml.template
	dash0MonitoringResourceSource   string
	dash0MonitoringResourceTemplate *template.Template

	dash0MonitoringValuesDefault = dash0MonitoringValues{
		InstrumentWorkloads: dash0v1alpha1.All,
	}

	dash0MonitoringValuesWithExport = dash0MonitoringValues{
		InstrumentWorkloads: dash0v1alpha1.All,
		Endpoint:            defaultEndpoint,
		Token:               defaultToken,
	}
)

func renderDash0MonitoringResourceTemplate(dash0MonitoringValues dash0MonitoringValues) string {
	By("render Dash0Monitoring resource template")
	if dash0MonitoringResourceTemplate == nil {
		dash0MonitoringResourceTemplate =
			template.Must(template.New("dash0monitoring").Parse(dash0MonitoringResourceSource))
	}

	var dash0MonitoringResource bytes.Buffer
	Expect(dash0MonitoringResourceTemplate.Execute(&dash0MonitoringResource, dash0MonitoringValues)).To(Succeed())

	renderedResourceFile, err := os.CreateTemp(os.TempDir(), "dash0monitoring-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(renderedResourceFile.Name(), dash0MonitoringResource.Bytes(), 0644)).To(Succeed())

	return renderedResourceFile.Name()
}

func deployDash0MonitoringResource(
	namespace string,
	dash0MonitoringValues dash0MonitoringValues,
	operatorNamespace string,
) {
	renderedResourceFileName := renderDash0MonitoringResourceTemplate(dash0MonitoringValues)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	retryLogger := zap.New()
	By(fmt.Sprintf(
		"deploying the Dash0 monitoring resource to namespace %s with values %v, operator namespace is %s",
		namespace, dash0MonitoringValues, operatorNamespace))
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
	instrumentWorkloadsMode dash0v1alpha1.InstrumentWorkloadsMode,
) {
	updateDash0MonitoringResource(
		namespace,
		fmt.Sprintf(`
{
  "spec": {
    "instrumentWorkloads": "%s"
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
