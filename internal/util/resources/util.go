// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"strings"

	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

// todo: merge with resources_util

func FindOperatorConfigurationResource(
	ctx context.Context,
	k8sClient client.Client,
	logger logr.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	operatorConfigurationResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		k8sClient,
		"", /* cluster-scope, thus no namespace */
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		logger,
	)
	if err != nil {
		return nil, err
	}
	if operatorConfigurationResource == nil {
		return nil, nil
	}
	return operatorConfigurationResource.(*dash0v1alpha1.Dash0OperatorConfiguration), nil
}

func FindAllMonitoringResources(
	ctx context.Context,
	k8sClient client.Client,
	logger logr.Logger,
) ([]dash0v1beta1.Dash0Monitoring, error) {
	monitoringResourceList := dash0v1beta1.Dash0MonitoringList{}
	if err := k8sClient.List(
		ctx,
		&monitoringResourceList,
		&client.ListOptions{},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 monitoring resources, requeuing reconcile request.")
		return nil, err
	}

	// filter monitoring resources that are not in state available
	monitoringResources := make([]dash0v1beta1.Dash0Monitoring, 0, len(monitoringResourceList.Items))
	for _, mr := range monitoringResourceList.Items {
		availableCondition := meta.FindStatusCondition(
			mr.Status.Conditions,
			string(dash0common.ConditionTypeAvailable),
		)
		if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
			continue
		}
		monitoringResources = append(monitoringResources, mr)
	}
	return monitoringResources, nil
}

func CreateEmptyReceiverFor(desiredResource client.Object) (client.Object, error) {
	objectKind := desiredResource.GetObjectKind()
	gvk := schema.GroupVersionKind{
		Group:   objectKind.GroupVersionKind().Group,
		Version: objectKind.GroupVersionKind().Version,
		Kind:    objectKind.GroupVersionKind().Kind,
	}
	runtimeObject, err := scheme.Scheme.New(gvk)
	if err != nil {
		return nil, err
	}
	return runtimeObject.(client.Object), nil
}

func SetOwnerReference(
	operatorManagerDeployment *appsv1.Deployment,
	scheme *runtime.Scheme,
	object client.Object,
	logger logr.Logger,
) error {
	if object.GetNamespace() == "" {
		// cluster scoped resources like ClusterRole and ClusterRoleBinding cannot have a namespace-scoped owner.
		return nil
	}
	if err := controllerutil.SetControllerReference(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: operatorManagerDeployment.Namespace,
			Name:      operatorManagerDeployment.Name,
			UID:       operatorManagerDeployment.UID,
		},
	}, object, scheme); err != nil {
		logger.Error(err, "cannot set owner reference on object")
		return err
	}
	return nil
}

func RenderName(prefix string, parts ...string) string {
	return strings.Join(append([]string{prefix}, parts...), "-")
}
