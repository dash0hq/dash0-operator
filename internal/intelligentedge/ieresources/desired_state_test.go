// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package ieresources

import (
	corev1 "k8s.io/api/core/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	testOperatorVersion = "v1.2.3"
)

var (
	minimalIntelligentEdge = &dash0v1alpha1.Dash0IntelligentEdge{
		Spec: dash0v1alpha1.Dash0IntelligentEdgeSpec{},
	}

	operatorConfigWithDash0Export = &dash0v1alpha1.Dash0OperatorConfiguration{
		Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
			Export: &dash0common.Export{
				Dash0: &dash0common.Dash0Configuration{
					Endpoint: "ingress.dash0.com:4317",
					Authorization: dash0common.Authorization{
						Token: ptr("auth-token"),
					},
				},
			},
		},
	}
)

var _ = Describe("Barker deployment self-monitoring env vars", func() {
	It("injects OTel exporter env vars when self-monitoring is enabled (default, nil)", func() {
		opConfig := operatorConfigWithDash0Export.DeepCopy()
		opConfig.Spec.SelfMonitoring.Enabled = nil

		dep := assembleBarkerDeployment(
			OperatorNamespace, "test-prefix", minimalIntelligentEdge, opConfig,
			"barker:latest", corev1.PullIfNotPresent, testOperatorVersion, logd.Discard(),
		)

		container := dep.Spec.Template.Spec.Containers[0]
		expectSelfMonitoringEnvVarsPresent(container, testOperatorVersion)
	})

	It("injects OTel exporter env vars when self-monitoring is explicitly enabled", func() {
		opConfig := operatorConfigWithDash0Export.DeepCopy()
		opConfig.Spec.SelfMonitoring.Enabled = ptr(true)

		dep := assembleBarkerDeployment(
			OperatorNamespace, "test-prefix", minimalIntelligentEdge, opConfig,
			"barker:latest", corev1.PullIfNotPresent, testOperatorVersion, logd.Discard(),
		)

		container := dep.Spec.Template.Spec.Containers[0]
		expectSelfMonitoringEnvVarsPresent(container, testOperatorVersion)
	})

	It("does not inject OTel exporter env vars when self-monitoring is explicitly disabled", func() {
		opConfig := operatorConfigWithDash0Export.DeepCopy()
		opConfig.Spec.SelfMonitoring.Enabled = ptr(false)

		dep := assembleBarkerDeployment(
			OperatorNamespace, "test-prefix", minimalIntelligentEdge, opConfig,
			"barker:latest", corev1.PullIfNotPresent, testOperatorVersion, logd.Discard(),
		)

		container := dep.Spec.Template.Spec.Containers[0]
		expectSelfMonitoringEnvVarsAbsent(container)
	})

	It("does not inject OTel exporter env vars when operator config is nil", func() {
		dep := assembleBarkerDeployment(
			OperatorNamespace, "test-prefix", minimalIntelligentEdge, nil,
			"barker:latest", corev1.PullIfNotPresent, testOperatorVersion, logd.Discard(),
		)

		container := dep.Spec.Template.Spec.Containers[0]
		expectSelfMonitoringEnvVarsAbsent(container)
	})
})

func expectSelfMonitoringEnvVarsPresent(container corev1.Container, operatorVersion string) {
	envByName := map[string]corev1.EnvVar{}
	for _, e := range container.Env {
		envByName[e.Name] = e
	}

	Expect(envByName).To(HaveKey("DASH0_NODE_IP"))
	Expect(envByName["DASH0_NODE_IP"].ValueFrom).ToNot(BeNil())
	Expect(envByName["DASH0_NODE_IP"].ValueFrom.FieldRef.FieldPath).To(Equal("status.hostIP"))

	Expect(envByName).To(HaveKey("OTEL_EXPORTER_OTLP_ENDPOINT"))
	Expect(envByName["OTEL_EXPORTER_OTLP_ENDPOINT"].Value).To(Equal("http://$(DASH0_NODE_IP):40317"))

	Expect(envByName).To(HaveKey("OTEL_EXPORTER_OTLP_PROTOCOL"))
	Expect(envByName["OTEL_EXPORTER_OTLP_PROTOCOL"].Value).To(Equal("grpc"))

	Expect(envByName).To(HaveKey("OTEL_RESOURCE_ATTRIBUTES"))
	Expect(envByName["OTEL_RESOURCE_ATTRIBUTES"].Value).To(Equal(
		"service.namespace=dash0-operator,service.name=barker,service.version=" + operatorVersion,
	))
}

func expectSelfMonitoringEnvVarsAbsent(container corev1.Container) {
	envByName := map[string]corev1.EnvVar{}
	for _, e := range container.Env {
		envByName[e.Name] = e
	}
	Expect(envByName).ToNot(HaveKey("DASH0_NODE_IP"))
	Expect(envByName).ToNot(HaveKey("OTEL_EXPORTER_OTLP_ENDPOINT"))
	Expect(envByName).ToNot(HaveKey("OTEL_EXPORTER_OTLP_PROTOCOL"))
	Expect(envByName).ToNot(HaveKey("OTEL_RESOURCE_ATTRIBUTES"))
}

func ptr[T any](v T) *T { return &v }
