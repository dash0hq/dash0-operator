// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type CustomResource interface {
	GetResourceTypeName() string
	GetNaturalLanguageResourceTypeName() string
	Get() client.Object
	GetName() string
	GetUid() types.UID
	GetCreationTimestamp() metav1.Time
	GetReceiver() client.Object
	GetListReceiver() client.ObjectList
	Items(client.ObjectList) []client.Object
	At(client.ObjectList, int) CustomResource
	SetAvailableConditionToUnknown()
	EnsureResourceIsMarkedAsDegraded(string, string)
	IsMarkedForDeletion() bool
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

// VerifyUniqueCustomResourceExists loads the resource that the current reconcile request applies to, if it exists. It
// also checks whether there is only one such resource (or, if there are multiple, if the currently reconciled one is
// the most recently created one). The bool returned has the meaning "stop the reconcile request", that is, if the
// function returns true, it expects the caller to stop the reconcile request immediately and not requeue it. If an
// error ocurrs during any of the checks (for example while talking to the Kubernetes API server), the function will
// return that error, the caller should then ignore the bool result and requeue the reconcile request.
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
func VerifyUniqueCustomResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	statusWriter client.SubResourceWriter,
	customResourceReceiver CustomResource,
	updateStatusFailedMessage string,
	req ctrl.Request,
	logger logr.Logger,
) (CustomResource, bool, error) {
	customResource, stopReconcile, err := verifyThatCustomResourceExists(
		ctx,
		k8sClient,
		req,
		customResourceReceiver,
		&logger,
	)
	if err != nil || stopReconcile {
		return nil, stopReconcile, err
	}
	stopReconcile, err =
		verifyThatCustomResourceIsUniqe(
			ctx,
			k8sClient,
			statusWriter,
			req,
			customResource,
			updateStatusFailedMessage,
			&logger,
		)
	return customResource, stopReconcile, err
}

// verifyThatCustomResourceExists loads the resource that the current reconcile request applies to. If that
// resource does not exist, the function logs a message and returns (nil, true, nil) and expects the caller to stop the
// reconciliation (without requeing it). If any other error occurs while trying to fetch the resource, the function logs
// the error and returns (nil, true, err) and expects the caller to requeue the reconciliation.
func verifyThatCustomResourceExists(
	ctx context.Context,
	k8sClient client.Client,
	req ctrl.Request,
	customResourceReceiver CustomResource,
	logger *logr.Logger,
) (CustomResource, bool, error) {
	resource := customResourceReceiver.GetReceiver()
	err := k8sClient.Get(ctx, req.NamespacedName, resource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(
				fmt.Sprintf(
					"The %s has not been found, either it hasn't been installed or it has been deleted. Ignoring the reconcile request.",
					customResourceReceiver.GetNaturalLanguageResourceTypeName()))
			// stop the reconciliation, and do not requeue it (that is, return (ctrl.Result{}, nil))
			return nil, true, nil
		}
		logger.Error(err, "Failed to get the Dash0 custom resource, requeuing reconcile request.")
		// requeue the reconciliation (that is, return (ctrl.Result{}, err))
		return nil, true, err
	}
	return resource.(CustomResource), false, nil
}

