// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type collectorSelfMonitoringExpectations struct {
	endpoint       string
	protocol       string
	authTokenValue string
	secretRefName  string
	secretRefKey   string
	otlpHeaders    map[string]string
	exportIsDash0  bool
}

const (
	numberOfResourcesWithKubernetesInfrastructureMetricsCollectionEnabled    = 14
	numberOfResourcesWithoutKubernetesInfrastructureMetricsCollectionEnabled = 9

	otelExporterOtlpEndpointEnvVarName = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterOtlpHeadersEnvVarName  = "OTEL_EXPORTER_OTLP_HEADERS"
	otelExporterOtlpProtocolEnvVarName = "OTEL_EXPORTER_OTLP_PROTOCOL"
)

var _ = Describe("The desired state of the OpenTelemetry Collector resources", func() {
	It("should fail if no endpoint has been provided", func() {
		_, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).To(HaveOccurred())
	})

	It("should describe the desired state as a set of Kubernetes client objects", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			UseHostMetricsReceiver:                           true,
			Images:                                           TestImages,
		}, nil, util.ExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		Expect(desiredState).To(HaveLen(numberOfResourcesWithKubernetesInfrastructureMetricsCollectionEnabled))

		for _, wrapper := range desiredState {
			object := wrapper.object
			annotations := object.GetAnnotations()
			Expect(annotations).To(HaveLen(2))
			Expect(annotations["argocd.argoproj.io/sync-options"]).To(Equal("Prune=false"))
			Expect(annotations["argocd.argoproj.io/compare-options"]).To(Equal("IgnoreExtraneous"))
		}
		collectorConfigConfigMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(collectorConfigConfigMapContent).To(ContainSubstring(fmt.Sprintf("endpoint: %s", EndpointDash0TestQuoted)))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/traces"))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/metrics"))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/logs"))

		fileOffsetConfigMapContent := getFileOffsetConfigMapContent(desiredState)
		Expect(fileOffsetConfigMapContent).NotTo(BeNil())

		daemonSet := getDaemonSet(desiredState)
		Expect(daemonSet).NotTo(BeNil())
		Expect(daemonSet.ObjectMeta.Labels["dash0.com/enable"]).To(Equal("false"))
		daemonSetPodSpec := daemonSet.Spec.Template.Spec

		Expect(daemonSetPodSpec.InitContainers).To(HaveLen(1))
		Expect(daemonSetPodSpec.InitContainers[0].Name).To(Equal("filelog-offset-init"))
		Expect(daemonSetPodSpec.Containers).To(HaveLen(3))
		daemonSetCollectorContainer := daemonSetPodSpec.Containers[0]
		Expect(daemonSetCollectorContainer.Name).To(Equal("opentelemetry-collector"))
		daemonSetCollectorContainerArgs := daemonSetCollectorContainer.Args
		daemonSetConfigReloaderContainer := daemonSetPodSpec.Containers[1]
		Expect(daemonSetConfigReloaderContainer.Name).To(Equal("configuration-reloader"))
		daemonSetFileLogOffsetSyncContainer := daemonSetPodSpec.Containers[2]
		Expect(daemonSetFileLogOffsetSyncContainer.Name).To(Equal("filelog-offset-sync"))

		Expect(daemonSetPodSpec.Volumes).To(HaveLen(6))
		configMapVolume := FindVolumeByName(daemonSetPodSpec.Volumes, "opentelemetry-collector-configmap")
		Expect(configMapVolume).NotTo(BeNil())
		Expect(configMapVolume.VolumeSource.ConfigMap.LocalObjectReference.Name).
			To(Equal(ExpectedDaemonSetCollectorConfigMapName))
		Expect(FindVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())
		Expect(FindVolumeMountByName(daemonSetConfigReloaderContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())

		pidFileVolume := FindVolumeByName(daemonSetPodSpec.Volumes, "opentelemetry-collector-pidfile")
		Expect(pidFileVolume).NotTo(BeNil())
		Expect(pidFileVolume.VolumeSource.EmptyDir).NotTo(BeNil())
		Expect(FindVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, "opentelemetry-collector-pidfile")).NotTo(BeNil())
		Expect(FindVolumeMountByName(daemonSetConfigReloaderContainer.VolumeMounts, "opentelemetry-collector-pidfile")).NotTo(BeNil())
		Expect(FindVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, "hostfs")).NotTo(BeNil())

		Expect(daemonSetCollectorContainer).NotTo(BeNil())
		Expect(daemonSetCollectorContainer.Image).To(Equal(CollectorImageTest))
		Expect(daemonSetCollectorContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(daemonSetCollectorContainer.Resources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(daemonSetCollectorContainer.Resources.Requests.Memory().String()).To(Equal("500Mi"))
		Expect(daemonSetCollectorContainerArgs).To(HaveLen(1))
		Expect(daemonSetCollectorContainerArgs[0]).To(Equal("--config=file:/etc/otelcol/conf/config.yaml"))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(HaveLen(6))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/etc/otelcol/conf")))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-pidfile", "/etc/otelcol/run")))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("node-pod-logs", "/var/log/pods")))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("node-docker-container-logs", "/var/lib/docker/containers")))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("filelogreceiver-offsets", "/var/otelcol/filelogreceiver_offsets")))
		Expect(daemonSetCollectorContainer.VolumeMounts).To(ContainElement(MatchVolumeMount("hostfs", "/hostfs")))

		Expect(daemonSetConfigReloaderContainer).NotTo(BeNil())
		Expect(daemonSetConfigReloaderContainer.Image).To(Equal(ConfigurationReloaderImageTest))
		Expect(daemonSetConfigReloaderContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(daemonSetConfigReloaderContainer.Resources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(daemonSetConfigReloaderContainer.Resources.Requests.Memory().String()).To(Equal("12Mi"))
		configReloaderContainerArgs := daemonSetConfigReloaderContainer.Args
		Expect(configReloaderContainerArgs).To(HaveLen(2))
		Expect(configReloaderContainerArgs[0]).To(Equal("--pidfile=/etc/otelcol/run/pid.file"))
		Expect(configReloaderContainerArgs[1]).To(Equal("/etc/otelcol/conf/config.yaml"))
		Expect(daemonSetConfigReloaderContainer.VolumeMounts).To(HaveLen(2))
		Expect(daemonSetConfigReloaderContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/etc/otelcol/conf")))
		Expect(daemonSetConfigReloaderContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-pidfile", "/etc/otelcol/run")))

		Expect(daemonSetFileLogOffsetSyncContainer).NotTo(BeNil())
		Expect(daemonSetFileLogOffsetSyncContainer.Resources.Limits.Memory().String()).To(Equal("32Mi"))
		Expect(daemonSetFileLogOffsetSyncContainer.Resources.Requests.Memory().String()).To(Equal("32Mi"))

		Expect(daemonSetPodSpec.Tolerations).To(HaveLen(0))

		deployment := getDeployment(desiredState)
		Expect(deployment).NotTo(BeNil())
		Expect(deployment.Labels["dash0.com/enable"]).To(Equal("false"))
		deploymentPodSpec := deployment.Spec.Template.Spec

		Expect(deploymentPodSpec.Containers).To(HaveLen(2))
		deploymentCollectorContainer := deploymentPodSpec.Containers[0]
		deploymentConfigReloaderContainer := deploymentPodSpec.Containers[1]

		Expect(deploymentPodSpec.Volumes).To(HaveLen(2))
		configMapVolume = FindVolumeByName(deploymentPodSpec.Volumes, "opentelemetry-collector-configmap")
		Expect(configMapVolume).NotTo(BeNil())
		Expect(configMapVolume.VolumeSource.ConfigMap.LocalObjectReference.Name).
			To(Equal(ExpectedDeploymentCollectorConfigMapName))
		Expect(FindVolumeMountByName(deploymentCollectorContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())
		Expect(FindVolumeMountByName(deploymentCollectorContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())

		Expect(deploymentCollectorContainer).NotTo(BeNil())
		Expect(deploymentCollectorContainer.Image).To(Equal(CollectorImageTest))
		Expect(deploymentCollectorContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(deploymentCollectorContainer.Resources.Limits.Memory().String()).To(Equal("500Mi"))
		Expect(deploymentCollectorContainer.Resources.Requests.Memory().String()).To(Equal("500Mi"))
		deploymentCollectorContainerArgs := deploymentCollectorContainer.Args
		Expect(deploymentCollectorContainerArgs).To(HaveLen(1))
		Expect(deploymentCollectorContainerArgs[0]).To(Equal("--config=file:/etc/otelcol/conf/config.yaml"))
		Expect(deploymentCollectorContainer.VolumeMounts).To(HaveLen(2))
		Expect(deploymentCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/etc/otelcol/conf")))
		Expect(deploymentCollectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-pidfile", "/etc/otelcol/run")))

		Expect(deploymentConfigReloaderContainer).NotTo(BeNil())
		Expect(deploymentConfigReloaderContainer.Image).To(Equal(ConfigurationReloaderImageTest))
		Expect(deploymentConfigReloaderContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
		Expect(deploymentConfigReloaderContainer.Resources.Limits.Memory().String()).To(Equal("12Mi"))
		Expect(deploymentConfigReloaderContainer.Resources.Requests.Memory().String()).To(Equal("12Mi"))
		deploymentConfigReloaderContainerArgs := deploymentConfigReloaderContainer.Args
		Expect(deploymentConfigReloaderContainerArgs).To(HaveLen(2))
		Expect(deploymentConfigReloaderContainerArgs[0]).To(Equal("--pidfile=/etc/otelcol/run/pid.file"))
		Expect(deploymentConfigReloaderContainerArgs[1]).To(Equal("/etc/otelcol/conf/config.yaml"))
		Expect(deploymentConfigReloaderContainer.VolumeMounts).To(HaveLen(2))
		Expect(deploymentConfigReloaderContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/etc/otelcol/conf")))
		Expect(deploymentConfigReloaderContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-pidfile", "/etc/otelcol/run")))

		Expect(findObjectByName(desiredState, ExpectedDeploymentServiceAccountName)).ToNot(BeNil())
		Expect(findObjectByName(desiredState, ExpectedDeploymentClusterRoleName)).ToNot(BeNil())
		Expect(findObjectByName(desiredState, ExpectedDeploymentClusterRoleBindingName)).ToNot(BeNil())
		Expect(findObjectByName(desiredState, ExpectedDeploymentCollectorConfigMapName)).ToNot(BeNil())
	})

	It("should omit all resources related to the collector deployment if collecting cluster metrics is disabled", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: false,
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		Expect(desiredState).To(HaveLen(numberOfResourcesWithoutKubernetesInfrastructureMetricsCollectionEnabled))

		collectorConfigConfigMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(collectorConfigConfigMapContent).To(
			ContainSubstring(fmt.Sprintf("endpoint: %s", EndpointDash0TestQuoted)))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/traces"))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/metrics"))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/logs"))

		fileOffsetConfigMapContent := getFileOffsetConfigMapContent(desiredState)
		Expect(fileOffsetConfigMapContent).NotTo(BeNil())

		Expect(findObjectByName(desiredState, ExpectedDeploymentServiceAccountName)).To(BeNil())
		Expect(findObjectByName(desiredState, ExpectedDeploymentClusterRoleName)).To(BeNil())
		Expect(findObjectByName(desiredState, ExpectedDeploymentClusterRoleBindingName)).To(BeNil())
		Expect(findObjectByName(desiredState, ExpectedDeploymentCollectorConfigMapName)).To(BeNil())
		Expect(getDeployment(desiredState)).To(BeNil())

		daemonSet := getDaemonSet(desiredState)
		Expect(daemonSet).NotTo(BeNil())
		podSpec := daemonSet.Spec.Template.Spec
		Expect(podSpec.Volumes).To(HaveLen(5))
		Expect(FindVolumeMountByName(
			FindContainerByName(podSpec.Containers, "opentelemetry-collector").VolumeMounts, "hostfs")).To(BeNil())
		collectorContainer := podSpec.Containers[0]
		Expect(collectorContainer).NotTo(BeNil())
		Expect(collectorContainer.VolumeMounts).To(HaveLen(5))
		Expect(collectorContainer.VolumeMounts).ToNot(ContainElement(MatchVolumeMount("hostfs", "/hostfs")))
	})

	It("should use the authorization token directly if provided", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
		}, nil, util.ExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))

		daemonSet := getDaemonSet(desiredState)

		authTokenEnvVar := FindEnvVarByName(daemonSet.Spec.Template.Spec.Containers[0].Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).NotTo(BeNil())
		Expect(authTokenEnvVar.Value).To(Equal(AuthorizationTokenTest))
	})

	It("should use the secret reference if provided (and no authorization token has been provided)", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndSecretRef(),
		}, nil, util.ExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		container := podSpec.Containers[0]
		authTokenEnvVar := FindEnvVarByName(container.Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).NotTo(BeNil())
		Expect(authTokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal(SecretRefTest.Name))
		Expect(authTokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal(SecretRefTest.Key))
	})

	It("should not add the auth token env var if no Dash0 exporter is used", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *HttpExportTest(),
		}, nil, util.ExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).NotTo(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		container := podSpec.Containers[0]
		authTokenEnvVar := FindEnvVarByName(container.Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).To(BeNil())
	})

	It("should correctly apply Dash0 export self-monitoring with token on the daemonset", func() {
		export := Dash0ExportWithEndpointAndToken()
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *export,
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)

		verifySelfMonitoringSettings(
			daemonSet,
			collectorSelfMonitoringExpectations{
				exportIsDash0:  true,
				endpoint:       EndpointDash0WithProtocolTest,
				protocol:       common.ProtocolGrpc,
				authTokenValue: AuthorizationTokenTest,
				otlpHeaders: map[string]string{
					util.AuthorizationHeaderName: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
				},
			},
		)
	})

	It("should correctly apply Dash0 export self-monitoring with a secret ref on the daemonset", func() {
		export := Dash0ExportWithEndpointAndSecretRef()
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *export,
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)

		verifySelfMonitoringSettings(
			daemonSet,
			collectorSelfMonitoringExpectations{
				exportIsDash0: true,
				endpoint:      EndpointDash0WithProtocolTest,
				protocol:      common.ProtocolGrpc,
				secretRefName: SecretRefTest.Name,
				secretRefKey:  SecretRefTest.Key,
				otlpHeaders: map[string]string{
					util.AuthorizationHeaderName: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
				},
			},
		)
	})

	It("should correctly apply Dash0 export self-monitoring with a custom dataset on the daemonset", func() {
		export := Dash0ExportWithEndpointTokenAndCustomDataset()
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *export,
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)

		verifySelfMonitoringSettings(
			daemonSet,
			collectorSelfMonitoringExpectations{
				exportIsDash0:  true,
				endpoint:       EndpointDash0WithProtocolTest,
				protocol:       common.ProtocolGrpc,
				authTokenValue: AuthorizationTokenTest,
				otlpHeaders: map[string]string{
					util.AuthorizationHeaderName: "Bearer $(SELF_MONITORING_AUTH_TOKEN)",
					util.Dash0DatasetHeaderName:  DatasetCustomTest,
				},
			},
		)
	})

	It("should correctly apply self-monitoring with a gRCP export on the daemonset", func() {
		export := GrpcExportTest()
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *export,
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)

		verifySelfMonitoringSettings(
			daemonSet,
			collectorSelfMonitoringExpectations{
				exportIsDash0: false,
				endpoint:      "dns://" + EndpointGrpcTest,
				protocol:      common.ProtocolGrpc,
				otlpHeaders: map[string]string{
					"Key": "Value",
				},
			},
		)
	})

	It("should correctly apply self-monitoring with an HTTP/proto export on the daemonset", func() {
		export := HttpExportTest()
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: true,
				Export:                *export,
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)

		verifySelfMonitoringSettings(
			daemonSet,
			collectorSelfMonitoringExpectations{
				exportIsDash0: false,
				endpoint:      EndpointHttpTest,
				protocol:      common.ProtocolHttpProtobuf,
				otlpHeaders: map[string]string{
					"Key": "Value",
				},
			},
		)
	})

	It("should correctly apply disabled self-monitoring settings to the daemonset", func() {
		export := Dash0ExportWithEndpointAndToken()
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *export,
			SelfMonitoringConfiguration: selfmonitoringapiaccess.SelfMonitoringConfiguration{
				SelfMonitoringEnabled: false,
			},
			Images: TestImages,
		}, nil, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)

		verifyAbsentSelfMonitoringSettings(daemonSet)
	})

	It("should render custom tolerations", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			UseHostMetricsReceiver:                           true,
			Images:                                           TestImages,
		}, nil, util.ExtraConfig{
			DaemonSetTolerations: []corev1.Toleration{
				{
					Key:      "key1",
					Operator: corev1.TolerationOpEqual,
					Value:    "value1",
					Effect:   corev1.TaintEffectNoSchedule,
				},
				{
					Key:      "key2",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())

		daemonSetPodSpec := getDaemonSet(desiredState).Spec.Template.Spec
		Expect(daemonSetPodSpec.Tolerations).To(HaveLen(2))
		Expect(daemonSetPodSpec.Tolerations).To(HaveLen(2))
		Expect(daemonSetPodSpec.Tolerations[0].Key).To(Equal("key1"))
		Expect(daemonSetPodSpec.Tolerations[0].Operator).To(Equal(corev1.TolerationOpEqual))
		Expect(daemonSetPodSpec.Tolerations[0].Value).To(Equal("value1"))
		Expect(daemonSetPodSpec.Tolerations[0].Effect).To(Equal(corev1.TaintEffectNoSchedule))
		Expect(daemonSetPodSpec.Tolerations[0].TolerationSeconds).To(BeNil())
		Expect(daemonSetPodSpec.Tolerations[1].Key).To(Equal("key2"))
		Expect(daemonSetPodSpec.Tolerations[1].Operator).To(Equal(corev1.TolerationOpExists))
		Expect(daemonSetPodSpec.Tolerations[1].Value).To(BeEmpty())
		Expect(daemonSetPodSpec.Tolerations[1].Effect).To(Equal(corev1.TaintEffectNoSchedule))
		Expect(daemonSetPodSpec.Tolerations[1].TolerationSeconds).To(BeNil())
	})

	It("should collect logs from namespaces, but not from the operator namespace", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			Images:            TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "namespace-1",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					LogCollection: dash0v1alpha1.LogCollection{Enabled: ptr.To(true)},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: OperatorNamespace,
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					LogCollection: dash0v1alpha1.LogCollection{Enabled: ptr.To(true)},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "namespace-2",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					LogCollection: dash0v1alpha1.LogCollection{Enabled: ptr.To(true)},
				},
			},
		}, util.ExtraConfigDefaults)
		Expect(err).ToNot(HaveOccurred())

		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("- /var/log/pods/namespace-1_*/*/*.log"))
		Expect(configMapContent).NotTo(ContainSubstring(fmt.Sprintf("- /var/log/pods/%s_*/*/*.log", OperatorNamespace)))
		Expect(configMapContent).To(ContainSubstring("- /var/log/pods/namespace-2_*/*/*.log"))
	})

	It("should scrape Prometheus metrics from namespaces when enabled", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			Images:            TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "default-to-true",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "new-setting-true",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					PrometheusScraping: dash0v1alpha1.PrometheusScraping{Enabled: ptr.To(true)},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "deprecated-setting-true",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					PrometheusScrapingEnabled: ptr.To(true),
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "new-setting-false",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					PrometheusScraping: dash0v1alpha1.PrometheusScraping{Enabled: ptr.To(false)},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MonitoringResourceName,
					Namespace: "deprecated-setting-false",
				},
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					PrometheusScrapingEnabled: ptr.To(false),
				},
			},
		}, util.ExtraConfigDefaults)
		Expect(err).ToNot(HaveOccurred())

		configMap := getDaemonSetCollectorConfigConfigMap(desiredState)
		collectorConfig := parseConfigMapContent(configMap)
		namespaces := readFromMap(collectorConfig, []string{
			"receivers",
			"prometheus",
			"config",
			"scrape_configs",
			"job_name=dash0-kubernetes-pods-scrape-config",
			"kubernetes_sd_configs",
			"role=pod",
			"namespaces",
			"names",
		})
		Expect(namespaces).To(HaveLen(3))
		Expect(namespaces).To(ContainElement("default-to-true"))
		Expect(namespaces).To(ContainElement("new-setting-true"))
		Expect(namespaces).To(ContainElement("deprecated-setting-true"))
	})

	It("should omit the filelog offset containers if a volume is provided for filelog offset storage", func() {
		offsetStorageVolume := corev1.Volume{
			Name: "offset-storage-volume",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "offset-storage-claim",
				},
			},
		}
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			UseHostMetricsReceiver:                           true,
			Images:                                           TestImages,
			OffsetStorageVolume:                              &offsetStorageVolume,
		}, nil, util.ExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		Expect(desiredState).To(HaveLen(numberOfResourcesWithKubernetesInfrastructureMetricsCollectionEnabled - 1))

		Expect(getConfigMap(desiredState, ExpectedDaemonSetFilelogOffsetSyncConfigMapName)).To(BeNil())

		daemonSet := getDaemonSet(desiredState)
		Expect(daemonSet).NotTo(BeNil())
		daemonSetPodSpec := daemonSet.Spec.Template.Spec

		Expect(daemonSetPodSpec.InitContainers).To(BeEmpty())
		Expect(daemonSetPodSpec.Containers).To(HaveLen(2))
		daemonSetCollectorContainer := daemonSetPodSpec.Containers[0]

		Expect(daemonSetPodSpec.Volumes).To(HaveLen(6))
		offsetVolumeFromDesiredState := FindVolumeByName(daemonSetPodSpec.Volumes, offsetStorageVolume.Name)
		Expect(offsetVolumeFromDesiredState).ToNot(BeNil())
		Expect(*offsetVolumeFromDesiredState).To(Equal(offsetStorageVolume))
		offsetVolumeMount := FindVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, offsetStorageVolume.Name)
		Expect(offsetVolumeFromDesiredState).NotTo(BeNil())
		Expect(offsetVolumeMount.SubPathExpr).To(Equal("$(K8S_NODE_NAME)"))
	})

	It("rendered objects must be stable", func() {
		mr1 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-1",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0v1alpha1.PrometheusScraping{
					Enabled: ptr.To(true),
				},
				LogCollection: dash0v1alpha1.LogCollection{
					Enabled: ptr.To(true),
				},
			},
		}
		mr2 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-2",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0v1alpha1.PrometheusScraping{
					Enabled: ptr.To(true),
				},
				LogCollection: dash0v1alpha1.LogCollection{
					Enabled: ptr.To(false),
				},
			},
		}
		mr3 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-3",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0v1alpha1.PrometheusScraping{
					Enabled: ptr.To(false),
				},
				LogCollection: dash0v1alpha1.LogCollection{
					Enabled: ptr.To(false),
				},
			},
		}
		mr4 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-4",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScraping: dash0v1alpha1.PrometheusScraping{
					Enabled: ptr.To(false),
				},
				LogCollection: dash0v1alpha1.LogCollection{
					Enabled: ptr.To(true),
				},
			},
		}

		desiredState1, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			Images:            TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr1, mr2, mr3, mr4}, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())
		desiredState2, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			Images:            TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr3, mr4, mr1, mr2}, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())
		desiredState3, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			Images:            TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr4, mr3, mr2, mr1}, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())
		desiredState4, err := assembleDesiredStateForUpsert(&oTelColConfig{
			OperatorNamespace: OperatorNamespace,
			NamePrefix:        namePrefix,
			Export:            *Dash0ExportWithEndpointAndToken(),
			Images:            TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr3, mr1, mr4, mr2}, util.ExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		Expect(reflect.DeepEqual(desiredState1, desiredState2)).To(BeTrue())
		Expect(reflect.DeepEqual(desiredState1, desiredState3)).To(BeTrue())
		Expect(reflect.DeepEqual(desiredState1, desiredState4)).To(BeTrue())
		Expect(reflect.DeepEqual(desiredState2, desiredState3)).To(BeTrue())
		Expect(reflect.DeepEqual(desiredState2, desiredState4)).To(BeTrue())
		Expect(reflect.DeepEqual(desiredState3, desiredState4)).To(BeTrue())
	})
})

