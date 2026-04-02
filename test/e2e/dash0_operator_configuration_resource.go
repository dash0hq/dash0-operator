// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type dash0OperatorConfigurationValues struct {
	SelfMonitoringEnabled          bool
	Endpoint                       string
	Token                          string
	ApiEndpoint                    string
	ClusterName                    string
	TelemetryCollectionEnabled     bool
	AutoNamespaceMonitoringEnabled bool
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
)

func renderDash0OperatorConfigurationResourceTemplate(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
) string {
	By("rendering Dash0OperatorConfiguration resource template")
	dash0OperatorConfigurationResourceTemplate = initTemplateOnce(
		dash0OperatorConfigurationResourceTemplate,
		dash0OperatorConfigurationResourceSource,
		"dash0operatorconfiguration",
	)
	return renderResourceTemplate(
		dash0OperatorConfigurationResourceTemplate,
		dash0OperatorConfigurationValues,
		"dash0operatorconfiguration",
	)
}

func deployDash0OperatorConfigurationResourceWithRetry(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
	operatorNamespace string,
	operatorHelmChart string,
) {
	renderedResourceFileName := renderDash0OperatorConfigurationResourceTemplate(dash0OperatorConfigurationValues)
	deployRenderedOperatorConfigurationResourceWithRetry(
		dash0OperatorConfigurationValues,
		operatorNamespace,
		operatorHelmChart,
		renderedResourceFileName,
	)
}

func deployRenderedOperatorConfigurationResourceWithRetry(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
	operatorNamespace string,
	operatorHelmChart string,
	renderedResourceFileName string,
) {
	defer func() {
		Expect(os.Remove(renderedResourceFileName)).To(Succeed())
	}()
	By(fmt.Sprintf(
		"deploying the Dash0 operator configuration resource with values %v", dash0OperatorConfigurationValues))
	retryLogger := logd.NewLogger(zap.New())
	err := util.RetryWithCustomBackoff("deploying the Dash0 operator configuration resource", func() error {
		return runAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-f",
			renderedResourceFileName,
		))
	},
		wait.Backoff{
			Duration: 10 * time.Second,
			Steps:    3,
		},
		true,
		true,
		retryLogger,
	)
	Expect(err).ToNot(HaveOccurred())

	waitForOperatorConfigurationResourceToBecomeAvailable()

	if dash0OperatorConfigurationValues.TelemetryCollectionEnabled && dash0OperatorConfigurationValues.Endpoint != "" {
		// Deploying the Dash0 operator configuration resource with an export will trigger creating the default
		// OpenTelemetry collector instance.
		waitForCollectorToStart(operatorNamespace, operatorHelmChart)
	}
}

func waitForOperatorConfigurationResourceToBecomeAvailable() {
	waitForOperatorConfigurationResourceWithNameToBecomeAvailable(dash0OperatorConfigurationResourceName)
}

func waitForAutoOperatorConfigurationResourceToBecomeAvailable() {
	waitForOperatorConfigurationResourceWithNameToBecomeAvailable(util.OperatorConfigurationAutoResourceName)
}

func waitForOperatorConfigurationResourceWithNameToBecomeAvailable(operatorConfigurationResourceName string) {
	By(
		fmt.Sprintf("waiting for the Dash0 operator configuration resource %s to become available",
			operatorConfigurationResourceName,
		))
	Eventually(func(g Gomega) {
		g.Expect(
			runAndIgnoreOutput(exec.Command(
				"kubectl",
				"get",
				"Dash0OperatorConfiguration",
				operatorConfigurationResourceName,
			))).To(Succeed())
	}, 60*time.Second, 1*time.Second).Should(Succeed())
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"wait",
			"Dash0OperatorConfiguration",
			operatorConfigurationResourceName,
			"--for",
			"condition=Available",
			"--timeout",
			"30s",
		))).To(Succeed())
}

func loadOperatorConfigurationResource(
	g Gomega,
	operatorConfigurationResourceName string,
) dash0v1alpha1.Dash0OperatorConfiguration {
	output, err := run(exec.Command(
		"kubectl",
		"get",
		"Dash0OperatorConfiguration",
		operatorConfigurationResourceName,
		"-o",
		"json",
	))
	g.Expect(err).NotTo(HaveOccurred())
	operatorConfiguration := dash0v1alpha1.Dash0OperatorConfiguration{}
	g.Expect(json.Unmarshal([]byte(output), &operatorConfiguration)).To(Succeed())
	return operatorConfiguration
}

func verifyThatAllTelemetrySettingsAreDisabledInOperatorConfiguration(
	g Gomega,
	operatorConfiguration dash0v1alpha1.Dash0OperatorConfiguration,
) {
	spec := operatorConfiguration.Spec
	g.Expect(spec.TelemetryCollection.Enabled).ToNot(BeNil())
	g.Expect(*spec.TelemetryCollection.Enabled).To(BeFalse())
	g.Expect(spec.SelfMonitoring.Enabled).ToNot(BeNil())
	g.Expect(*spec.SelfMonitoring.Enabled).To(BeFalse())
	g.Expect(spec.KubernetesInfrastructureMetricsCollection.Enabled).ToNot(BeNil())
	g.Expect(*spec.KubernetesInfrastructureMetricsCollection.Enabled).To(BeFalse())
	g.Expect(spec.CollectPodLabelsAndAnnotations.Enabled).ToNot(BeNil())
	g.Expect(*spec.CollectPodLabelsAndAnnotations.Enabled).To(BeFalse())
	g.Expect(spec.CollectNamespaceLabelsAndAnnotations.Enabled).ToNot(BeNil())
	g.Expect(*spec.CollectNamespaceLabelsAndAnnotations.Enabled).To(BeFalse())
	g.Expect(spec.PrometheusCrdSupport.Enabled).ToNot(BeNil())
	g.Expect(*spec.PrometheusCrdSupport.Enabled).To(BeFalse())
	g.Expect(spec.Profiling.Enabled).ToNot(BeNil())
	g.Expect(*spec.Profiling.Enabled).To(BeFalse())
}

func updateOperatorConfigurationExportEndpoint(
	newEndpoint string,
) {
	jsonPatch := fmt.Sprintf(`[{
    "op":"replace",
    "path":"/spec/exports/0/dash0/endpoint",
    "value":"%s"
	}]`, newEndpoint)
	updateDash0OperatorConfigurationResource(jsonPatch)
}

func updateOperatorConfigurationAutoNamespaceMonitoringLabelSelector(
	newLabelSelector string,
) {
	jsonPatch := fmt.Sprintf(`[{
   "op":"replace",
   "path":"/spec/autoMonitorNamespaces/labelSelector",
   "value":"%s"
	}]`, newLabelSelector)
	updateDash0OperatorConfigurationResource(jsonPatch)
}

func updateOperatorConfigurationMonitoringTemplateInstrumentWorkloadsMode(
	newInstrumentWorkloadsMode dash0common.InstrumentWorkloadsMode,
) {
	jsonPatch := fmt.Sprintf(`[{
   "op":"replace",
   "path":"/spec/monitoringTemplate/spec/instrumentWorkloads/mode",
   "value":"%s"
	}]`, newInstrumentWorkloadsMode)
	updateDash0OperatorConfigurationResource(jsonPatch)
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
			"json",
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
