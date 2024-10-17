// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	OperatorDeploymentName            = "controller-deployment"
	OperatorConfigurationResourceName = "dash0-operator-configuration-test"
)

var (
	OperatorConfigurationResourceDefaultObjectMeta = metav1.ObjectMeta{
		Name: OperatorConfigurationResourceName,
	}

	OperatorConfigurationResourceWithoutSelfMonitoringWithoutAuth = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
	}

	OperatorConfigurationResourceWithoutExport = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
	}

	OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithToken = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}

	OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Authorization: dash0v1alpha1.Authorization{
					SecretRef: &SecretRefTest,
				},
			},
		},
	}

	OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}

	OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					SecretRef: &SecretRefTest,
				},
			},
		},
	}

	OperatorConfigurationResourceWithoutSelfMonitoringWithToken = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}

	OperatorConfigurationResourceWithoutSelfMonitoringWithSecretRef = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					SecretRef: &SecretRefTest,
				},
			},
		},
	}

	OperatorConfigurationResourceWithSelfMonitoringWithToken = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(true),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
	}

	OperatorConfigurationResourceWithSelfMonitoringWithSecretRef = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(true),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					SecretRef: &SecretRefTest,
				},
			},
		},
	}

	OperatorConfigurationResourceDefaultSpec = OperatorConfigurationResourceWithSelfMonitoringWithToken
)

func EnsureControllerDeploymentExists(
	ctx context.Context,
	k8sClient client.Client,
	controllerDeployment *appsv1.Deployment,
) *appsv1.Deployment {
	deployment := EnsureKubernetesObjectExists(
		ctx,
		k8sClient,
		types.NamespacedName{Namespace: controllerDeployment.Namespace, Name: controllerDeployment.Name},
		&appsv1.Deployment{},
		controllerDeployment,
	)
	return deployment.(*appsv1.Deployment)
}

func EnsureControllerDeploymentDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	controllerDeployment *appsv1.Deployment,
) {
	Expect(k8sClient.Delete(ctx, controllerDeployment)).To(Succeed())
}

func CreateDefaultOperatorConfigurationResource(
	ctx context.Context,
	k8sClient client.Client,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	return CreateOperatorConfigurationResourceWithSpec(
		ctx,
		k8sClient,
		OperatorConfigurationResourceDefaultSpec,
	)
}

func CreateOperatorConfigurationResourceWithSpec(
	ctx context.Context,
	k8sClient client.Client,
	operatorConfigurationSpec dash0v1alpha1.Dash0OperatorConfigurationSpec,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	operatorConfigurationResource, err := CreateOperatorConfigurationResource(
		ctx,
		k8sClient,
		&dash0v1alpha1.Dash0OperatorConfiguration{
			ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
			Spec:       operatorConfigurationSpec,
		})
	Expect(err).ToNot(HaveOccurred())
	return operatorConfigurationResource
}

func CreateOperatorConfigurationResource(
	ctx context.Context,
	k8sClient client.Client,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	err := k8sClient.Create(ctx, operatorConfigurationResource)
	return operatorConfigurationResource, err
}

func DeleteOperatorConfigurationResource(
	ctx context.Context,
	k8sClient client.Client,
) {
	Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
}

func DeleteAllOperatorConfigurationResources(
	ctx context.Context,
	k8sClient client.Client,
) {
	Expect(k8sClient.DeleteAllOf(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})).To(Succeed())
}

func LoadOperatorConfigurationResourceByNameIfItExists(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	name string,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	return LoadOperatorConfigurationResourceByName(ctx, k8sClient, g, name, false)
}

func LoadOperatorDeploymentOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	if err := k8sClient.Get(
		ctx,
		types.NamespacedName{Namespace: OperatorNamespace, Name: OperatorDeploymentName},
		deployment,
	); err != nil {
		g.Expect(err).NotTo(HaveOccurred())
		return nil
	}

	return deployment
}

func LoadOperatorConfigurationResourceOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	return LoadOperatorConfigurationResourceByNameOrFail(ctx, k8sClient, g, OperatorConfigurationResourceName)
}

func LoadOperatorConfigurationResourceByNameOrFail(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	name string,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	return LoadOperatorConfigurationResourceByName(ctx, k8sClient, g, name, true)
}

func LoadOperatorConfigurationResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	name string,
	failTestsOnNonExists bool,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	list := dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := k8sClient.List(ctx, &list, &client.ListOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			if failTestsOnNonExists {
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			} else {
				return nil
			}
		} else {
			// an error occurred, but it is not an IsNotFound error, fail test immediately
			g.Expect(err).NotTo(HaveOccurred())
			return nil
		}
	}

	var resource *dash0v1alpha1.Dash0OperatorConfiguration
	if len(list.Items) > -1 {
		resourceIdx := slices.IndexFunc(list.Items, func(r dash0v1alpha1.Dash0OperatorConfiguration) bool {
			return r.Name == name
		})

		if resourceIdx > -1 {
			resource = &list.Items[resourceIdx]
		}
	}

	if failTestsOnNonExists {
		g.Expect(resource).NotTo(BeNil())
	}

	return resource
}

func VerifyOperatorConfigurationResourceByNameDoesNotExist(
	ctx context.Context,
	k8sClient client.Client,
	g Gomega,
	name string,
) {
	g.Expect(LoadOperatorConfigurationResourceByNameIfItExists(
		ctx,
		k8sClient,
		g,
		name,
	)).To(BeNil())
}

func RemoveOperatorConfigurationResource(ctx context.Context, k8sClient client.Client) {
	RemoveOperatorConfigurationResourceByName(ctx, k8sClient, OperatorConfigurationResourceName)
}

func RemoveOperatorConfigurationResourceByName(
	ctx context.Context,
	k8sClient client.Client,
	name string,
) {
	By("Removing the operator configuration resource instance")
	if resource := LoadOperatorConfigurationResourceByNameIfItExists(
		ctx,
		k8sClient,
		Default,
		name,
	); resource != nil {
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	}
}
