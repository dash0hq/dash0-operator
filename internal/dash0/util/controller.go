// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dash0common "github.com/dash0hq/dash0-operator/api/dash0monitoring"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

type CheckResourceResult struct {
	Resource             dash0common.Dash0Resource
	StopReconcile        bool
	ResourceDoesNotExist bool
}

type DanglingEventsTimeouts struct {
	InitialTimeout time.Duration
	Backoff        wait.Backoff
}

// CheckIfNamespaceExists checks if the given namespace (which is supposed to be the namespace from a reconcile request)
// exists in the cluster. If the namespace does not exist, it returns false, and this is supposed to stop the reconcile
func CheckIfNamespaceExists(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	logger *logr.Logger,
) (bool, error) {
	_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		} else {
			logger.Error(err, "Failed to fetch the current namespace, requeuing reconcile request.")
			return true, err
		}
	}
	return true, nil
}

// VerifyThatUniqueResourceExists loads the resource that the current reconcile request applies to, if it exists. It
// also checks whether there is only one such resource (or, if there are multiple, if the currently reconciled one is
// the most recently created one). The bool returned has the meaning "stop the reconcile request", that is, if the
// function returns true, it expects the caller to stop the reconcile request immediately and not requeue it.
//
// If an error occurs during any of the checks (for example while talking to the Kubernetes API server), the
// function will return that error, the caller should then ignore the bool result and requeue the reconcile request.
//
//   - If the resource does not exist, the function logs a message and returns (nil, true, nil) and expects the caller
//     to stop the reconciliation (without requeing it).
//   - If there are multiple resources in the namespace, but the given resource is the most recent one, the function
//     will return the resource together with (false, nil) as well, since the newest resource should be reconciled. The
//     caller should continue with the reconcile request in that case.
//   - If there are multiple resources and the given one is not the most recent one, the function will return true for
//     stopReconcile and the caller is expected to stop the reconcile and not requeue it.
//   - If any error is encountered when searching for resources etc., that error will be returned, the caller is
//     expected to ignore the bool result and requeue the reconcile request.
func VerifyThatUniqueResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	req ctrl.Request,
	resourcePrototype dash0common.Dash0Resource,
	updateStatusFailedMessage string,
	logger *logr.Logger,
) (CheckResourceResult, error) {
	checkResourceResult, err := VerifyThatResourceExists(
		ctx,
		k8sClient,
		req,
		resourcePrototype,
		logger,
	)
	if err != nil || checkResourceResult.StopReconcile || checkResourceResult.ResourceDoesNotExist {
		return checkResourceResult, err
	}
	checkResourceResult.StopReconcile, err =
		VerifyThatResourceIsUniqueInScope(
			ctx,
			k8sClient,
			req,
			checkResourceResult.Resource,
			updateStatusFailedMessage,
			logger,
		)
	return checkResourceResult, err
}

// VerifyThatResourceExists loads the resource that the current reconcile request applies to. If that resource does not
// exist, the function logs a message and returns (nil, true, nil) and expects the caller to stop the reconciliation
// (without requeing it). If any other error occurs while trying to fetch the resource, the function logs the error and
// returns (nil, true, err) and expects the caller to requeue the reconciliation.
func VerifyThatResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	req ctrl.Request,
	resourcePrototype dash0common.Dash0Resource,
	logger *logr.Logger,
) (CheckResourceResult, error) {
	resource := resourcePrototype.GetReceiver()
	if err := k8sClient.Get(ctx, req.NamespacedName, resource); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(
				fmt.Sprintf(
					"The %s %s has not been found, either it hasn't been installed or it has been deleted.",
					resourcePrototype.GetNaturalLanguageResourceTypeName(),
					resourcePrototype.RequestToName(req),
				))
			// stop the reconciliation, and do not requeue it.
			return CheckResourceResult{nil, true, true}, err
		}
		logger.Error(err,
			fmt.Sprintf(
				"Failed to get the %s %s, requeuing reconcile request.",
				resourcePrototype.GetNaturalLanguageResourceTypeName(),
				resourcePrototype.RequestToName(req),
			))
		// requeue the reconciliation
		return CheckResourceResult{nil, true, false}, err
	}

	// We have found a resource and return it.
	return CheckResourceResult{resource.(dash0common.Dash0Resource), false, false}, nil
}

