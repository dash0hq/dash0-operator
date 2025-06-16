// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	OperatorWebhookServiceName        = "dash0-operator-webhook-service"
	OperatorConfigurationResourceName = "dash0-operator-configuration-test"
)

var (
	OperatorConfigurationResourceDefaultObjectMeta = metav1.ObjectMeta{
		Name: OperatorConfigurationResourceName,
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
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: ptr.To(true),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: ptr.To(true),
		},
	}

	OperatorConfigurationResourceDash0ExportWithoutApiEndpointWithSecretRef = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint: EndpointDash0Test,
				Authorization: dash0v1alpha1.Authorization{
					SecretRef: &SecretRefTest,
				},
			},
		},
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: ptr.To(true),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: ptr.To(true),
		},
	}

	OperatorConfigurationResourceDash0ExportWithApiEndpointWithToken = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint:    EndpointDash0Test,
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: ptr.To(true),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: ptr.To(true),
		},
	}

	OperatorConfigurationResourceDash0ExportWithApiEndpointWithSecretRef = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint:    EndpointDash0Test,
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					SecretRef: &SecretRefTest,
				},
			},
		},
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: ptr.To(true),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: ptr.To(true),
		},
	}

	OperatorConfigurationResourceWithoutSelfMonitoringWithToken = dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: ptr.To(false),
		},
		Export: &dash0v1alpha1.Export{
			Dash0: &dash0v1alpha1.Dash0Configuration{
				Endpoint:    EndpointDash0Test,
				ApiEndpoint: ApiEndpointTest,
				Authorization: dash0v1alpha1.Authorization{
					Token: &AuthorizationTokenTest,
				},
			},
		},
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: ptr.To(true),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: ptr.To(true),
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
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: ptr.To(true),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: ptr.To(true),
		},
	}

	OperatorConfigurationResourceDefaultSpec = OperatorConfigurationResourceWithSelfMonitoringWithToken
)

func DefaultOperatorConfigurationResource() *dash0v1alpha1.Dash0OperatorConfiguration {
	operatorConfiguration := dash0v1alpha1.Dash0OperatorConfiguration{
		ObjectMeta: OperatorConfigurationResourceDefaultObjectMeta,
		Spec:       OperatorConfigurationResourceDefaultSpec,
	}
	return &operatorConfiguration
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

func CreateOperatorConfigurationResourceWithName(
	ctx context.Context,
	k8sClient client.Client,
	resourceName string,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	operatorConfigurationResource, err := CreateOperatorConfigurationResource(
		ctx,
		k8sClient,
		&dash0v1alpha1.Dash0OperatorConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceName,
			},
			Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
				SelfMonitoring: dash0v1alpha1.SelfMonitoring{
					Enabled: ptr.To(false),
				},
				Export: &dash0v1alpha1.Export{
					Dash0: &dash0v1alpha1.Dash0Configuration{
						Endpoint: EndpointDash0Test,
						Authorization: dash0v1alpha1.Authorization{
							Token: &AuthorizationTokenTest,
						},
					},
				},
			},
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

func DefaultSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OperatorNamespace,
			Name:      "secret-ref",
		},
		Data: map[string][]byte{
			"key": []byte(AuthorizationTokenTestFromSecret),
		},
	}
}
