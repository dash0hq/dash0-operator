// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	namespace  = "some-namespace"
	namePrefix = OTelCollectorNamePrefixTest

	numberOfResourcesWithKubernetesInfrastructureMetricsCollectionEnabled    = 14
	numberOfResourcesWithoutKubernetesInfrastructureMetricsCollectionEnabled = 9
)

var _ = Describe("The desired state of the OpenTelemetry Collector resources", func() {
	It("should fail if no endpoint has been provided", func() {
		_, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export: dash0v1alpha1.Export{
				Dash0: &dash0v1alpha1.Dash0Configuration{
					Authorization: dash0v1alpha1.Authorization{
						Token: &AuthorizationTokenTest,
					},
				},
			},
			Images: TestImages,
		}, nil, &OTelExtraConfigDefaults)
		Expect(err).To(HaveOccurred())
	})

	It("should describe the desired state as a set of Kubernetes client objects", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			UseHostMetricsReceiver:                           true,
			Images:                                           TestImages,
		}, nil, &OTelExtraConfigDefaults)

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

		Expect(daemonSetPodSpec.Containers).To(HaveLen(3))
		daemonSetCollectorContainer := daemonSetPodSpec.Containers[0]
		daemonSetCollectorContainerArgs := daemonSetCollectorContainer.Args
		daemonSetConfigReloaderContainer := daemonSetPodSpec.Containers[1]
		daemonSetFileLogOffsetSynchContainer := daemonSetPodSpec.Containers[2]

		Expect(daemonSetPodSpec.Volumes).To(HaveLen(6))
		configMapVolume := findVolumeByName(daemonSetPodSpec.Volumes, "opentelemetry-collector-configmap")
		Expect(configMapVolume).NotTo(BeNil())
		Expect(configMapVolume.VolumeSource.ConfigMap.LocalObjectReference.Name).
			To(Equal(ExpectedDaemonSetCollectorConfigMapName))
		Expect(findVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())
		Expect(findVolumeMountByName(daemonSetConfigReloaderContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())

		pidFileVolume := findVolumeByName(daemonSetPodSpec.Volumes, "opentelemetry-collector-pidfile")
		Expect(pidFileVolume).NotTo(BeNil())
		Expect(pidFileVolume.VolumeSource.EmptyDir).NotTo(BeNil())
		Expect(findVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, "opentelemetry-collector-pidfile")).NotTo(BeNil())
		Expect(findVolumeMountByName(daemonSetConfigReloaderContainer.VolumeMounts, "opentelemetry-collector-pidfile")).NotTo(BeNil())
		Expect(findVolumeMountByName(daemonSetCollectorContainer.VolumeMounts, "hostfs")).NotTo(BeNil())

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

		Expect(daemonSetFileLogOffsetSynchContainer).NotTo(BeNil())
		Expect(daemonSetFileLogOffsetSynchContainer.Resources.Limits.Memory().String()).To(Equal("32Mi"))
		Expect(daemonSetFileLogOffsetSynchContainer.Resources.Requests.Memory().String()).To(Equal("32Mi"))

		Expect(daemonSetPodSpec.Tolerations).To(HaveLen(0))

		deployment := getDeployment(desiredState)
		Expect(deployment).NotTo(BeNil())
		Expect(deployment.Labels["dash0.com/enable"]).To(Equal("false"))
		deploymentPodSpec := deployment.Spec.Template.Spec

		Expect(deploymentPodSpec.Containers).To(HaveLen(2))
		deploymentCollectorContainer := deploymentPodSpec.Containers[0]
		deploymentConfigReloaderContainer := deploymentPodSpec.Containers[1]

		Expect(deploymentPodSpec.Volumes).To(HaveLen(2))
		configMapVolume = findVolumeByName(deploymentPodSpec.Volumes, "opentelemetry-collector-configmap")
		Expect(configMapVolume).NotTo(BeNil())
		Expect(configMapVolume.VolumeSource.ConfigMap.LocalObjectReference.Name).
			To(Equal(ExpectedDeploymentCollectorConfigMapName))
		Expect(findVolumeMountByName(deploymentCollectorContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())
		Expect(findVolumeMountByName(deploymentCollectorContainer.VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())

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
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: false,
			Images: TestImages,
		}, nil, &OTelExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		Expect(desiredState).To(HaveLen(numberOfResourcesWithoutKubernetesInfrastructureMetricsCollectionEnabled))

		collectorConfigConfigMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(collectorConfigConfigMapContent).To(ContainSubstring(fmt.Sprintf("endpoint: %s", EndpointDash0TestQuoted)))
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
		Expect(findVolumeMountByName(findContainerByName(podSpec.Containers, "opentelemetry-collector").VolumeMounts, "hostfs")).To(BeNil())
		collectorContainer := podSpec.Containers[0]
		Expect(collectorContainer).NotTo(BeNil())
		Expect(collectorContainer.VolumeMounts).To(HaveLen(5))
		Expect(collectorContainer.VolumeMounts).ToNot(ContainElement(MatchVolumeMount("hostfs", "/hostfs")))
	})

	It("should use the authorization token directly if provided", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
		}, nil, &OTelExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))

		daemonSet := getDaemonSet(desiredState)

		authTokenEnvVar := findEnvVarByName(daemonSet.Spec.Template.Spec.Containers[0].Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).NotTo(BeNil())
		Expect(authTokenEnvVar.Value).To(Equal(AuthorizationTokenTest))
	})

	It("should use the secret reference if provided (and no authorization token has been provided)", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndSecretRef(),
		}, nil, &OTelExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		container := podSpec.Containers[0]
		authTokenEnvVar := findEnvVarByName(container.Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).NotTo(BeNil())
		Expect(authTokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal(SecretRefTest.Name))
		Expect(authTokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal(SecretRefTest.Key))
	})

	It("should not add the auth token env var if no Dash0 exporter is used", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     HttpExportTest(),
		}, nil, &OTelExtraConfigDefaults)

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getDaemonSetCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).NotTo(ContainSubstring("\"Authorization\": \"Bearer ${env:AUTH_TOKEN}\""))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		container := podSpec.Containers[0]
		authTokenEnvVar := findEnvVarByName(container.Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).To(BeNil())
	})

	It("should correctly apply enabled self-monitoring on the daemonset", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			SelfMonitoringAndApiAccessConfiguration: selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{
				SelfMonitoringEnabled: true,
				Export:                Dash0ExportWithEndpointTokenAndInsightsDataset(),
			},
			Images: TestImages,
		}, nil, &OTelExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)
		selfMonitoringConfiguration, err := parseBackSelfMonitoringEnvVarsFromCollectorDaemonSet(daemonSet)
		Expect(err).NotTo(HaveOccurred())
		Expect(selfMonitoringConfiguration.SelfMonitoringEnabled).To(BeTrue())
		Expect(selfMonitoringConfiguration.Export.Dash0).ToNot(BeNil())
		Expect(selfMonitoringConfiguration.Export.Dash0.Endpoint).To(Equal(EndpointDash0WithProtocolTest))
		Expect(selfMonitoringConfiguration.Export.Dash0.Dataset).To(Equal(util.DatasetInsights))
		Expect(*selfMonitoringConfiguration.Export.Dash0.Authorization.Token).To(Equal(AuthorizationTokenTest))
		Expect(selfMonitoringConfiguration.Export.Grpc).To(BeNil())
		Expect(selfMonitoringConfiguration.Export.Http).To(BeNil())
	})

	It("should correctly apply disabled self-monitoring on the daemonset", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			SelfMonitoringAndApiAccessConfiguration: selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{
				SelfMonitoringEnabled: false,
				Export:                Dash0ExportWithEndpointTokenAndInsightsDataset(),
			},
			Images: TestImages,
		}, nil, &OTelExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())

		daemonSet := getDaemonSet(desiredState)
		selfMonitoringConfiguration, err := parseBackSelfMonitoringEnvVarsFromCollectorDaemonSet(daemonSet)
		Expect(err).NotTo(HaveOccurred())
		Expect(selfMonitoringConfiguration.SelfMonitoringEnabled).To(BeFalse())
		Expect(selfMonitoringConfiguration.Export.Dash0).To(BeNil())
		Expect(selfMonitoringConfiguration.Export.Grpc).To(BeNil())
		Expect(selfMonitoringConfiguration.Export.Http).To(BeNil())
	})

	It("should render custom tolerations", func() {
		desiredState, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			KubernetesInfrastructureMetricsCollectionEnabled: true,
			UseHostMetricsReceiver:                           true,
			Images:                                           TestImages,
		}, nil, &OTelColExtraConfig{
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

	It("rendered objects must be stable", func() {
		mr1 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-1",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScrapingEnabled: ptr.To(true),
			},
		}
		mr2 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-2",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScrapingEnabled: ptr.To(true),
			},
		}
		mr3 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-3",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScrapingEnabled: ptr.To(false),
			},
		}
		mr4 := dash0v1alpha1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MonitoringResourceName,
				Namespace: "namespace-4",
			},
			Spec: dash0v1alpha1.Dash0MonitoringSpec{
				PrometheusScrapingEnabled: ptr.To(false),
			},
		}

		desiredState1, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			Images:     TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr1, mr2, mr3, mr4}, &OTelExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())
		desiredState2, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			Images:     TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr3, mr4, mr1, mr2}, &OTelExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())
		desiredState3, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			Images:     TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr4, mr3, mr2, mr1}, &OTelExtraConfigDefaults)
		Expect(err).NotTo(HaveOccurred())
		desiredState4, err := assembleDesiredStateForUpsert(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			Export:     Dash0ExportWithEndpointAndToken(),
			Images:     TestImages,
		}, []dash0v1alpha1.Dash0Monitoring{mr3, mr1, mr4, mr2}, &OTelExtraConfigDefaults)
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

