// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package removal

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
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
	err := operatorv1alpha1.AddToScheme(s)
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

func (r *OperatorPreDeleteHandler) DeleteAllDash0CustomResources() error {
	ctx := context.Background()

	totalNumberOfDash0CustomResources, err := r.findAllAndRequestDeletion(ctx)
	if err != nil {
		return err
	}

	err = r.waitForAllDash0CustomResourcesToBeFinalizedAndDeleted(ctx, totalNumberOfDash0CustomResources)
	if err != nil {
		return err
	}

	return nil
}

func (r *OperatorPreDeleteHandler) findAllAndRequestDeletion(ctx context.Context) (int, error) {
	allDash0CustomResources := &operatorv1alpha1.Dash0List{}
	err := r.client.List(ctx, allDash0CustomResources)
	if err != nil {
		r.logger.Error(err, "failed to list all Dash0 custom resources across all namespaces")
		return 0, fmt.Errorf("failed to list all Dash0 custom resourcesa cross all namespaces: %w", err)
	}

	if len(allDash0CustomResources.Items) == 0 {
		r.logger.Info("No Dash0 custom resources have been found. Nothing to delete.")
		return 0, nil
	}

	for _, dash0CustomResource := range allDash0CustomResources.Items {
		namespace := dash0CustomResource.Namespace
		// You would think that the following call without the "client.InNamespace(namespace)" would delete all
		// resources across all namespaces in one go, but instead it fails with "the server could not find the requested
		// resource". Same for the dynamic client.
		err = r.client.DeleteAllOf(ctx, &operatorv1alpha1.Dash0{}, client.InNamespace(namespace))
		if err != nil {
			r.logger.Error(err, fmt.Sprintf("Failed to delete Dash0 custom resource in namespace %s.", namespace))
		} else {
			r.logger.Info(
				fmt.Sprintf("Successfully requested the deletion of the Dash0 custom resource in namespace %s.",
					namespace))
		}
	}

	return len(allDash0CustomResources.Items), nil
}

func (r *OperatorPreDeleteHandler) waitForAllDash0CustomResourcesToBeFinalizedAndDeleted(
	ctx context.Context,
	totalNumberOfDash0CustomResources int,
) error {
	watcher, err := r.client.Watch(ctx, &operatorv1alpha1.Dash0List{})
	if err != nil {
		r.logger.Error(err, "failed to watch Dash0 custom resources across all namespaces to wait for deletion")
		return fmt.Errorf("failed to watch Dash0 custom resources across all namespaces to wait for deletion: %w", err)
	}

	// by stopping the watcher we make sure the goroutine running r.watchAndProcessEvents will terminate
	defer watcher.Stop()

	channelToSignalDeletions := make(chan string)
	go func() {
		r.watchAndProcessEvents(watcher, &channelToSignalDeletions)
	}()

	r.logger.Info(
		fmt.Sprintf("Waiting for the deletion of %d Dash0 custom resource(s) across all namespaces.",
			totalNumberOfDash0CustomResources))
	successfullyDeletedDash0CustomResources := 0
	timeoutHasOccured := false
	for !timeoutHasOccured && successfullyDeletedDash0CustomResources < totalNumberOfDash0CustomResources {
		select {
		case <-time.After(r.timeout):
			timeoutHasOccured = true

		case namespaceOfDeletedResource := <-channelToSignalDeletions:
			successfullyDeletedDash0CustomResources++
			r.logger.Info(
				fmt.Sprintf("The deletion of the Dash0 custom resource in namespace %s has completed successfully (%d/%d).",
					namespaceOfDeletedResource, successfullyDeletedDash0CustomResources, totalNumberOfDash0CustomResources))
		}
	}

	if timeoutHasOccured {
		r.logger.Info(
			fmt.Sprintf("The deletion of all Dash0 custom resource(s) across all namespaces has not completed "+
				"successfully within the timeout of %d seconds. %d of %d resources have been deleted.",
				int(r.timeout/time.Second),
				successfullyDeletedDash0CustomResources,
				totalNumberOfDash0CustomResources,
			))
	} else {
		r.logger.Info(
			fmt.Sprintf("The deletion of all %d Dash0 custom resource(s) across all namespaces has completed successfully.",
				totalNumberOfDash0CustomResources))
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
			namespace := event.Object.(*operatorv1alpha1.Dash0).Namespace
			*channelToSignalDeletions <- namespace
		}
	}
}
