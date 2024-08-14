// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

type dash0MonitoringTemplateValues struct {
	IngressEndpoint string
}

const (
	dash0MonitoringResourceName = "dash0-monitoring-resource"
)

func deployDash0MonitoringResource(namespace string, operatorNamespace string) {
	truncateExportedTelemetry()

	By(fmt.Sprintf(
		"Deploying the Dash0 monitoring resource to namespace %s, operator namespace is %s",
		namespace, operatorNamespace))

	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-n",
			namespace,
			"-f",
			tmpDash0MonitoringResourceFile.Name(),
		))).To(Succeed())

	// Deploying the Dash0 monitoring resource will trigger creating the default OpenTelemetry collecor instance.
	verifyThatCollectorIsRunning(operatorNamespace)
}

func undeployDash0MonitoringResource(namespace string) {
	truncateExportedTelemetry()

	if tmpDash0MonitoringResourceFile == nil {
		Fail("tmpDash0MonitoringResourceFile is nil, this should not happen")
	}

	By(fmt.Sprintf("Removing the Dash0 monitoring resource from namespace %s", namespace))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"--namespace",
			namespace,
			"-f",
			tmpDash0MonitoringResourceFile.Name(),
			"--ignore-not-found",
		))).To(Succeed())
}

func updateIngressEndpointOfDash0MonitoringResource(namespace string, newIngressEndpoint string) bool {
	return Expect(
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
			"{\"spec\":{\"ingressEndpoint\":\""+newIngressEndpoint+"\"}}",
		))).To(Succeed())
}

func truncateExportedTelemetry() {
	By("truncating old captured telemetry files")
	_ = os.Truncate("test-resources/e2e-test-volumes/otlp-sink/traces.jsonl", 0)
	_ = os.Truncate("test-resources/e2e-test-volumes/otlp-sink/metrics.jsonl", 0)
	_ = os.Truncate("test-resources/e2e-test-volumes/otlp-sink/logs.jsonl", 0)
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