// VerifyThatResourceIsUniqueInScope checks whether there are any additional resources of the same type
// in the namespace, besides the one that the current reconcile request applies to. The bool the function returns has
// the semantic stopReconcile, that is, if the function returns true, it expects the caller to stop the reconcile. If
// there are no errors and the resource is unique, the function will return (false, nil). If there are multiple
// resources in the namespace, but the given resource is the most recent one, the function will return (false, nil) as
// well, since the newest resource should be reconciled. If there are multiple resources and the given one is not the
// most recent one, the function will return (true, nil), and the caller is expected to stop the reconcile and not
// requeue it.
// If any error is encountered when searching for other resource etc., that error will be returned, the caller is
// expected to ignore the bool result and requeue the reconcile request.
func VerifyThatResourceIsUniqueInScope(
	ctx context.Context,
	k8sClient client.Client,
	req ctrl.Request,
	resource dash0common.Dash0Resource,
	updateStatusFailedMessage string,
	logger *logr.Logger,
) (bool, error) {
	scope, allResourcesInScope, err :=
		findAllResourceInstancesInScope(ctx, k8sClient, req, resource, logger)
	if err != nil {
		return true, err
	}

	items := resource.Items(allResourcesInScope)
	if len(items) <= 1 {
		// The given resource is unique.
		return false, nil
	}

	// There are multiple instances of the resource in scope (that is, in the same namespace for namespaced
	// resources, or in the same cluster for cluster-scoped resources). If the resource that is currently being
	// reconciled  is the one that has been most recently created, we assume that this is the source of truth in terms
	// of configuration settings etc., and we ignore the other instances in this reconcile request (they will be
	// handled when they are being reconciled). If the currently reconciled resource is not the most recent one, we
	// set its status to degraded.
	sort.Sort(SortByCreationTimestamp(items))
	mostRecentResource := resource.At(allResourcesInScope, len(items)-1)
	if mostRecentResource.GetUid() == resource.GetUid() {
		logger.Info(fmt.Sprintf(
			"At least one other %[1]s exists in this %[2]s. This %[1]s resource (%[3]s) is the most recent one."+
				" The state of the other resource(s) will be set to degraded.",
			resource.GetNaturalLanguageResourceTypeName(),
			scope,
			resource.RequestToName(req),
		))
		// continue with the reconcile request for this resource, let the reconcile requests for the other offending
		// resources handle the situation for those resources
		return false, nil
	} else {
		logger.Info(
			fmt.Sprintf(
				"At least one other %[1]s exists in this %[2]s, and at least one other %[1]s has been created "+
					"more recently than this one. Setting the state of this resource to degraded.",
				resource.GetNaturalLanguageResourceTypeName(),
				scope,
			),
			fmt.Sprintf("most recently created %s", resource.GetNaturalLanguageResourceTypeName()),
			fmt.Sprintf("%s (%s)", mostRecentResource.GetName(), mostRecentResource.GetUid()),
		)
		resource.EnsureResourceIsMarkedAsDegraded(
			"NewerResourceIsPresent",
			fmt.Sprintf("There is a more recently created %s in this %s, please remove all but one resource "+
				"instance.",
				resource.GetNaturalLanguageResourceTypeName(),
				scope,
			))
		if err := k8sClient.Status().Update(ctx, resource.Get()); err != nil {
			logger.Error(err, updateStatusFailedMessage)
			return true, err
		}
		// stop the reconciliation, and do not requeue it
		return true, nil
	}
}

func findAllResourceInstancesInScope(
	ctx context.Context,
	k8sClient client.Client,
	req ctrl.Request,
	resource dash0common.Dash0Resource,
	logger *logr.Logger,
) (string, client.ObjectList, error) {
	scope := "namespace"
	listOptions := client.ListOptions{
		Namespace: req.Namespace,
	}

	if resource.IsClusterResource() {
		scope = "cluster"
		listOptions = client.ListOptions{}
	}

	allResourcesInScope := resource.GetListReceiver()
	if err := k8sClient.List(
		ctx,
		allResourcesInScope,
		&listOptions,
	); err != nil {
		logger.Error(
			err,
			fmt.Sprintf(
				"Failed to list all %ss, requeuing reconcile request.",
				resource.GetNaturalLanguageResourceTypeName(),
			))
		return scope, nil, err
	}

	return scope, allResourcesInScope, nil
}

// FindUniqueOrMostRecentResourceInScope tries to fetch the unique resource of a given type in a scope (cluster or
// namespace). If multiple resources exist, it returns the most recent one. If no resources exist, it returns nil.
func FindUniqueOrMostRecentResourceInScope(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	resourcePrototype dash0common.Dash0Resource,
	logger *logr.Logger,
) (dash0common.Dash0Resource, error) {
	_, allResourcesInScope, err := findAllResourceInstancesInScope(
		ctx,
		k8sClient,
		ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: namespace,
			},
		},
		resourcePrototype,
		logger,
	)
	if err != nil {
		return nil, err
	}

	return findMostRecentResource(resourcePrototype, allResourcesInScope), nil
}

