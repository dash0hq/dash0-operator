// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/dash0hq/dash0-operator/test/util"
)

type SelfMonitoringTestConfig struct {
	createExport func() dash0v1alpha1.Export
	verify       func(Gomega, selfmonitoring.SelfMonitoringConfiguration)
}

var (
	reconciler *OperatorConfigurationReconciler
)

var _ = Describe("The Dash0 controller", Ordered, func() {
	ctx := context.Background()
	var controllerDeployment *appsv1.Deployment

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	Describe("when creating the Dash0Operator resource", func() {

		BeforeEach(func() {
			// When creating the resource, we assume the operator has no
			// self-monitoring enabled
			controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
			EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
			reconciler = createReconciler(controllerDeployment)
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
			EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
		})

		Describe("enabling self-monitoring", func() {

			DescribeTable("it enables self-monitoring in the controller deployment",
				func(config SelfMonitoringTestConfig) {
					CreateOperatorConfigurationResource(
						ctx,
						k8sClient,
						OperatorConfigurationResourceName,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(config.createExport()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: true,
							},
						},
					)

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err :=
							selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
								updatedDeployment,
								ManagerContainerName,
							)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeTrue())
						config.verify(g, selfMonitoringConfiguration)
					}, timeout, pollingInterval).Should(Succeed())
				},
				Entry("with a Dash0 export with a token", SelfMonitoringTestConfig{
					createExport: Dash0ExportWithEndpointAndToken,
					verify:       verifySelfMonitoringConfigurationDash0Token,
				}),
				Entry("with a Dash0 export with a secret ref", SelfMonitoringTestConfig{
					createExport: Dash0ExportWithEndpointAndSecretRef,
					verify:       verifySelfMonitoringConfigurationDash0SecretRef,
				}),
				Entry("with a Grpc export", SelfMonitoringTestConfig{
					createExport: GrpcExportTest,
					verify:       verifySelfMonitoringConfigurationGrpc,
				}),
				Entry("with an HTTP export", SelfMonitoringTestConfig{
					createExport: HttpExportTest,
					verify:       verifySelfMonitoringConfigurationHttp,
				}),
			)
		})

		Describe("disabling self-monitoring", func() {

			It("it does not change the controller deployment", func() {
				CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					OperatorConfigurationResourceName,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: false,
						},
					},
				)

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				verifyOperatorConfigurationResourceIsAvailable(ctx)
				Consistently(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err :=
						selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
							updatedDeployment,
							ManagerContainerName,
						)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, consistentlyTimeout, pollingInterval).Should(Succeed())
			})
		})
	})

	Describe("when updating the Dash0Operator resource", func() {

		Describe("enabling self-monitoring", func() {

			Describe("when self-monitoring is already enabled", func() {

				BeforeEach(func() {
					// When creating the resource, we assume the operator has
					// self-monitoring enabled
					controllerDeployment = controllerDeploymentWithSelfMonitoring()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it does not change the controller deployment", func() {
					CreateOperatorConfigurationResource(
						ctx,
						k8sClient,
						OperatorConfigurationResourceName,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: true,
							},
						},
					)

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)

					Consistently(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err :=
							selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
								updatedDeployment,
								ManagerContainerName,
							)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeTrue())
					}, consistentlyTimeout, pollingInterval).Should(Succeed())
				})
			})

			Describe("when self-monitoring is disabled", func() {

				BeforeEach(func() {
					controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)

					CreateOperatorConfigurationResource(
						ctx,
						k8sClient,
						OperatorConfigurationResourceName,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: false,
							},
						},
					)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it enables self-monitoring in the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

					resource.Spec.SelfMonitoring.Enabled = true

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err :=
							selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
								updatedDeployment,
								ManagerContainerName,
							)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeTrue())
					}, timeout, pollingInterval).Should(Succeed())
				})
			})
		})

		Describe("disabling self-monitoring", func() {

			Describe("when self-monitoring is enabled", func() {

				BeforeEach(func() {
					CreateOperatorConfigurationResource(
						ctx,
						k8sClient,
						OperatorConfigurationResourceName,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: true,
							},
						},
					)

					controllerDeployment = controllerDeploymentWithSelfMonitoring()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it disables self-monitoring in the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(resource.Spec.SelfMonitoring.Enabled).To(BeTrue())

					resource.Spec.SelfMonitoring.Enabled = false

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err :=
							selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
								updatedDeployment,
								ManagerContainerName,
							)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
					}, timeout, pollingInterval).Should(Succeed())
				})
			})

			Describe("when self-monitoring is already disabled", func() {

				BeforeEach(func() {
					CreateOperatorConfigurationResource(
						ctx,
						k8sClient,
						OperatorConfigurationResourceName,
						dash0v1alpha1.Dash0OperatorConfigurationSpec{
							Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
							SelfMonitoring: dash0v1alpha1.SelfMonitoring{
								Enabled: false,
							},
						},
					)

					controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
					EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
					reconciler = createReconciler(controllerDeployment)
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
					EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
				})

				It("it does not change the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

					resource.Spec.SelfMonitoring.Enabled = false

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Consistently(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err :=
							selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
								updatedDeployment,
								ManagerContainerName,
							)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
					}, consistentlyTimeout, pollingInterval).Should(Succeed())
				})
			})
		})
	})

	Describe("when deleting the Dash0Operator resource", func() {

		Describe("when self-monitoring is enabled", func() {

			BeforeEach(func() {
				CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					OperatorConfigurationResourceName,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Export: ExportToPrt(Dash0ExportWithEndpointAndToken()),
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: true,
						},
					},
				)

				controllerDeployment = controllerDeploymentWithSelfMonitoring()
				EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
				reconciler = createReconciler(controllerDeployment)
			})

			AfterEach(func() {
				RemoveOperatorConfigurationResource(ctx, k8sClient)
				EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
			})

			It("it disables self-monitoring in the controller deployment", func() {
				selfMonitoringConfiguration, err :=
					selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
						controllerDeployment,
						ManagerContainerName,
					)
				Expect(err).NotTo(HaveOccurred())
				Expect(selfMonitoringConfiguration.Enabled).To(BeTrue())

				resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

				Eventually(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err :=
						selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
							updatedDeployment,
							ManagerContainerName,
						)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, timeout, pollingInterval).Should(Succeed())
			})
		})

		Describe("when self-monitoring is disabled", func() {

			BeforeEach(func() {
				CreateOperatorConfigurationResource(
					ctx,
					k8sClient,
					OperatorConfigurationResourceName,
					dash0v1alpha1.Dash0OperatorConfigurationSpec{
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: false,
						},
					},
				)

				controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
				EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
				reconciler = createReconciler(controllerDeployment)
			})

			AfterEach(func() {
				RemoveOperatorConfigurationResource(ctx, k8sClient)
				EnsureControllerDeploymentDoesNotExist(ctx, k8sClient, controllerDeployment)
			})

			It("it does not change the controller deployment", func() {
				selfMonitoringConfiguration, err :=
					selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
						controllerDeployment,
						ManagerContainerName,
					)
				Expect(err).NotTo(HaveOccurred())
				Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())

				resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				Expect(resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler)
				VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

				Consistently(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err :=
						selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
							updatedDeployment,
							ManagerContainerName,
						)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, consistentlyTimeout, pollingInterval).Should(Succeed())
			})
		})
	})
})

