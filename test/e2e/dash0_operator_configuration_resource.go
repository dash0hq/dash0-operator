// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"text/template"
	"time"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/agent0connector"
	"github.com/dash0hq/dash0-operator/internal/syntheticsworker"
	"github.com/dash0hq/dash0-operator/internal/util"
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
	dash0OperatorConfigurationResourceManuallyManagedName      = "dash0-operator-configuration-resource-e2e"
	dash0OperatorConfigurationResourceAutomaticallyManagedName = "dash0-operator-configuration-auto-resource"

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

func deployDash0OperatorConfigurationResource(
	dash0OperatorConfigurationValues dash0OperatorConfigurationValues,
	operatorNamespace string,
	operatorHelmChart string,
) {
	renderedResourceFileName := renderDash0OperatorConfigurationResourceTemplate(dash0OperatorConfigurationValues)
	deployRenderedOperatorConfigurationResource(
		dash0OperatorConfigurationValues,
		operatorNamespace,
		operatorHelmChart,
		renderedResourceFileName,
	)
}

func deployRenderedOperatorConfigurationResource(
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
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"apply",
			"-f",
			renderedResourceFileName,
		))).To(Succeed())

	waitForOperatorConfigurationResourceToBecomeAvailable()

	if dash0OperatorConfigurationValues.TelemetryCollectionEnabled && dash0OperatorConfigurationValues.Endpoint != "" {
		// Deploying the Dash0 operator configuration resource with an export will trigger creating the default
		// OpenTelemetry collector instance.
		waitForCollectorToStart(operatorNamespace, operatorHelmChart)
	}
}

func waitForOperatorConfigurationResourceToBecomeAvailable() {
	waitForOperatorConfigurationResourceWithNameToBecomeAvailable(dash0OperatorConfigurationResourceManuallyManagedName)
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
	g.Expect(spec.CollectNodeLabelsAndAnnotations.Enabled).ToNot(BeNil())
	g.Expect(*spec.CollectNodeLabelsAndAnnotations.Enabled).To(BeFalse())
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

func updateOperatorConfigurationAutoNamespaceMonitoringEnabled(
	enabled bool,
) {
	jsonPatch := fmt.Sprintf(`[{
   "op":"replace",
   "path":"/spec/autoMonitorNamespaces/enabled",
   "value":%t
	}]`, enabled)
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
			dash0OperatorConfigurationResourceManuallyManagedName,
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
			dash0OperatorConfigurationResourceManuallyManagedName,
			"--ignore-not-found",
		))).To(Succeed())
}

// workloadDeployStatus normalizes Agent0ConnectorStatus and SyntheticsWorkerStatus, which are structurally
// identical but distinct generated types, so verifyOptionalWorkloadIsReportedAsDeployed/Disabled can handle both.
type workloadDeployStatus struct {
	deployed bool
	reason   string
	message  string
}

// verifyOptionalWorkloadIsReportedAsDeployed verifies that the operator reports an optional, operator-managed
// workload (agent0-connector, synthetics-worker) as deployed, both in the status of the operator configuration
// resource and via a Kubernetes event. The status is written on every reconciliation, the event only when the
// outcome changes.
func verifyOptionalWorkloadIsReportedAsDeployed(
	operatorConfigurationResourceName string,
	workloadName string,
	statusFieldName string,
	getStatus func(dash0v1alpha1.Dash0OperatorConfiguration) *workloadDeployStatus,
	expectedReason string,
	eventReasons func(g Gomega, operatorConfigurationResourceUid string) []string,
	deployedEventReason string,
) {
	By(fmt.Sprintf("verifying that the operator configuration resource reports the %s as deployed", workloadName))
	Eventually(func(g Gomega) {
		operatorConfiguration := loadOperatorConfigurationResource(g, operatorConfigurationResourceName)
		status := getStatus(operatorConfiguration)
		g.Expect(status).ToNot(BeNil(),
			fmt.Sprintf("the operator configuration resource has no status.%s entry", statusFieldName))
		g.Expect(status.deployed).To(BeTrue(),
			"the %s is reported as not deployed: %s", workloadName, status.message)
		g.Expect(status.reason).To(Equal(expectedReason))

		// An issue with the workload must not affect the availability of the operator configuration resource, and
		// neither must the absence of one.
		g.Expect(operatorConfiguration.IsAvailable()).To(BeTrue())
		g.Expect(operatorConfiguration.IsDegraded()).To(BeFalse())
	}, 60*time.Second, pollingInterval).Should(Succeed())

	By(fmt.Sprintf("verifying that the operator has written the %s deployed event", workloadName))
	Eventually(func(g Gomega) {
		operatorConfiguration := loadOperatorConfigurationResource(g, operatorConfigurationResourceName)
		g.Expect(eventReasons(g, string(operatorConfiguration.UID))).To(ContainElement(deployedEventReason))
	}, 60*time.Second, pollingInterval).Should(Succeed())
}

