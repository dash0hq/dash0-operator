// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
)

type ReadyCheckExecuter struct {
	client.Client

	OperatorNamespace  string
	WebhookServiceName string

	retryBackoff       wait.Backoff
	bypassWebhookCheck bool

	webhookEndpointHasPortAssigned atomic.Bool
}

var (
	defaultRetryBackoff = wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1,
		Steps:    60,
	}
)

func NewReadyCheckExecuter(
	client client.Client,
	operatorNamespace string,
	webhookServiceName string,
) *ReadyCheckExecuter {
	return &ReadyCheckExecuter{
		Client:             client,
		OperatorNamespace:  operatorNamespace,
		WebhookServiceName: webhookServiceName,
		retryBackoff:       defaultRetryBackoff,
	}
}

// start begins polling the webhook endpoint until it gets a port assigned, in a separate Goroutine. It returns
// immediately. Clients can check the result of the polling via the isReady method.
func (c *ReadyCheckExecuter) start(ctx context.Context, logger *logr.Logger) {
	go func() {
		if err := c.pollWebhookServiceEndpoint(ctx, logger, false); err != nil {
			logger.Error(err, "failed to poll the webhook service endpoint, the pod will not be marked as ready")
		}
	}()
}

// isReady checks whether the webhook service endpoint has a port assigned, and returns an error if not. Despite the
// name, this check does not include checking the Ready condition of the endpoint.
func (c *ReadyCheckExecuter) isReady(_ *http.Request) error {
	if !c.webhookEndpointHasPortAssigned.Load() {
		return fmt.Errorf("the Dash0 operator webhook service is not available yet")
	}
	return nil
}

// waitForWebhookServiceEndpointToBecomeReady blocks until the webhook service endpoint has become ready, or until the
// polling routine for the webhook service gives up.
func (c *ReadyCheckExecuter) waitForWebhookServiceEndpointToBecomeReady(ctx context.Context, logger *logr.Logger) (bool, error) {
	if err := c.pollWebhookServiceEndpoint(ctx, logger, true); err != nil {
		return false, err
	}
	return c.webhookEndpointHasPortAssigned.Load(), nil
}

// pollWebhookServiceEndpoint polls the webhook service endpoint and only returns when the webhook service endpoint has
// a port assigned. If waitForReadyCondition is true, it additionally also checks the Ready condition of the endpoint.
// (This check needs to be skipped for the operator manager pod's readyness check, since a service endpoint only will be
// marked ready when its underlying pod is marked as ready). The function also returns when the polling times out after
// one minute.
func (c *ReadyCheckExecuter) pollWebhookServiceEndpoint(ctx context.Context, logger *logr.Logger, waitForReadyCondition bool) error {
	if c.bypassWebhookCheck {
		logger.Info("bypassing the webhook service endpoint check")
		c.webhookEndpointHasPortAssigned.Store(true)
		return nil
	}

	conditionLabel := "it has a port assigned"
	if waitForReadyCondition {
		conditionLabel = "it has a port assigned and is marked as ready"
	}
	logger.Info(
		fmt.Sprintf(
			"starting to poll the webhook service endpoint until %s",
			conditionLabel,
		))
	if err := util.RetryWithCustomBackoff(
		"waiting for webhook service endpoint",
		func() error {
			return c.checkWebhookServiceEndpoint(ctx, waitForReadyCondition)
		},
		c.retryBackoff,
		false,
		logger,
	); err != nil {
		e := fmt.Errorf("waiting for the webhook service endpoint has timed out (no more retries left): %v", err)
		return e
	}
	logger.Info(fmt.Sprintf("the webhook service endpoint is ready (%s)", conditionLabel))
	return nil
}

//nolint:staticcheck
func (c *ReadyCheckExecuter) checkWebhookServiceEndpoint(
	ctx context.Context,
	waitForReadyCondition bool,
) error {
	labelSelector := labels.SelectorFromSet(map[string]string{"kubernetes.io/service-name": c.WebhookServiceName})
	endpointSliceList := discoveryv1.EndpointSliceList{}
	if err := c.List(ctx, &endpointSliceList, &client.ListOptions{
		Namespace:     c.OperatorNamespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return err
	}

	for _, endpointSlice := range endpointSliceList.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if waitForReadyCondition {
				// The endpoint does not get marked as ready until its backing pod is marked as ready, so for the
				// ready check of the operator manager pod, this would be a cyclic dependency, i.e. a deadlock.
				// Hence, this check is skipped for the operator manager pod's ready check.
				// We do include the check when evaluating whether the operator configuration auto resource can be
				// created. It can take a couple of seconds for the endpoint to be marked as ready with the ready
				// condition after our own pod's ready check returns true.
				if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
					// wait for the endpoint to be marked as ready
					continue
				}
			}
			for _, port := range endpointSlice.Ports {
				if port.Port != nil && *port.Port == 9443 {
					// the endpoint is ready (that is, as ready as it can be before the backing pod is marked as ready)
					c.webhookEndpointHasPortAssigned.Store(true)
					return nil
				}
			}
		}
	}

	return fmt.Errorf("the webhook service endpoint is not ready yet")
}