func controllerDeploymentWithoutSelfMonitoring() *appsv1.Deployment {
	replicaCount := int32(2)
	falsy := false
	truthy := true
	terminationGracePeriodSeconds := int64(10)
	secretMode := corev1.SecretVolumeSourceDefaultMode

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Dash0OperatorDeploymentName,
			Namespace: Dash0OperatorNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dash0monitoring-operator",
				"app.kubernetes.io/component": "controller",
				"app.kubernetes.io/instance":  "deployment",
				"dash0.com/enable":            "false",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "dash0monitoring-operator",
					"app.kubernetes.io/component": "controller",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubectl.kubernetes.io/default-container": "manager",
					},
					Labels: map[string]string{
						"app.kubernetes.io/name":      "dash0monitoring-operator",
						"app.kubernetes.io/component": "controller",
						"dash0.cert-digest":           "1234567890",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "manager",
							Image:   "ghcr.io/dash0hq/operator-controller@latest",
							Command: []string{"/manager"},
							Args: []string{
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=127.0.0.1:8080",
								"--leader-elect",
							},
							Env: []corev1.EnvVar{
								{
									Name: "DASH0_OPERATOR_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name:  "DASH0_DEPLOYMENT_NAME",
									Value: Dash0OperatorDeploymentName,
								},
								{
									Name:  "OTEL_COLLECTOR_NAME_PREFIX",
									Value: "dash0monitoring-system",
								},
								{
									Name:  "DASH0_INIT_CONTAINER_IMAGE",
									Value: "ghcr.io/dash0hq/instrumentation",
								},
								{
									Name:  "DASH0_INIT_CONTAINER_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_COLLECTOR_IMAGE",
									Value: "ghcr.io/dash0hq/collector",
								},
								{
									Name:  "DASH0_COLLECTOR_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_CONFIGURATION_RELOADER_IMAGE",
									Value: "ghcr.io/dash0hq/configuration-reloader@latest",
								},
								{
									Name:  "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_FILELOG_OFFSET_SYNCH_IMAGE",
									Value: "ghcr.io/dash0hq/filelog-offset-synch",
								},
								{
									Name:  "DASH0_FILELOG_OFFSET_SYNCH_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_DEVELOPMENT_MODE",
									Value: "false",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "webhook-server",
									ContainerPort: 9443,
									Protocol:      "TCP",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "certificates",
									MountPath: "/tmp/k8s-webhook-server/serving-certs",
									ReadOnly:  true,
								},
							},
						},
						{
							Name:  "kube-rbac-proxy",
							Image: "quay.io/brancz/kube-rbac-proxy:v0.18.0",
							Args: []string{
								"--secure-listen-address=0.0.0.0:8443",
								"--upstream=http://127.0.0.1:8080/",
								"--logtostderr=true",
								"--v=0",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 8443,
									Protocol:      "TCP",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &falsy,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &truthy,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					ServiceAccountName:            "dash0-operator-service-account",
					AutomountServiceAccountToken:  &truthy,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					Volumes: []corev1.Volume{
						{
							Name: "certificates",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									DefaultMode: &secretMode,
									SecretName:  "dash0-operator-certificates",
								},
							},
						},
					},
				},
			},
		},
	}
}

