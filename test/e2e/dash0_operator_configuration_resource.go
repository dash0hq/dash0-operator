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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dash0OperatorConfigurationValues struct {
	SelfMonitoringEnabled bool
	Endpoint              string
	Token                 string
}

const (
	dash0OperatorConfigurationResourceName = "dash0-operator-configuration-resource-e2e"

	// We are using the Dash0 exporter which uses a gRPC exporter under the hood, so actually omitting the http://
	// scheme would be fine, but for self-monitoring we would prepend https:// to URLs without scheme, see comment in
	// self_monitoring.go#prependProtocol. Since the OTLP sink does not serve https, we use a URL with http:// to avoid
	// this behavior.
	defaultEndpoint = "http://otlp-sink.otlp-sink.svc.cluster.local:4317"

	// We only need a non-empty token to pass the validation in startup.auto_operator_configuration_handler.go,
	// we do not actually send data to a Dash0 backend so no real token is required.
	defaultToken = "dummy-token"
)

var (
	//go:embed dash0operatorconfiguration.e2e.yaml.template
	dash0OperatorConfigurationResourceSource   string
	dash0OperatorConfigurationResourceTemplate *template.Template

	defaultDash0OperatorConfigurationValues = dash0OperatorConfigurationValues{
		SelfMonitoringEnabled: true,
		Endpoint:              defaultEndpoint,
		Token:                 defaultToken,
	}
)

func renderDash0OperatorConfigurationResourceTemplate(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
) string {
	By("render Dash0OperatorConfiguration resource template")
	if dash0OperatorConfigurationResourceTemplate == nil {
		dash0OperatorConfigurationResourceTemplate =
			template.Must(template.New("dash0operatorconfiguration").Parse(dash0OperatorConfigurationResourceSource))
	}

	var dash0OperatorConfigurationResource bytes.Buffer
	Expect(
		dash0OperatorConfigurationResourceTemplate.Execute(
			&dash0OperatorConfigurationResource,
			dash0OperatorConfigurationValues,
		)).To(Succeed())

	renderedResourceFile, err := os.CreateTemp(os.TempDir(), "dash0operatorconfiguration-*.yaml")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(renderedResourceFile.Name(), dash0OperatorConfigurationResource.Bytes(), 0644)).To(Succeed())

	return renderedResourceFile.Name()
}

func deployDash0OperatorConfigurationResource(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
) {
	renderedResourceFileName := renderDash0OperatorConfigurationResourceTemplate(dash0OperatorConfigurationValues)
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()

	By(fmt.Sprintf(
		"deploying the Dash0 operator configuration resource with values %v", dash0OperatorConfigurationValues))
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-f",
			renderedResourceFileName,
		))).To(Succeed())
}

func waitForAutoOperatorConfigurationResourceToBecomeAvailable() {
	By("waiting for the automatically create Dash0 operator configuration resource to become available")
	Eventually(func(g Gomega) {
		g.Expect(
			runAndIgnoreOutput(exec.Command(
				"kubectl",
				"get",
				"dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration-auto-resource",
			))).To(Succeed())
	}, 30*time.Second, 1*time.Second).Should(Succeed())
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"wait",
			"dash0operatorconfigurations.operator.dash0.com/dash0-operator-configuration-auto-resource",
			"--for",
			"condition=Available",
			"--timeout",
			"30s",
		))).To(Succeed())
}

func updateEndpointOfDash0OperatorConfigurationResource(
	newEndpoint string,
) {
	updateDash0OperatorConfigurationResource(
		fmt.Sprintf("{\"spec\":{\"export\":{\"dash0\":{\"endpoint\":\"%s\"}}}}", newEndpoint),
	)
}

func updateDash0OperatorConfigurationResource(
	jsonPatch string,
) {
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"patch",
			"Dash0OperatorConfiguration",
			dash0OperatorConfigurationResourceName,
			"--type",
			"merge",
			"-p",
			jsonPatch,
		))).To(Succeed())
}

func undeployDash0OperatorConfigurationResource() {
	By("removing the Dash0 operator configuration resource")
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"delete",
			"dash0operatorconfiguration",
			dash0OperatorConfigurationResourceName,
			"--ignore-not-found",
		))).To(Succeed())
}
