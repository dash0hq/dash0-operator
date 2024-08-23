// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	namespace  = "some-namespace"
	namePrefix = "unit-test"
)

var _ = Describe("The desired state of the OpenTelemetry Collector resources", func() {
	It("should fail if no endpoint has been provided", func() {
		_, err := assembleDesiredState(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			MonitoringResource: &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					AuthorizationToken: AuthorizationTokenTest,
				},
			},
			Images: TestImages,
		})
		Expect(err).To(HaveOccurred())
	})

	It("should describe the desired state as a set of Kubernetes client objects", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:          namespace,
			NamePrefix:         namePrefix,
			MonitoringResource: MonitoringResourceWithDefaultSpec,
			Images:             TestImages,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(desiredState).To(HaveLen(9))
		collectorConfigConfigMapContent := getCollectorConfigConfigMapContent(desiredState)
		Expect(collectorConfigConfigMapContent).To(ContainSubstring(fmt.Sprintf("endpoint: %s", EndpointTest)))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/traces"))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/metrics"))
		Expect(collectorConfigConfigMapContent).NotTo(ContainSubstring("file/logs"))

		fileOffsetConfigMapContent := getFileOffsetConfigMapContent(desiredState)
		Expect(fileOffsetConfigMapContent).NotTo(BeNil())

		daemonSet := getDaemonSet(desiredState)
		Expect(daemonSet).NotTo(BeNil())
		Expect(daemonSet.ObjectMeta.Labels["dash0.com/enable"]).To(Equal("false"))
		podSpec := daemonSet.Spec.Template.Spec

		Expect(podSpec.Volumes).To(HaveLen(5))
		configMapVolume := findVolumeByName(podSpec.Volumes, "opentelemetry-collector-configmap")
		Expect(configMapVolume).NotTo(BeNil())
		Expect(configMapVolume.VolumeSource.ConfigMap.LocalObjectReference.Name).
			To(Equal("unit-test-opentelemetry-collector-agent"))
		Expect(findVolumeMountByName(findContainerByName(podSpec.Containers, "opentelemetry-collector").VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())
		Expect(findVolumeMountByName(findContainerByName(podSpec.Containers, "configuration-reloader").VolumeMounts, "opentelemetry-collector-configmap")).NotTo(BeNil())

		pidFileVolume := findVolumeByName(podSpec.Volumes, "opentelemetry-collector-pidfile")
		Expect(pidFileVolume).NotTo(BeNil())
		Expect(pidFileVolume.VolumeSource.EmptyDir).NotTo(BeNil())
		Expect(findVolumeMountByName(findContainerByName(podSpec.Containers, "opentelemetry-collector").VolumeMounts, "opentelemetry-collector-pidfile")).NotTo(BeNil())
		Expect(findVolumeMountByName(findContainerByName(podSpec.Containers, "configuration-reloader").VolumeMounts, "opentelemetry-collector-pidfile")).NotTo(BeNil())

		Expect(podSpec.Containers).To(HaveLen(3))

		collectorContainer := podSpec.Containers[0]
		Expect(collectorContainer).NotTo(BeNil())
		Expect(collectorContainer.Image).To(Equal(CollectorImageTest))
		Expect(collectorContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
		collectorContainerArgs := collectorContainer.Args
		Expect(collectorContainerArgs).To(HaveLen(1))
		Expect(collectorContainerArgs[0]).To(Equal("--config=file:/etc/otelcol/conf/config.yaml"))
		Expect(collectorContainer.VolumeMounts).To(HaveLen(5))
		Expect(collectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/etc/otelcol/conf")))
		Expect(collectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-pidfile", "/etc/otelcol/run")))
		Expect(collectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("node-pod-logs", "/var/log/pods")))
		Expect(collectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("node-docker-container-logs", "/var/lib/docker/containers")))
		Expect(collectorContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("filelogreceiver-offsets", "/var/otelcol/filelogreceiver_offsets")))

		configReloaderContainer := podSpec.Containers[1]
		Expect(configReloaderContainer).NotTo(BeNil())
		Expect(configReloaderContainer.Image).To(Equal(ConfigurationReloaderImageTest))
		Expect(configReloaderContainer.ImagePullPolicy).To(Equal(corev1.PullAlways))
		configReloaderContainerArgs := configReloaderContainer.Args
		Expect(configReloaderContainerArgs).To(HaveLen(2))
		Expect(configReloaderContainerArgs[0]).To(Equal("--pidfile=/etc/otelcol/run/pid.file"))
		Expect(configReloaderContainerArgs[1]).To(Equal("/etc/otelcol/conf/config.yaml"))
		Expect(configReloaderContainer.VolumeMounts).To(HaveLen(2))
		Expect(configReloaderContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/etc/otelcol/conf")))
		Expect(configReloaderContainer.VolumeMounts).To(
			ContainElement(MatchVolumeMount("opentelemetry-collector-pidfile", "/etc/otelcol/run")))
	})

	It("should use the authorization token directly if provided", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			MonitoringResource: &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Endpoint:           EndpointTest,
					AuthorizationToken: AuthorizationTokenTest,
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("Authorization: Bearer ${env:AUTH_TOKEN}"))

		daemonSet := getDaemonSet(desiredState)

		authTokenEnvVar := findEnvVarByName(daemonSet.Spec.Template.Spec.Containers[0].Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).NotTo(BeNil())
		Expect(authTokenEnvVar.Value).To(Equal(AuthorizationTokenTest))
	})

	It("should use the secret reference if provided (and no authorization token has been provided)", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			MonitoringResource: &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Endpoint:  EndpointTest,
					SecretRef: SecretRefTest,
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("Authorization: Bearer ${env:AUTH_TOKEN}"))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		container := podSpec.Containers[0]
		authTokenEnvVar := findEnvVarByName(container.Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).NotTo(BeNil())
		Expect(authTokenEnvVar.ValueFrom.SecretKeyRef.Name).To(Equal(SecretRefTest))
		Expect(authTokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal("dash0-authorization-token"))
	})

	It("should not add the auth token env var if no authorization token nor secret has been provided", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:  namespace,
			NamePrefix: namePrefix,
			MonitoringResource: &dash0v1alpha1.Dash0Monitoring{
				Spec: dash0v1alpha1.Dash0MonitoringSpec{
					Endpoint: EndpointTest,
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getCollectorConfigConfigMapContent(desiredState)
		Expect(configMapContent).NotTo(ContainSubstring("Authorization: Bearer ${env:AUTH_TOKEN}"))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		container := podSpec.Containers[0]
		authTokenEnvVar := findEnvVarByName(container.Env, "AUTH_TOKEN")
		Expect(authTokenEnvVar).To(BeNil())
	})
})

func getConfigMap(desiredState []client.Object, matcher func(c *corev1.ConfigMap) bool) *corev1.ConfigMap {
	for _, object := range desiredState {
		if cm, ok := object.(*corev1.ConfigMap); ok && matcher(cm) {
			return cm
		}
	}
	return nil
}

func getCollectorConfigConfigMapContent(desiredState []client.Object) string {
	cm := getConfigMap(desiredState, func(c *corev1.ConfigMap) bool {
		return strings.HasSuffix(c.Name, "-opentelemetry-collector-agent")
	})
	return cm.Data["config.yaml"]
}

func getFileOffsetConfigMapContent(desiredState []client.Object) string {
	cm := getConfigMap(desiredState, func(c *corev1.ConfigMap) bool {
		return strings.HasSuffix(c.Name, "-filelogoffsets")
	})
	return cm.Data["config.yaml"]
}

func getDaemonSet(desiredState []client.Object) *appsv1.DaemonSet {
	for _, object := range desiredState {
		if ds, ok := object.(*appsv1.DaemonSet); ok {
			return ds
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
