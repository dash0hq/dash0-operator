// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultNamespace       = "namespace"
	DeploymentName         = "deployment"
	DeploymentNameExisting = "existing-deployment"
)

func CreateDeployment(namespace string, name string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	deployment.Namespace = namespace
	deployment.Name = name
	deployment.Spec = appsv1.DeploymentSpec{}
	deployment.Spec.Template = corev1.PodTemplateSpec{}
	deployment.Spec.Template.Labels = map[string]string{"app": "test"}
	deployment.Spec.Template.Spec.Containers = []corev1.Container{{
		Name:  "test-container",
		Image: "ubuntu",
	}}
	deployment.Spec.Selector = &metav1.LabelSelector{}
	deployment.Spec.Selector.MatchLabels = map[string]string{"app": "test"}
	return deployment
}

func GetDeployment(ctx context.Context, k8sClient client.Client, namespace string, deploymentName string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      deploymentName,
	}
	ExpectWithOffset(1, k8sClient.Get(ctx, namespacedName, deployment)).Should(Succeed())
	return deployment
}