// verifyOptionalWorkloadIsReportedAsDisabled verifies that the operator reports an optional, operator-managed
// workload (agent0-connector, synthetics-worker) as not deployed because it has been disabled in the operator
// configuration resource, both in the status of that resource and via a Kubernetes event.
func verifyOptionalWorkloadIsReportedAsDisabled(
	operatorConfigurationResourceName string,
	workloadName string,
	statusFieldName string,
	getStatus func(dash0v1alpha1.Dash0OperatorConfiguration) *workloadDeployStatus,
	expectedReason string,
	eventReasons func(g Gomega, operatorConfigurationResourceUid string) []string,
	disabledEventReason string,
) {
	By(fmt.Sprintf("verifying that the operator configuration resource reports the %s as disabled", workloadName))
	Eventually(func(g Gomega) {
		operatorConfiguration := loadOperatorConfigurationResource(g, operatorConfigurationResourceName)
		status := getStatus(operatorConfiguration)
		g.Expect(status).ToNot(BeNil(),
			fmt.Sprintf("the operator configuration resource has no status.%s entry", statusFieldName))
		g.Expect(status.deployed).To(BeFalse())
		g.Expect(status.reason).To(Equal(expectedReason),
			"the %s is not reported as disabled: %s", workloadName, status.message)

		// Disabling the workload must not affect the availability of the operator configuration resource.
		g.Expect(operatorConfiguration.IsAvailable()).To(BeTrue())
		g.Expect(operatorConfiguration.IsDegraded()).To(BeFalse())
	}, 60*time.Second, pollingInterval).Should(Succeed())

	By(fmt.Sprintf("verifying that the operator has written the %s disabled event", workloadName))
	Eventually(func(g Gomega) {
		operatorConfiguration := loadOperatorConfigurationResource(g, operatorConfigurationResourceName)
		g.Expect(eventReasons(g, string(operatorConfiguration.UID))).To(ContainElement(disabledEventReason))
	}, 60*time.Second, pollingInterval).Should(Succeed())
}

func agent0ConnectorDeployStatus(operatorConfiguration dash0v1alpha1.Dash0OperatorConfiguration) *workloadDeployStatus {
	s := operatorConfiguration.Status.Agent0Connector
	if s == nil {
		return nil
	}
	return &workloadDeployStatus{deployed: s.Deployed, reason: s.Reason, message: s.Message}
}

func verifyAgent0ConnectorIsReportedAsDeployed(operatorConfigurationResourceName string) {
	verifyOptionalWorkloadIsReportedAsDeployed(
		operatorConfigurationResourceName,
		"agent0-connector",
		"agent0Connector",
		agent0ConnectorDeployStatus,
		agent0connector.StatusReasonDeployed,
		agent0ConnectorEventReasons,
		string(util.ReasonAgent0ConnectorDeployed),
	)
}

// verifyNoAgent0ConnectorStatusOrEvent verifies that the operator reports nothing about the agent0-connector, which is
// what it does when the agent0-connector is disabled.
func verifyNoAgent0ConnectorStatusOrEvent(operatorConfigurationResourceName string) {
	By("verifying that the operator configuration resource has no agent0-connector status entry and no event")
	Consistently(func(g Gomega) {
		operatorConfiguration := loadOperatorConfigurationResource(g, operatorConfigurationResourceName)
		g.Expect(operatorConfiguration.Status.Agent0Connector).To(BeNil())
		g.Expect(agent0ConnectorEventReasons(g, string(operatorConfiguration.UID))).To(BeEmpty())
	}, 10*time.Second, pollingInterval).Should(Succeed())
}

func verifyAgent0ConnectorIsReportedAsDisabled(operatorConfigurationResourceName string) {
	verifyOptionalWorkloadIsReportedAsDisabled(
		operatorConfigurationResourceName,
		"agent0-connector",
		"agent0Connector",
		agent0ConnectorDeployStatus,
		agent0connector.StatusReasonDisabled,
		agent0ConnectorEventReasons,
		string(util.ReasonAgent0ConnectorDisabled),
	)
}

// updateOperatorConfigurationAgent0ConnectorEnabled sets spec.agent0Connector.enabled on the given operator
// configuration resource, which is how a user opts out of the agent0-connector and back in again.
func updateOperatorConfigurationAgent0ConnectorEnabled(operatorConfigurationResourceName string, enabled bool) {
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"patch",
			"Dash0OperatorConfiguration",
			operatorConfigurationResourceName,
			"--type",
			"merge",
			"-p",
			fmt.Sprintf(`{"spec":{"agent0Connector":{"enabled":%t}}}`, enabled),
		))).To(Succeed())
}