func getConfigMap(desiredState []clientObject, name string) *corev1.ConfigMap {
	if object := findObjectByName(desiredState, name); object != nil {
		return object.(*corev1.ConfigMap)
	}
	return nil
}

func getDaemonSetCollectorConfigConfigMap(desiredState []clientObject) *corev1.ConfigMap {
	return getConfigMap(desiredState, ExpectedDaemonSetCollectorConfigMapName)
}

func getDaemonSetCollectorConfigConfigMapContent(desiredState []clientObject) string {
	return getDaemonSetCollectorConfigConfigMap(desiredState).Data["config.yaml"]
}

func getFileOffsetConfigMapContent(desiredState []clientObject) string {
	cm := getConfigMap(desiredState, ExpectedDaemonSetFilelogOffsetSyncConfigMapName)
	return cm.Data["config.yaml"]
}

func getDaemonSet(desiredState []clientObject) *appsv1.DaemonSet {
	if daemonSet := findObjectByName(desiredState, ExpectedDaemonSetName); daemonSet != nil {
		return daemonSet.(*appsv1.DaemonSet)
	}
	return nil
}

func getDeployment(desiredState []clientObject) *appsv1.Deployment {
	if deployment := findObjectByName(desiredState, ExpectedDeploymentName); deployment != nil {
		return deployment.(*appsv1.Deployment)
	}
	return nil
}