func controllerDeploymentWithSelfMonitoring() *appsv1.Deployment {
	replicaCount := int32(2)
	falsy := false
	truthy := true
	terminationGracePeriodSeconds := int64(10)
	secretMode := corev1.SecretVolumeSourceDefaultMode

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Dash0OperatorDeploymentName,
			Namespace: Dash0OperatorNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dash0monitoring-operator",
				"app.kubernetes.io/component": "controller",
				"app.kubernetes.io/instance":  "deployment",
				"dash0monitoring.com/enable":  "false",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "dash0monitoring-operator",
					"app.kubernetes.io/component": "controller",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubectl.kubernetes.io/default-container": "manager",
					},
					Labels: map[string]string{
						"app.kubernetes.io/name":      "dash0monitoring-operator",
						"app.kubernetes.io/component": "controller",
						"dash0monitoring.cert-digest": "1234567890",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "manager",
							Image:   "ghcr.io/dash0hq/operator-controller@latest",
							Command: []string{"/manager"},
							Args: []string{
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=127.0.0.1:8080",
								"--leader-elect",
							},
							Env: []corev1.EnvVar{
								{
									Name: "DASH0_OPERATOR_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name:  "DASH0_DEPLOYMENT_NAME",
									Value: Dash0OperatorNamespace,
								}, {
									Name:  "OTEL_COLLECTOR_NAME_PREFIX",
									Value: "dash0monitoring-system",
								},
								{
									Name:  "DASH0_INIT_CONTAINER_IMAGE",
									Value: "ghcr.io/dash0hq/instrumentation",
								},
								{
									Name:  "DASH0_INIT_CONTAINER_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_COLLECTOR_IMAGE",
									Value: "ghcr.io/dash0hq/collector",
								},
								{
									Name:  "DASH0_COLLECTOR_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_CONFIGURATION_RELOADER_IMAGE",
									Value: "ghcr.io/dash0hq/configuration-reloader@latest",
								},
								{
									Name:  "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_FILELOG_OFFSET_SYNCH_IMAGE",
									Value: "ghcr.io/dash0hq/filelog-offset-synch",
								},
								{
									Name:  "DASH0_FILELOG_OFFSET_SYNCH_IMAGE_PULL_POLICY",
									Value: "",
								},
								{
									Name:  "DASH0_DEVELOPMENT_MODE",
									Value: "false",
								},
								{
									Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
									Value: "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
								},
								{
									Name:  "OTEL_EXPORTER_OTLP_HEADERS",
									Value: "Authorization=Bearer 1234567890",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "webhook-server",
									ContainerPort: 9443,
									Protocol:      "TCP",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "certificates",
									MountPath: "/tmp/k8s-webhook-server/serving-certs",
									ReadOnly:  true,
								},
							},
						},
						{
							Name:  "kube-rbac-proxy",
							Image: "quay.io/brancz/kube-rbac-proxy:v0.18.0",
							Args: []string{
								"--secure-listen-address=0.0.0.0:8443",
								"--upstream=http://127.0.0.1:8080/",
								"--logtostderr=true",
								"--v=0",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 8443,
									Protocol:      "TCP",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &falsy,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &truthy,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					ServiceAccountName:            "dash0monitoring-operator-service-account",
					AutomountServiceAccountToken:  &truthy,
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					Volumes: []corev1.Volume{
						{
							Name: "certificates",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									DefaultMode: &secretMode,
									SecretName:  "dash0monitoring-operator-certificates",
								},
							},
						},
					},
				},
			},
		},
	}
}

