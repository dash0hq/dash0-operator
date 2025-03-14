// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

const (
	SecretRefResolverDeploymentName = "dash0-operator-secret-ref-resolver"

	secretRefResolverTokenEnvVarName = "SELF_MONITORING_AND_API_AUTH_TOKEN"
)

func CreateSecretRefResolverDeploymentWithoutSecretRefEnvVar() *appsv1.Deployment {
	return createSecretRefResolverDeployment(createSecretRefResolverDefaultEnvVars())
}

func CreateSecretRefResolverDeploymentWithSecretRefEnvVar() *appsv1.Deployment {
	env := append([]corev1.EnvVar{createSecretRefEnvVar()}, createSecretRefResolverDefaultEnvVars()...)
	return createSecretRefResolverDeployment(env)
}

func createSecretRefEnvVar() corev1.EnvVar {
	return corev1.EnvVar{
		Name: secretRefResolverTokenEnvVarName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: SecretRefTest.Name,
				},
				Key: SecretRefTest.Key,
			},
		},
	}
}

func createSecretRefResolverDefaultEnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "TOKEN_UPDATE_SERVICE_SERVER_NAME",
			Value: "dash0-operator-token-update.operator-namespace.svc",
		},
		{
			Name:  "TOKEN_UPDATE_SERVICE_URL",
			Value: "https://dash0-operator-token-update.operator-namespace.svc:10443",
		},
	}
}

func createSecretRefResolverDeployment(env []corev1.EnvVar) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dash0-operator-secret-ref-resolver",
			Namespace: OperatorNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dash0-operator",
				"app.kubernetes.io/component": "secret-ref-resolver",
				"app.kubernetes.io/instance":  "deployment",
				"dash0.com/enable":            "false",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "dash0-operator",
					"app.kubernetes.io/component": "secret-ref-resolver",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubectl.kubernetes.io/default-container": "secret-ref-resolver",
					},
					Labels: map[string]string{
						"app.kubernetes.io/name":      "dash0-operator",
						"app.kubernetes.io/component": "secret-ref-resolver",
						"dash0.cert-digest":           "1234567890",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "secret-ref-resolver",
							Image: "ghcr.io/dash0hq/secret-ref-resolver@latest",
							Env:   env,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "certificates",
									MountPath: "/tmp/k8s-webhook-server/serving-certs",
									ReadOnly:  true,
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					TerminationGracePeriodSeconds: ptr.To(int64(10)),
					Volumes: []corev1.Volume{
						{
							Name: "certificates",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									DefaultMode: ptr.To(corev1.SecretVolumeSourceDefaultMode),
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

func EnsureSecretRefResolverDeploymentExists(
	ctx context.Context,
	k8sClient client.Client,
	secretRefResolverDeployment *appsv1.Deployment,
) *appsv1.Deployment {
	deployment := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		types.NamespacedName{Namespace: secretRefResolverDeployment.Namespace, Name: secretRefResolverDeployment.Name},
		&appsv1.Deployment{},
		secretRefResolverDeployment,
	)
	return deployment.(*appsv1.Deployment)
}

func EnsureSecretRefResolverDeploymentDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	secretRefResolverDeployment *appsv1.Deployment,
) {
	Expect(k8sClient.Delete(ctx, secretRefResolverDeployment)).To(Succeed())
}

func LoadSecretRefResolverDeploymentOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	if err := k8sClient.Get(
		ctx,
		types.NamespacedName{Namespace: OperatorNamespace, Name: SecretRefResolverDeploymentName},
		deployment,
	); err != nil {
		g.Expect(err).NotTo(HaveOccurred())
		return nil
	}

	return deployment
}