func findObjectByName(desiredState []clientObject, name string) client.Object {
	for _, object := range desiredState {
		if object.object.GetName() == name {
			return object.object
		}
	}
	return nil
}

func verifySelfMonitoringSettings(
	collectorDaemonSet *appsv1.DaemonSet,
	expectations collectorSelfMonitoringExpectations,
) {
	for _, container := range collectorDaemonSet.Spec.Template.Spec.Containers {
		verifySelfMonitoringEnvVarsForContainer(&container, expectations)
	}
}

func verifySelfMonitoringEnvVarsForContainer(
	container *corev1.Container,
	expectations collectorSelfMonitoringExpectations,
) {
	envVars := container.Env
	endpoint := parseEndpoint(envVars)
	if expectations.endpoint == "" {
		Expect(endpoint).To(BeEmpty())
	} else {
		Expect(endpoint).To(Equal(expectations.endpoint))
	}

	var protocol string
	otelExporterOtlpProtocolEnvVarIdx := slices.IndexFunc(envVars, matchOtelExporterOtlpProtocolEnvVar)
	if otelExporterOtlpProtocolEnvVarIdx >= 0 {
		protocol = envVars[otelExporterOtlpProtocolEnvVarIdx].Value
	}
	if expectations.protocol == "" {
		Expect(protocol).To(BeEmpty())
	} else {
		Expect(protocol).To(Equal(expectations.protocol))
	}

	otlpHeaders := parseHeadersFromEnvVar(envVars)
	Expect(otlpHeaders).To(HaveLen(len(expectations.otlpHeaders)))
	for key, expectedValue := range expectations.otlpHeaders {
		actualValue := otlpHeaders[key]
		Expect(actualValue).To(Equal(expectedValue))
	}

	if expectations.exportIsDash0 {
		verifyDash0SelfMonitoringEnvVars(envVars, expectations)
	}
}

