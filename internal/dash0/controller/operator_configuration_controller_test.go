// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	common "github.com/dash0hq/dash0-operator/api/dash0monitoring"
	. "github.com/dash0hq/dash0-operator/test/util"
)

const (
	controllerDeploymentName = "controller-deployment"
)

var (
	reconciler *OperatorConfigurationReconciler
)

var _ = Describe("The Dash0 controller", Ordered, func() {
	ctx := context.Background()
	var createdObjects []client.Object
	var controllerDeployment *appsv1.Deployment

	BeforeAll(func() {
		EnsureTestNamespaceExists(ctx, k8sClient)
		EnsureDash0OperatorNamespaceExists(ctx, k8sClient)
	})

	BeforeEach(func() {
		createdObjects = make([]client.Object, 0)
	})

	Describe("when creating the Dash0Operator resource", func() {

		BeforeEach(func() {
			// When creating the resource, we assume the operator has no
			// self-monitoring enabled
			controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
			EnsureControllerDeploymentExists(ctx, k8sClient, controllerDeployment)
			reconciler = &OperatorConfigurationReconciler{
				Client:                  k8sClient,
				Clientset:               clientset,
				Recorder:                recorder,
				DeploymentSelfReference: controllerDeployment,
				DanglingEventsTimeouts: &DanglingEventsTimeouts{
					InitialTimeout: 0 * time.Second,
					Backoff: wait.Backoff{
						Steps:    1,
						Duration: 0 * time.Second,
						Factor:   1,
						Jitter:   0,
					},
				},
			}
		})

		AfterEach(func() {
			RemoveOperatorConfigurationResource(ctx, k8sClient)
		})

		Describe("enabling self-monitoring", func() {

			It("it enables self-monitoring in the controller deployment", func() {
				CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
					AuthorizationToken: "1234567890",
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: true,
					},
				})

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
				verifyOperatorConfigurationResourceIsAvailable(ctx)
				Eventually(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, timeout, pollingInterval).Should(Succeed())
			})

		})

		Describe("disabling self-monitoring", func() {

			It("it does not change the controller deployment", func() {
				CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
					AuthorizationToken: "1234567890",
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: false,
					},
				})

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
				verifyOperatorConfigurationResourceIsAvailable(ctx)
				Consistently(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, timeout, pollingInterval).Should(Succeed())
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
					reconciler = &OperatorConfigurationReconciler{
						Client:                  k8sClient,
						Clientset:               clientset,
						Recorder:                recorder,
						DeploymentSelfReference: controllerDeployment,
						DanglingEventsTimeouts: &DanglingEventsTimeouts{
							InitialTimeout: 0 * time.Second,
							Backoff: wait.Backoff{
								Steps:    1,
								Duration: 0 * time.Second,
								Factor:   1,
								Jitter:   0,
							},
						},
					}
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
				})

				It("it does not change the controller deployment", func() {
					CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
						AuthorizationToken: "1234567890",
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: true,
						},
					})

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
					verifyOperatorConfigurationResourceIsAvailable(ctx)

					Consistently(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeTrue())
					}, timeout, pollingInterval).Should(Succeed())
				})

			})

			Describe("when self-monitoring is disabled", func() {

				BeforeEach(func() {
					controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
					reconciler = &OperatorConfigurationReconciler{
						Client:                  k8sClient,
						Clientset:               clientset,
						Recorder:                recorder,
						DeploymentSelfReference: controllerDeployment,
						DanglingEventsTimeouts: &DanglingEventsTimeouts{
							InitialTimeout: 0 * time.Second,
							Backoff: wait.Backoff{
								Steps:    1,
								Duration: 0 * time.Second,
								Factor:   1,
								Jitter:   0,
							},
						},
					}

					CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
						AuthorizationToken: "1234567890",
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: false,
						},
					})
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
				})

				It("it enables self-monitoring in the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

					resource.Spec.SelfMonitoring.Enabled = true

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeTrue())
					}, timeout, pollingInterval).Should(Succeed())
				})

			})

		})

		Describe("disabling self-monitoring", func() {

			Describe("when self-monitoring is enabled", func() {

				BeforeEach(func() {
					CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
						AuthorizationToken: "1234567890",
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: true,
						},
					})

					controllerDeployment = controllerDeploymentWithSelfMonitoring()
					reconciler = &OperatorConfigurationReconciler{
						Client:                  k8sClient,
						Clientset:               clientset,
						Recorder:                recorder,
						DeploymentSelfReference: controllerDeployment,
						DanglingEventsTimeouts: &DanglingEventsTimeouts{
							InitialTimeout: 0 * time.Second,
							Backoff: wait.Backoff{
								Steps:    1,
								Duration: 0 * time.Second,
								Factor:   1,
								Jitter:   0,
							},
						},
					}
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
				})

				It("it disables self-monitoring in the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(resource.Spec.SelfMonitoring.Enabled).To(BeTrue())

					resource.Spec.SelfMonitoring.Enabled = false

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Eventually(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
					}, timeout, pollingInterval).Should(Succeed())
				})

			})

			Describe("when self-monitoring is already disabled", func() {

				BeforeEach(func() {
					CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
						Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
						AuthorizationToken: "1234567890",
						SelfMonitoring: dash0v1alpha1.SelfMonitoring{
							Enabled: false,
						},
					})

					controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
					reconciler = &OperatorConfigurationReconciler{
						Client:                  k8sClient,
						Clientset:               clientset,
						Recorder:                recorder,
						DeploymentSelfReference: controllerDeployment,
						DanglingEventsTimeouts: &DanglingEventsTimeouts{
							InitialTimeout: 0 * time.Second,
							Backoff: wait.Backoff{
								Steps:    1,
								Duration: 0 * time.Second,
								Factor:   1,
								Jitter:   0,
							},
						},
					}
				})

				AfterEach(func() {
					RemoveOperatorConfigurationResource(ctx, k8sClient)
				})

				It("it does not change the controller deployment", func() {
					resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
					Expect(resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

					resource.Spec.SelfMonitoring.Enabled = false

					Expect(k8sClient.Update(ctx, resource)).To(Succeed())

					triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
					verifyOperatorConfigurationResourceIsAvailable(ctx)
					Consistently(func(g Gomega) {
						updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
						selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
						Expect(err).NotTo(HaveOccurred())
						Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
					}, timeout, pollingInterval).Should(Succeed())
				})

			})

		})

	})

	Describe("when deleting the Dash0Operator resource", func() {

		Describe("when self-monitoring is enabled", func() {

			BeforeEach(func() {
				CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
					AuthorizationToken: "1234567890",
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: true,
					},
				})

				controllerDeployment = controllerDeploymentWithSelfMonitoring()
				reconciler = &OperatorConfigurationReconciler{
					Client:                  k8sClient,
					Clientset:               clientset,
					Recorder:                recorder,
					DeploymentSelfReference: controllerDeployment,
					DanglingEventsTimeouts: &DanglingEventsTimeouts{
						InitialTimeout: 0 * time.Second,
						Backoff: wait.Backoff{
							Steps:    1,
							Duration: 0 * time.Second,
							Factor:   1,
							Jitter:   0,
						},
					},
				}
			})

			It("it disables self-monitoring in the controller deployment", func() {
				Expect(selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(controllerDeployment, ManagerContainerName)).To(BeTrue())

				resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				Expect(resource.Spec.SelfMonitoring.Enabled).To(BeTrue())

				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
				VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

				Eventually(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, timeout, pollingInterval).Should(Succeed())
			})

		})

		Describe("when self-monitoring is disabled", func() {

			BeforeEach(func() {
				CreateOperatorConfigurationResource(ctx, k8sClient, OperatorConfigurationResourceName, dash0v1alpha1.Dash0OperatorConfigurationSpec{
					Endpoint:           "ingress.eu-west-1.aws.dash0monitoring-dev.com:4317",
					AuthorizationToken: "1234567890",
					SelfMonitoring: dash0v1alpha1.SelfMonitoring{
						Enabled: false,
					},
				})

				controllerDeployment = controllerDeploymentWithoutSelfMonitoring()
				reconciler = &OperatorConfigurationReconciler{
					Client:                  k8sClient,
					Clientset:               clientset,
					Recorder:                recorder,
					DeploymentSelfReference: controllerDeployment,
					DanglingEventsTimeouts: &DanglingEventsTimeouts{
						InitialTimeout: 0 * time.Second,
						Backoff: wait.Backoff{
							Steps:    1,
							Duration: 0 * time.Second,
							Factor:   1,
							Jitter:   0,
						},
					},
				}
			})

			It("it does not change the controller deployment", func() {
				Expect(selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(controllerDeployment, ManagerContainerName)).To(BeFalse())

				resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, Default)
				Expect(resource.Spec.SelfMonitoring.Enabled).To(BeFalse())

				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				triggerOperatorConfigurationReconcileRequest(ctx, reconciler, "")
				VerifyOperatorConfigurationResourceByNameDoesNotExist(ctx, k8sClient, Default, resource.Name)

				Consistently(func(g Gomega) {
					updatedDeployment := LoadOperatorDeploymentOrFail(ctx, k8sClient, g)
					selfMonitoringConfiguration, err := selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(updatedDeployment, ManagerContainerName)
					Expect(err).NotTo(HaveOccurred())
					Expect(selfMonitoringConfiguration.Enabled).To(BeFalse())
				}, timeout, pollingInterval).Should(Succeed())
			})

		})

	})

	AfterEach(func() {
		createdObjects = DeleteAllCreatedObjects(ctx, k8sClient, createdObjects)
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
									Name:  "ENABLE_WEBHOOK",
									Value: "false",
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
									Name:  "ENABLE_WEBHOOK",
									Value: "false",
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

func triggerOperatorConfigurationReconcileRequest(ctx context.Context, reconciler *OperatorConfigurationReconciler, stepMessage string) {
	triggerOperatorReconcileRequestForName(ctx, reconciler, stepMessage, OperatorConfigurationResourceName)
}

func triggerOperatorReconcileRequestForName(
	ctx context.Context,
	reconciler *OperatorConfigurationReconciler,
	stepMessage string,
	dash0OperatorResourceName string,
) {
	if stepMessage == "" {
		stepMessage = "Trigger reconcile request"
	}
	By(stepMessage)
	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: dash0OperatorResourceName},
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyOperatorConfigurationResourceIsAvailable(ctx context.Context) *metav1.Condition {
	var availableCondition *metav1.Condition
	By("Verifying status conditions")
	Eventually(func(g Gomega) {
		resource := LoadOperatorConfigurationResourceOrFail(ctx, k8sClient, g)
		availableCondition = meta.FindStatusCondition(resource.Status.Conditions, string(common.ConditionTypeAvailable))
		g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
		degraded := meta.FindStatusCondition(resource.Status.Conditions, string(common.ConditionTypeDegraded))
		g.Expect(degraded).To(BeNil())
	}, timeout, pollingInterval).Should(Succeed())
	return availableCondition
}
