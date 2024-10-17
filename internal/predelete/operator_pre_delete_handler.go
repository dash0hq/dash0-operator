// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package predelete

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
)

const (
	defaultTimeout = 2 * time.Minute
)

type OperatorPreDeleteHandler struct {
	client  client.WithWatch
	logger  *logr.Logger
	timeout time.Duration
}

func NewOperatorPreDeleteHandler() (*OperatorPreDeleteHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPreDeleteHandlerFromConfig(config)
}

func NewOperatorPreDeleteHandlerFromConfig(config *rest.Config) (*OperatorPreDeleteHandler, error) {
	logger := ctrl.Log.WithName("dash0-uninstrument-all")
	s := runtime.NewScheme()
	err := dash0v1alpha1.AddToScheme(s)
	if err != nil {
		return nil, err
	}
	client, err := client.NewWithWatch(config, client.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the dynamic client: %w", err)
	}

	return &OperatorPreDeleteHandler{
		client:  client,
		logger:  &logger,
		timeout: defaultTimeout,
	}, nil
}

func (r *OperatorPreDeleteHandler) SetTimeout(timeout time.Duration) {
	r.timeout = timeout
}

func (r *OperatorPreDeleteHandler) DeleteAllMonitoringResources() error {
	ctx := context.Background()

	totalNumberOfDash0MonitoringResources, err := r.findAllAndRequestDeletion(ctx)
	if err != nil {
		return err
	}

	if totalNumberOfDash0MonitoringResources == 0 {
		r.logger.Info("No Dash0 monitoring resources have been deleted, nothing to wait for.")
		return nil
	}

	err = r.waitForAllDash0MonitoringResourcesToBeFinalizedAndDeleted(ctx, totalNumberOfDash0MonitoringResources)
	if err != nil {
		return err
	}

	return nil
}

func (r *OperatorPreDeleteHandler) findAllAndRequestDeletion(ctx context.Context) (int, error) {
	allDash0MonitoringResources := &dash0v1alpha1.Dash0MonitoringList{}
	err := r.client.List(ctx, allDash0MonitoringResources)
	if err != nil {
		errMsg := err.Error()
		if apierrors.IsNotFound(err) ||
			strings.Contains(errMsg, "operator.dash0.com/v1alpha1: the server could not find the requested resource") ||
			strings.Contains(errMsg, "no matches for kind \"Dash0\" in version") {
			r.logger.Error(err, "The Dash0 monitoring resource *definition* has not been found. Assuming that no Dash0 "+
				"monitoring resources exist and no cleanup is necessary.")
			return 0, nil
		}

		r.logger.Error(err, "failed to list all Dash0 monitoring resources across all namespaces")
		return 0, fmt.Errorf("failed to list all Dash0 monitoring resources across all namespaces: %w", err)
	}

	if len(allDash0MonitoringResources.Items) == 0 {
		r.logger.Info("No Dash0 monitoring resources have been found. Nothing to delete.")
		return 0, nil
	}

	for _, dash0MonitoringResource := range allDash0MonitoringResources.Items {
		namespace := dash0MonitoringResource.Namespace
		// You would think that the following call without the "client.InNamespace(namespace)" would delete all
		// resources across all namespaces in one go, but instead it fails with "the server could not find the requested
		// resource". Same for the dynamic client.
		err = r.client.DeleteAllOf(ctx, &dash0v1alpha1.Dash0Monitoring{}, client.InNamespace(namespace))
		if err != nil {
			r.logger.Error(err, fmt.Sprintf("Failed to delete Dash0 monitoring resource in namespace %s.", namespace))
		} else {
			r.logger.Info(
				fmt.Sprintf("Successfully requested the deletion of the Dash0 monitoring resource in namespace %s.",
					namespace))
		}
	}

	return len(allDash0MonitoringResources.Items), nil
}

func (r *OperatorPreDeleteHandler) waitForAllDash0MonitoringResourcesToBeFinalizedAndDeleted(
	ctx context.Context,
	totalNumberOfDash0MonitoringResources int,
) error {
	watcher, err := r.client.Watch(ctx, &dash0v1alpha1.Dash0MonitoringList{})
	if err != nil {
		r.logger.Error(err, "failed to watch Dash0 monitoring resources across all namespaces to wait for deletion")
		return fmt.Errorf("failed to watch Dash0 monitoring resources across all namespaces to wait for deletion: %w", err)
	}

	// by stopping the watcher we make sure the goroutine running r.watchAndProcessEvents will terminate
	defer watcher.Stop()

	channelToSignalDeletions := make(chan string)
	go func() {
		r.watchAndProcessEvents(watcher, &channelToSignalDeletions)
	}()

	r.logger.Info(
		fmt.Sprintf("Waiting for the deletion of %d Dash0 monitoring resource(s) across all namespaces.",
			totalNumberOfDash0MonitoringResources))
	successfullyDeletedDash0MonitoringResources := 0
	timeoutHasOccured := false
	for !timeoutHasOccured && successfullyDeletedDash0MonitoringResources < totalNumberOfDash0MonitoringResources {
		select {
		case <-time.After(r.timeout):
			timeoutHasOccured = true

		case namespaceOfDeletedResource := <-channelToSignalDeletions:
			successfullyDeletedDash0MonitoringResources++
			r.logger.Info(
				fmt.Sprintf("The deletion of the Dash0 monitoring resource in namespace %s has completed successfully (%d/%d).",
					namespaceOfDeletedResource, successfullyDeletedDash0MonitoringResources, totalNumberOfDash0MonitoringResources))
		}
	}

	if timeoutHasOccured {
		r.logger.Info(
			fmt.Sprintf("The deletion of all Dash0 monitoring resource(s) across all namespaces has not completed "+
				"successfully within the timeout of %d seconds. %d of %d resources have been deleted.",
				int(r.timeout/time.Second),
				successfullyDeletedDash0MonitoringResources,
				totalNumberOfDash0MonitoringResources,
			))
	} else {
		r.logger.Info(
			fmt.Sprintf("The deletion of all %d Dash0 monitoring resource(s) across all namespaces has completed successfully.",
				totalNumberOfDash0MonitoringResources))
	}

	return nil
}

func (r *OperatorPreDeleteHandler) watchAndProcessEvents(
	watcher watch.Interface,
	channelToSignalDeletions *chan string,
) {
	for event := range watcher.ResultChan() {
		switch event.Type {
		case watch.Deleted:
			namespace := event.Object.(*dash0v1alpha1.Dash0Monitoring).Namespace
			*channelToSignalDeletions <- namespace
		}
	}
}