func parseEndpoint(envVars []corev1.EnvVar) string {
	otelExporterOtlpEndpointEnvVarIdx := slices.IndexFunc(envVars, matchOtelExporterOtlpEndpointEnvVar)
	if otelExporterOtlpEndpointEnvVarIdx < 0 {
		return ""
	}
	otelExporterOtlpEndpointEnvVar := envVars[otelExporterOtlpEndpointEnvVarIdx]
	if otelExporterOtlpEndpointEnvVar.Value == "" && otelExporterOtlpEndpointEnvVar.ValueFrom != nil {
		Fail("retrieving the endpoint from OTEL_EXPORTER_OTLP_ENDPOINT with a ValueFrom source is not supported")
	} else if otelExporterOtlpEndpointEnvVar.Value == "" {
		Fail("there is an OTEL_EXPORTER_OTLP_ENDPOINT env var but it has no value")
	}
	return otelExporterOtlpEndpointEnvVar.Value
}

func parseHeadersFromEnvVar(envVars []corev1.EnvVar) map[string]string {
	otelExporterOtlpHeadersEnvVarValue := ""
	headers := map[string]string{}
	if otelExporterOtlpHeadersEnvVarIdx :=
		slices.IndexFunc(envVars, matchOtelExporterOtlpHeadersEnvVar); otelExporterOtlpHeadersEnvVarIdx >= 0 {
		otelExporterOtlpHeadersEnvVarValue = envVars[otelExporterOtlpHeadersEnvVarIdx].Value
		keyValuePairs := strings.Split(otelExporterOtlpHeadersEnvVarValue, ",")
		for _, keyValuePair := range keyValuePairs {
			parts := strings.Split(keyValuePair, "=")
			if len(parts) == 2 {
				headers[parts[0]] = parts[1]
			}
		}
	}

	return headers
}