func createReconciler(controllerDeployment *appsv1.Deployment) *OperatorConfigurationReconciler {
	return &OperatorConfigurationReconciler{
		Client:                  k8sClient,
		Clientset:               clientset,
		Recorder:                recorder,
		DeploymentSelfReference: controllerDeployment,
		DanglingEventsTimeouts:  &DanglingEventsTimeoutsTest,
	}
}

func triggerOperatorConfigurationReconcileRequest(ctx context.Context, reconciler *OperatorConfigurationReconciler) {
	triggerOperatorReconcileRequestForName(ctx, reconciler, OperatorConfigurationResourceName)
}

func triggerOperatorReconcileRequestForName(
	ctx context.Context,
	reconciler *OperatorConfigurationReconciler,
	dash0OperatorResourceName string,
) {
	By("Triggering an operator configuration resource reconcile request")
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: dash0OperatorResourceName},
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyOperatorConfigurationResourceIsAvailable(ctx context.Context) {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(resource.Status.Conditions, string(dash0v1alpha1.ConditionTypeAvailable))
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(resource.Status.Conditions, string(dash0v1alpha1.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
}

func verifySelfMonitoringConfigurationDash0Token(
	g Gomega,
	selfMonitoringConfiguration selfmonitoring.SelfMonitoringConfiguration,
) {
	dash0ExportConfiguration := selfMonitoringConfiguration.Export.Dash0
	g.Expect(dash0ExportConfiguration).NotTo(BeNil())
	g.Expect(dash0ExportConfiguration.Endpoint).To(Equal(EndpointDash0WithProtocolTest))
	g.Expect(dash0ExportConfiguration.Dataset).To(Equal(util.DatasetInsights))
	authorization := dash0ExportConfiguration.Authorization
	g.Expect(authorization).ToNot(BeNil())
	g.Expect(*authorization.Token).To(Equal(AuthorizationTokenTest))
	g.Expect(authorization.SecretRef).To(BeNil())
	g.Expect(selfMonitoringConfiguration.Export.Grpc).To(BeNil())
	g.Expect(selfMonitoringConfiguration.Export.Http).To(BeNil())
}

func verifySelfMonitoringConfigurationDash0SecretRef(
	g Gomega,
	selfMonitoringConfiguration selfmonitoring.SelfMonitoringConfiguration,
) {
	dash0ExportConfiguration := selfMonitoringConfiguration.Export.Dash0
	g.Expect(dash0ExportConfiguration).NotTo(BeNil())
	g.Expect(dash0ExportConfiguration.Endpoint).To(Equal(EndpointDash0WithProtocolTest))
	g.Expect(dash0ExportConfiguration.Dataset).To(Equal(util.DatasetInsights))
	authorization := dash0ExportConfiguration.Authorization
	g.Expect(authorization.Token).To(BeNil())
	g.Expect(authorization.SecretRef).ToNot(BeNil())
	g.Expect(authorization.SecretRef.Name).To(Equal(SecretRefTest.Name))
	g.Expect(authorization.SecretRef.Key).To(Equal(SecretRefTest.Key))
	g.Expect(selfMonitoringConfiguration.Export.Grpc).To(BeNil())
	g.Expect(selfMonitoringConfiguration.Export.Http).To(BeNil())
}

func verifySelfMonitoringConfigurationGrpc(
	g Gomega,
	selfMonitoringConfiguration selfmonitoring.SelfMonitoringConfiguration,
) {
	grpcExportConfiguration := selfMonitoringConfiguration.Export.Grpc
	g.Expect(grpcExportConfiguration).NotTo(BeNil())
	g.Expect(grpcExportConfiguration.Endpoint).To(Equal("dns://" + EndpointGrpcTest))
	headers := grpcExportConfiguration.Headers
	g.Expect(headers).To(HaveLen(2))
	g.Expect(headers[0].Name).To(Equal("Key"))
	g.Expect(headers[0].Value).To(Equal("Value"))
	g.Expect(headers[1].Name).To(Equal(util.Dash0DatasetHeaderName))
	g.Expect(headers[1].Value).To(Equal(util.DatasetInsights))
	g.Expect(selfMonitoringConfiguration.Export.Dash0).To(BeNil())
	g.Expect(selfMonitoringConfiguration.Export.Http).To(BeNil())
}

func verifySelfMonitoringConfigurationHttp(
	g Gomega,
	selfMonitoringConfiguration selfmonitoring.SelfMonitoringConfiguration,
) {
	httpExportConfiguration := selfMonitoringConfiguration.Export.Http
	g.Expect(httpExportConfiguration).NotTo(BeNil())
	g.Expect(httpExportConfiguration.Endpoint).To(Equal(EndpointHttpTest))
	g.Expect(httpExportConfiguration.Encoding).To(Equal(dash0v1alpha1.Proto))
	headers := httpExportConfiguration.Headers
	g.Expect(headers).To(HaveLen(2))
	g.Expect(headers[0].Name).To(Equal("Key"))
	g.Expect(headers[0].Value).To(Equal("Value"))
	g.Expect(headers[1].Name).To(Equal(util.Dash0DatasetHeaderName))
	g.Expect(headers[1].Value).To(Equal(util.DatasetInsights))
	g.Expect(selfMonitoringConfiguration.Export.Dash0).To(BeNil())
	g.Expect(selfMonitoringConfiguration.Export.Grpc).To(BeNil())
}