func findMostRecentResource(
	resourcePrototype dash0common.Dash0Resource,
	allResourcesInScope client.ObjectList,
) dash0common.Dash0Resource {
	items := resourcePrototype.Items(allResourcesInScope)
	if len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		return resourcePrototype.At(allResourcesInScope, 0)
	}
	sort.Sort(SortByCreationTimestamp(items))
	return resourcePrototype.At(allResourcesInScope, len(items)-1)
}

type SortByCreationTimestamp []client.Object

func (s SortByCreationTimestamp) Len() int {
	return len(s)
}
func (s SortByCreationTimestamp) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s SortByCreationTimestamp) Less(i, j int) bool {
	tsi := s[i].GetCreationTimestamp()
	tsj := s[j].GetCreationTimestamp()
	return tsi.Before(&tsj)
}

func InitStatusConditions(
	ctx context.Context,
	k8sClient client.Client,
	resource dash0common.Dash0Resource,
	conditions []metav1.Condition,
	logger *logr.Logger,
) (bool, error) {
	firstReconcile := false
	needsRefresh := false
	if len(conditions) == 0 {
		resource.SetAvailableConditionToUnknown()
		firstReconcile = true
		needsRefresh = true
	} else if availableCondition :=
		meta.FindStatusCondition(
			conditions,
			string(dash0v1alpha1.ConditionTypeAvailable),
		); availableCondition == nil {
		resource.SetAvailableConditionToUnknown()
		needsRefresh = true
	}
	if needsRefresh {
		err := updateResourceStatus(ctx, k8sClient, resource, logger)
		if err != nil {
			// The error has already been logged in refreshStatus
			return firstReconcile, err
		}
	}
	return firstReconcile, nil
}

func updateResourceStatus(
	ctx context.Context,
	k8sClient client.Client,
	resource dash0common.Dash0Resource,
	logger *logr.Logger,
) error {
	if err := k8sClient.Status().Update(ctx, resource.Get()); err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"Cannot update the status of the %s.",
				resource.GetNaturalLanguageResourceTypeName(),
			))
		return err
	}
	return nil
}

func CheckImminentDeletionAndHandleFinalizers(
	ctx context.Context,
	k8sClient client.Client,
	resource dash0common.Dash0Resource,
	finalizerId string,
	logger *logr.Logger,
) (bool, bool, error) {
	isMarkedForDeletion := resource.IsMarkedForDeletion()
	if !isMarkedForDeletion {
		err := addFinalizerIfNecessary(
			ctx,
			k8sClient,
			resource,
			finalizerId,
		)
		if err != nil {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to add finalizer to %s, requeuing reconcile request.",
					resource.GetNaturalLanguageResourceTypeName(),
				),
			)
			return isMarkedForDeletion, false, err
		}
		return isMarkedForDeletion, false, nil
	} else {
		if controllerutil.ContainsFinalizer(resource.Get(), finalizerId) {
			return isMarkedForDeletion, true, nil
		}
		return isMarkedForDeletion, false, nil
	}
}

func addFinalizerIfNecessary(
	ctx context.Context,
	k8sClient client.Client,
	resource dash0common.Dash0Resource,
	finalizerId string,
) error {
	finalizerHasBeenAdded := controllerutil.AddFinalizer(resource.Get(), finalizerId)
	if finalizerHasBeenAdded {
		return k8sClient.Update(ctx, resource.Get())
	}
	// The resource already had the finalizer, no update necessary.
	return nil
}

func CreateEnvVarForAuthorization(
	dash0ExportConfiguration dash0v1alpha1.Dash0Configuration,
	envVarName string,
) (corev1.EnvVar, error) {
	token := dash0ExportConfiguration.Authorization.Token
	secretRef := dash0ExportConfiguration.Authorization.SecretRef
	if token != nil && *token != "" {
		return corev1.EnvVar{
			Name:  envVarName,
			Value: *token,
		}, nil
	} else if secretRef != nil && secretRef.Name != "" && secretRef.Key != "" {
		return corev1.EnvVar{
			Name: envVarName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretRef.Name,
					},
					Key: secretRef.Key,
				},
			},
		}, nil
	} else {
		return corev1.EnvVar{}, fmt.Errorf("neither token nor secretRef provided for the Dash0 exporter")
	}
}
