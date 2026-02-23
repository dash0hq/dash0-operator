// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postdelete

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

const (
	defaultTimeout = 2 * time.Minute

	gkeAutopilotAllowlistSynchronizerGroup   = "auto.gke.io"
	gkeAutopilotAllowlistSynchronizerKind    = "AllowlistSynchronizer"
	gkeAutopilotAllowlistSynchronizerVersion = "v1"
	dash0AllowlistSynchronizerName           = "dash0-allowlist-synchronizer"
)

var (
	// "allowlistsynchronizers"
	gkeAutopilotAllowlistSynchronizerPlural = fmt.Sprintf("%ss", strings.ToLower(gkeAutopilotAllowlistSynchronizerKind))

	// "allowlistsynchronizers.auto.gke.io"
	gkeAutopilotAllowlistSynchronizerCrdName = fmt.Sprintf(
		"%s.%s",
		gkeAutopilotAllowlistSynchronizerPlural,
		gkeAutopilotAllowlistSynchronizerGroup,
	)
)

type OperatorPostDeleteHandler struct {
	client  client.WithWatch
	logger  logr.Logger
	timeout time.Duration
}

func NewOperatorPostDeleteHandler() (*OperatorPostDeleteHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPostDeleteHandlerFromConfig(config)
}

func NewOperatorPostDeleteHandlerFromConfig(config *rest.Config) (*OperatorPostDeleteHandler, error) {
	logger := ctrl.Log.WithName("dash0-remove-allowlist-synchronizer")
	s := runtime.NewScheme()
	if err := dash0v1alpha1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := dash0v1beta1.AddToScheme(s); err != nil {
		return nil, err
	}
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		return nil, err
	}
	k8sClient, err := client.NewWithWatch(config, client.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the dynamic client: %w", err)
	}

	return &OperatorPostDeleteHandler{
		client:  k8sClient,
		logger:  logger,
		timeout: defaultTimeout,
	}, nil
}

func (h *OperatorPostDeleteHandler) setTimeout(timeout time.Duration) {
	h.timeout = timeout
}

func (h *OperatorPostDeleteHandler) DeleteGkeAutopilotAllowlistSynchronizer(ctx context.Context, logger logr.Logger) error {
	if err := h.client.Get(ctx, client.ObjectKey{
		Name: gkeAutopilotAllowlistSynchronizerCrdName,
	}, &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(
				err,
				fmt.Sprintf(
					"error when checking for the existence of the GKE Autopilot AllowlistSynchronizer CRD (client.Get(\"%s\") failed)",
					gkeAutopilotAllowlistSynchronizerCrdName,
				))
		}

		// The AllowlistSynchronizer CRD does not exist, hence we can be sure that the Dash0 AllowlistSynchronizer also
		// does not exist. Abort the attempt to delete the AllowlistSynchronizer and terminate with exit code 0.
		return nil
	}

	dash0AllowlistSynchronizer := &unstructured.Unstructured{}
	dash0AllowlistSynchronizer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gkeAutopilotAllowlistSynchronizerGroup,
		Kind:    gkeAutopilotAllowlistSynchronizerKind,
		Version: gkeAutopilotAllowlistSynchronizerVersion,
	})
	dash0AllowlistSynchronizer.SetName(dash0AllowlistSynchronizerName)

	if err := h.client.Delete(ctx, dash0AllowlistSynchronizer); err != nil {
		if apierrors.IsNotFound(err) {
			// The dash0-allowlist-synchronizer resource does not exist, nothing to do.
			return nil
		} else {
			logger.Error(
				err,
				fmt.Sprintf(
					"error when attempting to delete the Dash0 AllowlistSynchronizer (%s: %s)",
					dash0AllowlistSynchronizerName,
					gkeAutopilotAllowlistSynchronizerCrdName,
				))
			return err
		}
	}
	return nil
}