// agent0ConnectorEventReasons returns the reasons of the agent0-connector events the operator has written for the
// operator configuration resource with the given UID. The events are matched by UID rather than by name, so that
// events left behind by an earlier test which used an equally named resource are ignored. The events are attached to
// the cluster-scoped operator configuration resource, hence they are not confined to a single namespace.
func agent0ConnectorEventReasons(g Gomega, operatorConfigurationResourceUid string) []string {
	output, err := run(exec.Command(
		"kubectl",
		"get",
		"events.events.k8s.io",
		"--all-namespaces",
		"-o",
		"jsonpath={range .items[*]}{.regarding.uid}{\" \"}{.reason}{\"\\n\"}{end}",
	), false)
	g.Expect(err).NotTo(HaveOccurred())

	agent0ConnectorReasons := []string{
		string(util.ReasonAgent0ConnectorDeployed),
		string(util.ReasonAgent0ConnectorNotDeployed),
		string(util.ReasonAgent0ConnectorDisabled),
	}
	var reasons []string
	for _, line := range strings.Split(output, "\n") {
		uid, reason, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || uid != operatorConfigurationResourceUid {
			continue
		}
		if slices.Contains(agent0ConnectorReasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func syntheticsWorkerDeployStatus(
	operatorConfiguration dash0v1alpha1.Dash0OperatorConfiguration,
) *workloadDeployStatus {
	s := operatorConfiguration.Status.SyntheticsWorker
	if s == nil {
		return nil
	}
	return &workloadDeployStatus{deployed: s.Deployed, reason: s.Reason, message: s.Message}
}

func verifySyntheticsWorkerIsReportedAsDeployed(operatorConfigurationResourceName string) {
	verifyOptionalWorkloadIsReportedAsDeployed(
		operatorConfigurationResourceName,
		"synthetics-worker",
		"syntheticsWorker",
		syntheticsWorkerDeployStatus,
		syntheticsworker.StatusReasonDeployed,
		syntheticsWorkerEventReasons,
		string(util.ReasonSyntheticsWorkerDeployed),
	)
}

func verifySyntheticsWorkerIsReportedAsDisabled(operatorConfigurationResourceName string) {
	verifyOptionalWorkloadIsReportedAsDisabled(
		operatorConfigurationResourceName,
		"synthetics-worker",
		"syntheticsWorker",
		syntheticsWorkerDeployStatus,
		syntheticsworker.StatusReasonDisabled,
		syntheticsWorkerEventReasons,
		string(util.ReasonSyntheticsWorkerDisabled),
	)
}

// updateOperatorConfigurationSyntheticsWorkerEnabled sets spec.syntheticsWorker.enabled on the given operator
// configuration resource, which is how a user opts out of the synthetics-worker and back in again.
func updateOperatorConfigurationSyntheticsWorkerEnabled(operatorConfigurationResourceName string, enabled bool) {
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"patch",
			"Dash0OperatorConfiguration",
			operatorConfigurationResourceName,
			"--type",
			"merge",
			"-p",
			fmt.Sprintf(`{"spec":{"syntheticsWorker":{"enabled":%t}}}`, enabled),
		))).To(Succeed())
}

// configureSyntheticsWorkerLocationAndToken sets spec.syntheticsWorker.locationId and spec.syntheticsWorker.
// authorization.token on the given operator configuration resource. Unlike the agent0-connector's server address and
// token, which are Helm-level settings, the synthetics-worker's location ID and authorization live on the CRD
// resource so that they can be changed per-cluster without a Helm re-install.
func configureSyntheticsWorkerLocationAndToken(
	operatorConfigurationResourceName string,
	locationId string,
	token string,
) {
	Expect(
		runAndIgnoreOutput(exec.Command(
			"kubectl",
			"patch",
			"Dash0OperatorConfiguration",
			operatorConfigurationResourceName,
			"--type",
			"merge",
			"-p",
			fmt.Sprintf(
				`{"spec":{"syntheticsWorker":{"locationId":%q,"authorization":{"token":%q}}}}`,
				locationId,
				token,
			),
		))).To(Succeed())
}

// syntheticsWorkerEventReasons returns the reasons of the synthetics-worker events the operator has written for the
// operator configuration resource with the given UID. The events are matched by UID rather than by name, so that
// events left behind by an earlier test which used an equally named resource are ignored. The events are attached to
// the cluster-scoped operator configuration resource, hence they are not confined to a single namespace.
func syntheticsWorkerEventReasons(g Gomega, operatorConfigurationResourceUid string) []string {
	output, err := run(exec.Command(
		"kubectl",
		"get",
		"events.events.k8s.io",
		"--all-namespaces",
		"-o",
		"jsonpath={range .items[*]}{.regarding.uid}{\" \"}{.reason}{\"\\n\"}{end}",
	), false)
	g.Expect(err).NotTo(HaveOccurred())

	syntheticsWorkerReasons := []string{
		string(util.ReasonSyntheticsWorkerDeployed),
		string(util.ReasonSyntheticsWorkerNotDeployed),
		string(util.ReasonSyntheticsWorkerDisabled),
	}
	var reasons []string
	for _, line := range strings.Split(output, "\n") {
		uid, reason, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || uid != operatorConfigurationResourceUid {
			continue
		}
		if slices.Contains(syntheticsWorkerReasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}