// verifyThatCustomResourceIsUniqe checks whether there are any additional resources of the same type in the namespace,
// besides the one that the current reconcile request applies to. The bool the function returns has the semantic
// stopReconcile, that is, if the function returns true, it expects the caller to stop the reconcile. If there are no
// errors and the resource is unique, the function will return (false, nil). If there are multiple resources in the
// namespace, but the given resource is the most recent one, the function will return (false, nil) as well, since the
// newest resource should be reconciled. If there are multiple resources and the given one is not the most recent one,
// the function will return (true, nil), and the caller is expected to stop the reconcile and not requeue it.
// If any error is encountered when searching for other resource etc., that error will be returned, the caller is
// expected to ignore the bool result and requeue the reconcile request.
func verifyThatCustomResourceIsUniqe(
	ctx context.Context,
	k8sClient client.Client,
	statusWriter client.SubResourceWriter,
	req ctrl.Request,
	customResource CustomResource,
	updateStatusFailedMessage string,
	logger *logr.Logger,
) (bool, error) {
	allCustomResourcesInNamespace := customResource.GetListReceiver()
	if err := k8sClient.List(
		ctx,
		allCustomResourcesInNamespace,
		&client.ListOptions{
			Namespace: req.Namespace,
		},
	); err != nil {
		logger.Error(
			err,
			fmt.Sprintf(
				"Failed to list all %ss, requeuing reconcile request.",
				customResource.GetNaturalLanguageResourceTypeName(),
			))
		return true, err
	}

	items := customResource.Items(allCustomResourcesInNamespace)
	if len(items) > 1 {
		// There are multiple instances of the Dash0 custom resource in this namespace. If the resource that is
		// currently being reconciled is the one that has been most recently created, we assume that this is the source
		// of truth in terms of configuration settings etc., and we ignore the other instances in this reconcile request
		// (they will be handled when they are being reconciled). If the currently reconciled resource is not the most
		// recent one, we set its status to degraded.
		sort.Sort(SortByCreationTimestamp(items))
		mostRecentResource := customResource.At(allCustomResourcesInNamespace, len(items)-1)
		if mostRecentResource.GetUid() == customResource.GetUid() {
			logger.Info(fmt.Sprintf(
				"At least one other %[1]s exists in this namespace. This %[1]s resource is the most recent one. The state of the other resource(s) will be set to degraded.",
				customResource.GetNaturalLanguageResourceTypeName(),
			))
			// continue with the reconcile request for this resource, let the reconcile requests for the other offending
			// resources handle the situation for those resources
			return false, nil
		} else {
			logger.Info(
				fmt.Sprintf(
					"At least one other %[1]s exists in this namespace, and at least one other %[1]s has been created more recently than this one. Setting the state of this resource to degraded.",
					customResource.GetNaturalLanguageResourceTypeName(),
				),
				fmt.Sprintf("most recently created %s", customResource.GetNaturalLanguageResourceTypeName()),
				fmt.Sprintf("%s (%s)", mostRecentResource.GetName(), mostRecentResource.GetUid()),
			)
			customResource.EnsureResourceIsMarkedAsDegraded(
				"NewerResourceIsPresent",
				fmt.Sprintf("There is a more recently created %s in this namespace, please remove all but one resource instance.", customResource.GetNaturalLanguageResourceTypeName()),
			)
			if err := statusWriter.Update(ctx, customResource.Get()); err != nil {
				logger.Error(err, updateStatusFailedMessage)
				return true, err
			}
			// stop the reconciliation, and do not requeue it
			return true, nil
		}
	}
	return false, nil
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
	statusWriter client.SubResourceWriter,
	customResource CustomResource,
	conditions []metav1.Condition,
	conditionTypeAvailable string,
	logger *logr.Logger,
) (bool, error) {
	firstReconcile := false
	needsRefresh := false
	if len(conditions) == 0 {
		customResource.SetAvailableConditionToUnknown()
		firstReconcile = true
		needsRefresh = true
	} else if availableCondition :=
		meta.FindStatusCondition(
			conditions,
			conditionTypeAvailable,
		); availableCondition == nil {
		customResource.SetAvailableConditionToUnknown()
		needsRefresh = true
	}
	if needsRefresh {
		err := updateResourceStatus(ctx, statusWriter, customResource, logger)
		if err != nil {
			// The error has already been logged in refreshStatus
			return firstReconcile, err
		}
	}
	return firstReconcile, nil
}

func updateResourceStatus(
	ctx context.Context,
	statusWriter client.SubResourceWriter,
	customResource CustomResource,
	logger *logr.Logger,
) error {
	if err := statusWriter.Update(ctx, customResource.Get()); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot update the status of the %s.", customResource.GetNaturalLanguageResourceTypeName()))
		return err
	}
	return nil
}

func CheckImminentDeletionAndHandleFinalizers(
	ctx context.Context,
	k8sClient client.Client,
	customResource CustomResource,
	finalizerId string,
	logger *logr.Logger,
) (bool, bool, error) {
	isMarkedForDeletion := customResource.IsMarkedForDeletion()
	if !isMarkedForDeletion {
		err := addFinalizerIfNecessary(
			ctx,
			k8sClient,
			customResource,
			finalizerId,
		)
		if err != nil {
			logger.Error(
				err,
				fmt.Sprintf(
					"Failed to add finalizer to %s, requeuing reconcile request.",
					customResource.GetNaturalLanguageResourceTypeName(),
				),
			)
			return isMarkedForDeletion, false, err
		}
		return isMarkedForDeletion, false, nil
	} else {
		if controllerutil.ContainsFinalizer(customResource.Get(), finalizerId) {
			return isMarkedForDeletion, true, nil
		}
		return isMarkedForDeletion, false, nil
	}
}

func addFinalizerIfNecessary(
	ctx context.Context,
	k8sClient client.Client,
	customResource CustomResource,
	finalizerId string,
) error {
	finalizerHasBeenAdded := controllerutil.AddFinalizer(customResource.Get(), finalizerId)
	if finalizerHasBeenAdded {
		return k8sClient.Update(ctx, customResource.Get())
	}
	// The resource already had the finalizer, no update necessary.
	return nil
}