func getDaemonSetCollectorConfigConfigMapContent(desiredState []clientObject) string {
	cm := getConfigMap(desiredState, ExpectedDaemonSetCollectorConfigMapName)
	return cm.Data["config.yaml"]
}

func getFileOffsetConfigMapContent(desiredState []clientObject) string {
	cm := getConfigMap(desiredState, ExpectedDaemonSetFilelogOffsetSynchConfigMapName)
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

func findContainerByName(objects []corev1.Container, name string) *corev1.Container {
	for _, object := range objects {
		if object.Name == name {
			return &object
		}
	}
	return nil
}

func findEnvVarByName(objects []corev1.EnvVar, name string) *corev1.EnvVar {
	for _, object := range objects {
		if object.Name == name {
			return &object
		}
	}
	return nil
}

func findVolumeByName(objects []corev1.Volume, name string) *corev1.Volume {
	for _, object := range objects {
		if object.Name == name {
			return &object
		}
	}
	return nil
}

func findVolumeMountByName(objects []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for _, object := range objects {
		if object.Name == name {
			return &object
		}
	}
	return nil
}

// Note: There is no real need to parse the env vars on the daemonset back into a SelfMonitoringConfiguration, we could
// just read the env vars and check that they have the expected values. We might want to refactor/simplify later.
// However, this also tests the functionality used in
// selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment.
func parseBackSelfMonitoringEnvVarsFromCollectorDaemonSet(collectorDemonSet *appsv1.DaemonSet) (
	selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration,
	error,
) {
	selfMonitoringConfigurations := make(map[string]selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration)

	for _, container := range collectorDemonSet.Spec.Template.Spec.Containers {
		if selfMonitoringConfiguration, err :=
			selfmonitoringapiaccess.ParseSelfMonitoringConfigurationFromContainer(&container); err != nil {
			return selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{}, err
		} else {
			selfMonitoringConfigurations[container.Name] = selfMonitoringConfiguration
		}
	}

	// verify that the configurations on all init containers and regular containers are consistent
	var referenceMonitoringConfiguration *selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration
	for _, selfMonitoringConfiguration := range selfMonitoringConfigurations {
		if referenceMonitoringConfiguration == nil {
			referenceMonitoringConfiguration = &selfMonitoringConfiguration
		} else {
			if !reflect.DeepEqual(*referenceMonitoringConfiguration, selfMonitoringConfiguration) {
				return selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{},
					fmt.Errorf("inconsistent self-monitoring configurations: %v", selfMonitoringConfigurations)
			}
		}
	}

	if referenceMonitoringConfiguration != nil {
		return *referenceMonitoringConfiguration, nil
	} else {
		return selfmonitoringapiaccess.SelfMonitoringAndApiAccessConfiguration{}, nil
	}
}
