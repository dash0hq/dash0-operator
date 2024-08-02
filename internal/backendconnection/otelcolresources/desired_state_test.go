// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	namespace  = "some-namespace"
	namePrefix = "unit-test"
)

var _ = Describe("The desired state of the OpenTelemetry Collector resources", func() {
	It("should fail if no ingress endpoint has been provided", func() {
		_, err := assembleDesiredState(&oTelColConfig{
			Namespace:          namespace,
			NamePrefix:         namePrefix,
			AuthorizationToken: AuthorizationToken,
			SecretRef:          SecretRefEmpty,
			oTelColVersion:     oTelCollectorImageVersion,
		})
		Expect(err).To(HaveOccurred())
	})

	It("should fail if neither authorization token nor secret ref have been provided", func() {
		_, err := assembleDesiredState(&oTelColConfig{
			Namespace:       namespace,
			NamePrefix:      namePrefix,
			IngressEndpoint: IngressEndpoint,
			oTelColVersion:  oTelCollectorImageVersion,
		})
		Expect(err).To(HaveOccurred())
	})

	It("should describe the desired state as a set of Kubernetes client objects", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:          namespace,
			NamePrefix:         namePrefix,
			IngressEndpoint:    IngressEndpoint,
			AuthorizationToken: AuthorizationToken,
			oTelColVersion:     oTelCollectorImageVersion,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(desiredState).To(HaveLen(6))
		configMapContent := getConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring(fmt.Sprintf("endpoint: %s", IngressEndpoint)))
		Expect(configMapContent).NotTo(ContainSubstring("file/traces"))
		Expect(configMapContent).NotTo(ContainSubstring("file/metrics"))
		Expect(configMapContent).NotTo(ContainSubstring("file/logs"))

		daemonSet := getDaemonSet(desiredState)
		Expect(daemonSet).NotTo(BeNil())
		Expect(daemonSet.ObjectMeta.Labels["dash0.com/enable"]).To(Equal("false"))
		podSpec := daemonSet.Spec.Template.Spec
		configMapVolume := findVolumeByName(podSpec.Volumes, "opentelemetry-collector-configmap")
		Expect(configMapVolume).NotTo(BeNil())
		Expect(configMapVolume.VolumeSource.ConfigMap.LocalObjectReference.Name).
			To(Equal("unit-test-opentelemetry-collector-agent"))
		container := podSpec.Containers[0]
		Expect(container).NotTo(BeNil())
		containerArgs := container.Args
		Expect(containerArgs).To(HaveLen(1))
		Expect(containerArgs[0]).To(Equal("--config=file:/conf/collector.yaml"))
		Expect(container.VolumeMounts).To(ContainElement(MatchVolumeMount("opentelemetry-collector-configmap", "/conf")))
	})

	It("should use the authorization token directly if provided", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:          namespace,
			NamePrefix:         namePrefix,
			IngressEndpoint:    IngressEndpoint,
			AuthorizationToken: AuthorizationToken,
			oTelColVersion:     oTelCollectorImageVersion,
		})

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring(fmt.Sprintf("token: %s", AuthorizationToken)))
		Expect(configMapContent).NotTo(ContainSubstring("filename:"))

		daemonSet := getDaemonSet(desiredState)
		volumes := daemonSet.Spec.Template.Spec.Volumes
		secretVolume := findVolumeByName(volumes, "dash0-secret-volume")
		Expect(secretVolume).To(BeNil())
		volumeMounts := daemonSet.Spec.Template.Spec.Containers[0].VolumeMounts
		secretVolumeMount := findVolumeMountByName(volumeMounts, "dash0-secret-volume")
		Expect(secretVolumeMount).To(BeNil())
	})

	It("should use the secret reference if provided (and no authorization token has been provided)", func() {
		desiredState, err := assembleDesiredState(&oTelColConfig{
			Namespace:       namespace,
			NamePrefix:      namePrefix,
			IngressEndpoint: IngressEndpoint,
			SecretRef:       "some-secret",
			oTelColVersion:  oTelCollectorImageVersion,
		})

		Expect(err).ToNot(HaveOccurred())
		configMapContent := getConfigMapContent(desiredState)
		Expect(configMapContent).To(ContainSubstring("filename: /etc/dash0/secret-volume/dash0-authorization-token"))
		Expect(configMapContent).NotTo(ContainSubstring("token:"))

		daemonSet := getDaemonSet(desiredState)
		podSpec := daemonSet.Spec.Template.Spec
		secretVolume := findVolumeByName(podSpec.Volumes, "dash0-secret-volume")
		Expect(secretVolume).NotTo(BeNil())
		Expect(secretVolume.VolumeSource.Secret.SecretName).To(Equal("some-secret"))
		container := podSpec.Containers[0]
		volumeMounts := container.VolumeMounts
		secretVolumeMount := findVolumeMountByName(volumeMounts, "dash0-secret-volume")
		Expect(secretVolumeMount).NotTo(BeNil())
		Expect(volumeMounts).To(ContainElement(MatchVolumeMount("dash0-secret-volume", "/etc/dash0/secret-volume")))
	})
})

func getConfigMap(desiredState []client.Object) *corev1.ConfigMap {
	for _, object := range desiredState {
		if cm, ok := object.(*corev1.ConfigMap); ok {
			return cm
		}
	}
	return nil
}

func getConfigMapContent(desiredState []client.Object) string {
	cm := getConfigMap(desiredState)
	return cm.Data["collector.yaml"]
}

func getDaemonSet(desiredState []client.Object) *appsv1.DaemonSet {
	for _, object := range desiredState {
		if ds, ok := object.(*appsv1.DaemonSet); ok {
			return ds
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