func verifyDash0SelfMonitoringEnvVars(
	containerEnvVars []corev1.EnvVar,
	expectations collectorSelfMonitoringExpectations,
) {
	// we always put the auth token as the first env var since it is referenced later in the OTLP header env var
	authTokenEnvVar := containerEnvVars[0]
	Expect(authTokenEnvVar.Name).To(Equal(selfmonitoringapiaccess.CollectorSelfMonitoringAuthTokenEnvVarName))
	if expectations.authTokenValue != "" {
		Expect(authTokenEnvVar.ValueFrom).To(BeNil())
		Expect(authTokenEnvVar.Value).To(Equal(expectations.authTokenValue))
	} else if expectations.secretRefName != "" && expectations.secretRefKey != "" {
		Expect(authTokenEnvVar.Value).To(BeEmpty())
		valueFrom := authTokenEnvVar.ValueFrom
		Expect(valueFrom).NotTo(BeNil())
		secretKeyRef := valueFrom.SecretKeyRef
		Expect(secretKeyRef).NotTo(BeNil())
		Expect(secretKeyRef.LocalObjectReference.Name).To(Equal(expectations.secretRefName))
		Expect(secretKeyRef.Key).To(Equal(expectations.secretRefKey))
	} else {
		Fail("auth token expectations cannot be verified")
	}
}

func matchOtelExporterOtlpProtocolEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpProtocolEnvVarName
}

func matchOtelExporterOtlpEndpointEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpEndpointEnvVarName
}

func matchOtelExporterOtlpHeadersEnvVar(e corev1.EnvVar) bool {
	return e.Name == otelExporterOtlpHeadersEnvVarName
}

func verifyAbsentSelfMonitoringSettings(collectorDaemonSet *appsv1.DaemonSet) {
	for _, container := range collectorDaemonSet.Spec.Template.Spec.Containers {
		verifyAbsentSelfMonitoringEnvVarsForContainer(&container)
	}
}

func verifyAbsentSelfMonitoringEnvVarsForContainer(container *corev1.Container) {
	envVars := container.Env
	Expect(parseEndpoint(envVars)).To(BeEmpty())
	var protocol string
	otelExporterOtlpProtocolEnvVarIdx := slices.IndexFunc(envVars, matchOtelExporterOtlpProtocolEnvVar)
	if otelExporterOtlpProtocolEnvVarIdx >= 0 {
		protocol = envVars[otelExporterOtlpProtocolEnvVarIdx].Value
	}
	Expect(protocol).To(BeEmpty())
	Expect(parseHeadersFromEnvVar(envVars)).To(BeEmpty())
}
