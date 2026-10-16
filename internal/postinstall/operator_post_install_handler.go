// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postinstall

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/retry"
	"github.com/dash0hq/dash0-operator/internal/webhookcerts"
)

// OperatorPostInstallHandler handle the post-install Helm hook that is responsible for waiting until the operator
// configuration auto resource becomes available.
type OperatorPostInstallHandler struct {
	client                   client.WithWatch
	logger                   logd.Logger
	retryBackoff             wait.Backoff
	reachabilityRetryBackoff wait.Backoff
}

const reachabilityCheckCallTimeout = 10 * time.Second

var (
	defaultRetryBackoff = wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   1,
		Steps:    60,
	}

	// The reachability check waits for up to four minutes, which is below the default timeout of Helm (five minutes).
	defaultReachabilityRetryBackoff = wait.Backoff{
		Duration: 2 * time.Second,
		Factor:   1,
		Steps:    120,
	}

	// dash0WebhookCallFailure matches the errors the API server returns when it cannot call one of the operator's
	// admission webhooks or its conversion webhook for the Dash0 CRDs.
	dash0WebhookCallFailure = regexp.MustCompile(
		`failed calling webhook "[^"]*\.dash0\.com"|conversion webhook for operator\.dash0\.com/`,
	)
)

func NewOperatorPostInstallHandler() (*OperatorPostInstallHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPostInstallHandlerFromConfig(config)
}

func NewOperatorPostInstallHandlerFromConfig(config *rest.Config) (*OperatorPostInstallHandler, error) {
	logger := logd.NewLogger(ctrl.Log.WithName("dash0-operator-configuration-readiness"))
	s := runtime.NewScheme()
	if err := dash0v1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := dash0v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := appsv1.AddToScheme(s); err != nil {
		return nil, err
	}
	k8sClient, err := client.NewWithWatch(config, client.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the dynamic client: %w", err)
	}

	return &OperatorPostInstallHandler{
		client:                   k8sClient,
		logger:                   logger,
		retryBackoff:             defaultRetryBackoff,
		reachabilityRetryBackoff: defaultReachabilityRetryBackoff,
	}, nil
}

func (h *OperatorPostInstallHandler) setRetryBackoff(retryBackoff wait.Backoff) {
	h.retryBackoff = retryBackoff
	h.reachabilityRetryBackoff = retryBackoff
}

// WaitForWebhooksToBecomeReachable waits until the rollout of the operator manager deployment is complete, and until
// the API server can call the operator's admission webhooks. The latter is checked with dry-run requests that create a
// Dash0OperatorConfiguration and a Dash0Monitoring resource; the webhooks allow these requests right away.
func (h *OperatorPostInstallHandler) WaitForWebhooksToBecomeReachable(
	ctx context.Context,
	operatorNamespace string,
	deploymentName string,
) error {
	h.logger.Info(
		"waiting for the rollout of the operator manager deployment to complete and for the webhooks to become reachable",
	)
	if err := retry.Retry(
		"waiting for the operator manager deployment rollout and the webhooks",
		func() error {
			if err := h.checkDeploymentRollout(ctx, operatorNamespace, deploymentName); err != nil {
				return err
			}
			return h.checkWebhookReachability(ctx, operatorNamespace)
		},
		h.reachabilityRetryBackoff,
		&h.logger,
	); err != nil {
		return fmt.Errorf(
			"waiting for the operator manager deployment rollout and the webhooks has failed: %w",
			err,
		)
	}
	h.logger.Info("the operator manager deployment rollout is complete and the webhooks are reachable")
	return nil
}

func (h *OperatorPostInstallHandler) checkDeploymentRollout(
	ctx context.Context,
	operatorNamespace string,
	deploymentName string,
) error {
	callCtx, cancel := context.WithTimeout(ctx, reachabilityCheckCallTimeout)
	defer cancel()
	deployment := &appsv1.Deployment{}
	if err := h.client.Get(callCtx, client.ObjectKey{Namespace: operatorNamespace, Name: deploymentName}, deployment); err != nil {
		return fmt.Errorf("cannot read the operator manager deployment: %w", err)
	}
	status := webhookcerts.EvaluateDeploymentRollout(deployment)
	if status.Failed {
		return retry.NewRetryableError(fmt.Errorf("the operator manager deployment rollout has failed: %s", status.Message), false)
	}
	if !status.Complete {
		return fmt.Errorf("the operator manager deployment rollout is not complete: %s", status.Message)
	}
	return nil
}

func (h *OperatorPostInstallHandler) checkWebhookReachability(ctx context.Context, operatorNamespace string) error {
	objects := []client.Object{
		&dash0v1alpha1.Dash0OperatorConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: util.WebhookReachabilityCheckResourceName},
		},
		&dash0v1beta1.Dash0Monitoring{
			ObjectMeta: metav1.ObjectMeta{Name: util.WebhookReachabilityCheckResourceName, Namespace: operatorNamespace},
		},
	}
	for _, object := range objects {
		callCtx, cancel := context.WithTimeout(ctx, reachabilityCheckCallTimeout)
		err := h.client.Create(callCtx, object, client.DryRunAll)
		cancel()
		if err = classifyDryRunError(err); err != nil {
			return err
		}
	}
	return nil
}

// classifyDryRunError returns nil if the dry-run request has reached the operator's webhooks. An error from the API
// server that does not mention a Dash0 webhook (for example a rejection by a third-party webhook) is not caused by the
// operator's webhooks being unreachable.
func classifyDryRunError(err error) error {
	if err == nil {
		return nil
	}
	if dash0WebhookCallFailure.MatchString(err.Error()) {
		return fmt.Errorf("the API server cannot call the operator's webhooks yet: %w", err)
	}
	var apiStatus apierrors.APIStatus
	if !errors.As(err, &apiStatus) {
		return fmt.Errorf("the dry-run request has failed: %w", err)
	}
	return nil
}

func (h *OperatorPostInstallHandler) WaitForOperatorConfigurationResourceToBecomeAvailable() error {
	ctx := context.Background()
	message :=
		fmt.Sprintf(
			"waiting for the operator configuration resource %s to become available",
			util.OperatorConfigurationAutoResourceName,
		)
	h.logger.Info(message)
	if err := retry.Retry(
		message,
		func() error {
			return h.checkOperatorConfigurationResourceAvailability(ctx)
		},
		h.retryBackoff,
		nil,
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
