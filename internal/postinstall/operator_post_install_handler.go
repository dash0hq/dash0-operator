// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postinstall

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

// OperatorPostInstallHandler handle the post-install Helm hook that is responsible for waiting until the operator
// configuration auto resource becomes available.
type OperatorPostInstallHandler struct {
	client       client.WithWatch
	logger       *logr.Logger
	retryBackoff wait.Backoff
}

var (
	defaultRetryBackoff = wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   1,
		Steps:    60,
	}
)

func NewOperatorPostInstallHandler() (*OperatorPostInstallHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPostInstallHandlerFromConfig(config)
}

func NewOperatorPostInstallHandlerFromConfig(config *rest.Config) (*OperatorPostInstallHandler, error) {
	logger := ctrl.Log.WithName("dash0-operator-configuration-readiness")
	s := runtime.NewScheme()
	if err := dash0v1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := dash0v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	k8sClient, err := client.NewWithWatch(config, client.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the dynamic client: %w", err)
	}

	return &OperatorPostInstallHandler{
		client:       k8sClient,
		logger:       &logger,
		retryBackoff: defaultRetryBackoff,
	}, nil
}

func (h *OperatorPostInstallHandler) setRetryBackoff(retryBackoff wait.Backoff) {
	h.retryBackoff = retryBackoff
}

func (h *OperatorPostInstallHandler) WaitForOperatorConfigurationResourceToBecomeAvailable() error {
	ctx := context.Background()
	message :=
		fmt.Sprintf(
			"waiting for the operator configuration resource %s to become available",
			util.OperatorConfigurationAutoResourceName,
		)
	h.logger.Info(message)
	if err := util.RetryWithCustomBackoff(
		message,
		func() error {
			return h.checkOperatorConfigurationResourceAvailability(ctx)
		},
		h.retryBackoff,
		false,
		h.logger,
	); err != nil {
		return fmt.Errorf(
			"waiting for %s to become available has timed out (no more retries left): %v",
			util.OperatorConfigurationAutoResourceName,
			err,
		)
	}
	h.logger.Info(
		fmt.Sprintf("the operator configuration resource %s is available now, terminating",
			util.OperatorConfigurationAutoResourceName,
		))

	return nil
}

func (h *OperatorPostInstallHandler) checkOperatorConfigurationResourceAvailability(ctx context.Context) error {
	operatorConfigurationAutoResource := &dash0v1alpha1.Dash0OperatorConfiguration{}
	if err := h.client.Get(
		ctx,
		client.ObjectKey{Name: util.OperatorConfigurationAutoResourceName},
		operatorConfigurationAutoResource,
	); err != nil {
		return err
	}

	if !operatorConfigurationAutoResource.IsAvailable() {
		return fmt.Errorf("the Dash0OperatorConfiguration resource %s is not marked as available", util.OperatorConfigurationAutoResourceName)
	}
	return nil
}
