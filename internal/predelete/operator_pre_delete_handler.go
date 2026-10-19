// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package predelete

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	defaultTimeout         = 2 * time.Minute
	defaultPollingInterval = 500 * time.Millisecond
)

type OperatorPreDeleteHandler struct {
	client          client.Client
	logger          logd.Logger
	timeout         time.Duration
	pollingInterval time.Duration
}

func NewOperatorPreDeleteHandler() (*OperatorPreDeleteHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPreDeleteHandlerFromConfig(config)
}

func NewOperatorPreDeleteHandlerFromConfig(config *rest.Config) (*OperatorPreDeleteHandler, error) {
	logger := logd.NewLogger(ctrl.Log.WithName("dash0-uninstrument-all"))
	s := runtime.NewScheme()
	if err := dash0v1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := dash0v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	k8sClient, err := client.New(config, client.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the dynamic client: %w", err)
	}

	return &OperatorPreDeleteHandler{
		client:          k8sClient,
		logger:          logger,
		timeout:         defaultTimeout,
		pollingInterval: defaultPollingInterval,
	}, nil
}

func (h *OperatorPreDeleteHandler) setTimeout(timeout time.Duration) {
	h.timeout = timeout
}

func (h *OperatorPreDeleteHandler) DeleteAllMonitoringResources() error {
	ctx := context.Background()

	allDash0MonitoringResources := &dash0v1beta1.Dash0MonitoringList{}
	if err := h.client.List(ctx, allDash0MonitoringResources); err != nil {
		if isResourceDefinitionMissing(err) {
			h.logger.Error(err, "The Dash0 monitoring resource *definition* has not been found. Assuming that no Dash0 "+
				"monitoring resources exist and no cleanup is necessary.")
			return nil
		}
		h.logger.Error(err, "failed to list all Dash0 monitoring resources across all namespaces")
		return fmt.Errorf("failed to list all Dash0 monitoring resources across all namespaces: %w", err)
	}
	if len(allDash0MonitoringResources.Items) == 0 {
		h.logger.Info("No Dash0 monitoring resources have been found. Nothing to delete.")
		return nil
	}

	h.deleteAndWaitUntilNoMonitoringResourcesAreLeft(ctx)

	// We do not need to manually delete the Dash0 operator configuration resource. helm uninstall will also remove
	// both of our CRDs and that will also delete all operator configuration resources. The reason we are having this
	// predelete handler for the Dash0 monitoring resources is that the monitoring resource has a finalizer and needs
	// to actually do work (uninstrumenting workloads, potentially remove the otel collector) before the resource can be
	// deleted. The operator configuration resource does not have a finalizer and can be deleted without any cleanup.
	return nil
}

// deleteAndWaitUntilNoMonitoringResourcesAreLeft requests the deletion of every Dash0 monitoring resource that is not
// already being deleted, and polls until none is left or the timeout has passed. Resources that are recreated in the
// meantime are deleted again.
func (h *OperatorPreDeleteHandler) deleteAndWaitUntilNoMonitoringResourcesAreLeft(ctx context.Context) {
	var lastSeenRemainingResources *dash0v1beta1.Dash0MonitoringList
	err := wait.PollUntilContextTimeout(ctx, h.pollingInterval, h.timeout, true,
		func(ctx context.Context) (bool, error) {
			remainingResources := &dash0v1beta1.Dash0MonitoringList{}
			if err := h.client.List(ctx, remainingResources); err != nil {
				if isResourceDefinitionMissing(err) {
					h.logger.Info("The Dash0 monitoring resource definition is gone, assuming all Dash0 monitoring " +
						"resources have been deleted.")
					return true, nil
				}
				h.logger.Error(err, "failed to list Dash0 monitoring resources while waiting for their deletion")
				return false, nil
			}
			numberOfRemainingResources := len(remainingResources.Items)
			if numberOfRemainingResources == 0 {
				return true, nil
			}
			if lastSeenRemainingResources == nil || len(lastSeenRemainingResources.Items) != numberOfRemainingResources {
				h.logger.Info(fmt.Sprintf(
					"Waiting for the deletion of %d Dash0 monitoring resource(s) across all namespaces.",
					numberOfRemainingResources))
			}
			lastSeenRemainingResources = remainingResources
			h.requestDeletionOfResourcesNotBeingDeletedYet(ctx, remainingResources)
			return false, nil
		})

	if err == nil {
		h.logger.Info("The deletion of all Dash0 monitoring resource(s) across all namespaces has completed " +
			"successfully.")
		return
	}
	remaining := "an unknown number of resource(s) are left"
	if lastSeenRemainingResources != nil {
		remaining = fmt.Sprintf("%d resource(s) are left in the following namespace(s): %s",
			len(lastSeenRemainingResources.Items), strings.Join(namespacesOf(lastSeenRemainingResources), ", "))
	}
	h.logger.Warn(fmt.Sprintf(
		"The deletion of all Dash0 monitoring resource(s) across all namespaces has not completed successfully within "+
			"the timeout of %d seconds, %s.",
		int(h.timeout/time.Second), remaining))
}

func (h *OperatorPreDeleteHandler) requestDeletionOfResourcesNotBeingDeletedYet(
	ctx context.Context,
	monitoringResources *dash0v1beta1.Dash0MonitoringList,
) {
	for i := range monitoringResources.Items {
		monitoringResource := &monitoringResources.Items[i]
		if monitoringResource.DeletionTimestamp != nil {
			continue
		}
		if err := h.client.Delete(ctx, monitoringResource); err != nil && !apierrors.IsNotFound(err) {
			h.logger.Error(err, fmt.Sprintf("Failed to delete the Dash0 monitoring resource in namespace %s.",
				monitoringResource.Namespace))
			continue
		}
		h.logger.Info(fmt.Sprintf("Successfully requested the deletion of the Dash0 monitoring resource in namespace %s.",
			monitoringResource.Namespace))
	}
}

func namespacesOf(monitoringResources *dash0v1beta1.Dash0MonitoringList) []string {
	namespaces := make([]string, 0, len(monitoringResources.Items))
	for _, monitoringResource := range monitoringResources.Items {
		namespaces = append(namespaces, monitoringResource.Namespace)
	}
	return namespaces
}

func isResourceDefinitionMissing(err error) bool {
	errMsg := err.Error()
	return apierrors.IsNotFound(err) ||
		meta.IsNoMatchError(err) ||
		strings.Contains(errMsg, "operator.dash0.com/v1alpha1: the server could not find the requested resource") ||
		strings.Contains(errMsg, "no matches for kind \"Dash0\" in version")
}
