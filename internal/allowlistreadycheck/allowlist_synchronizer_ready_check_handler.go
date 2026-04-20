// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package allowlistreadycheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	gkeAutopilotAllowlistSynchronizerGroup   = "auto.gke.io"
	gkeAutopilotAllowlistSynchronizerKind    = "AllowlistSynchronizer"
	gkeAutopilotAllowlistSynchronizerVersion = "v1"
	dash0AllowlistSynchronizerName           = "dash0-allowlist-synchronizer"

	allowlistSynchronizerReadyConditionType   = "Ready"
	allowlistSynchronizerReadyConditionStatus = "True"

	allowlistInstalledPhase = "Installed"
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

	defaultRetryBackoff = wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   1,
		Steps:    120,
	}
)

// OperatorPreInstallHandler handles the pre-install/pre-upgrade Helm hook that is responsible for waiting until the
// GKE Autopilot AllowlistSynchronizer becomes ready, so that the WorkloadAllowlists it references are available before
// the operator manager deployment is started.
type OperatorPreInstallHandler struct {
	client           client.WithWatch
	logger           logd.Logger
	retryBackoff     wait.Backoff
	allowlistVersion string
}

func NewOperatorPreInstallHandler(version string) (*OperatorPreInstallHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPreInstallHandlerFromConfig(config, version)
}

func NewOperatorPreInstallHandlerFromConfig(
	config *rest.Config,
	allowlistVersion string,
) (*OperatorPreInstallHandler, error) {
	logger := logd.NewLogger(ctrl.Log.WithName("dash0-allowlist-synchronizer-ready-check"))
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

	return &OperatorPreInstallHandler{
		client:           k8sClient,
		logger:           logger,
		retryBackoff:     defaultRetryBackoff,
		allowlistVersion: allowlistVersion,
	}, nil
}

func (h *OperatorPreInstallHandler) setRetryBackoff(retryBackoff wait.Backoff) {
	h.retryBackoff = retryBackoff
}

// WaitForAllowlistSynchronizerToBecomeReady polls the dash0-allowlist-synchronizer AllowlistSynchronizer resource until
// its Ready condition is True. If the AllowlistSynchronizer CRD does not exist in the cluster, it returns immediately
// with success (there is nothing to wait for).
func (h *OperatorPreInstallHandler) WaitForAllowlistSynchronizerToBecomeReady() error {
	ctx := context.Background()

	h.logger.Info("Checking whether the allowlist synchronizer CRD is available.")
	if err := h.client.Get(ctx, client.ObjectKey{
		Name: gkeAutopilotAllowlistSynchronizerCrdName,
	}, &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		if apierrors.IsNotFound(err) {
			h.logger.Info(
				fmt.Sprintf(
					"The AllowlistSynchronizer CRD (%s) does not exist in this cluster, nothing to wait for.",
					gkeAutopilotAllowlistSynchronizerCrdName,
				))
			return nil
		}
		return fmt.Errorf(
			"error checking for the existence of the AllowlistSynchronizer CRD (%s): %w",
			gkeAutopilotAllowlistSynchronizerCrdName,
			err,
		)
	}

	message := fmt.Sprintf(
		"waiting for the AllowlistSynchronizer %s to become ready",
		dash0AllowlistSynchronizerName,
	)
	h.logger.Info(message)
	if err := util.RetryWithCustomBackoff(
		message,
		func() error {
			return h.checkAllowlistSynchronizerReadiness(ctx)
		},
		h.retryBackoff,
		true,
		true,
		h.logger,
	); err != nil {
		return fmt.Errorf(
			"waiting for AllowlistSynchronizer %s to become ready has timed out (no more retries left): %v",
			dash0AllowlistSynchronizerName,
			err,
		)
	}

	h.logger.Info(
		fmt.Sprintf(
			"the AllowlistSynchronizer %s is ready and has synchronized the workload allowlist version %s, terminating",
			dash0AllowlistSynchronizerName,
			h.allowlistVersion,
		))

	return nil
}

func (h *OperatorPreInstallHandler) checkAllowlistSynchronizerReadiness(ctx context.Context) error {
	h.logger.Info(fmt.Sprintf("reading AllowlistSynchronizer %s", dash0AllowlistSynchronizerName))
	allowlistSynchronizer := &unstructured.Unstructured{}
	allowlistSynchronizer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gkeAutopilotAllowlistSynchronizerGroup,
		Kind:    gkeAutopilotAllowlistSynchronizerKind,
		Version: gkeAutopilotAllowlistSynchronizerVersion,
	})

	if err := h.client.Get(ctx, client.ObjectKey{Name: dash0AllowlistSynchronizerName}, allowlistSynchronizer); err != nil {
		return err
	}

	h.logger.Info("reading status conditions")
	conditions, found, err := unstructured.NestedSlice(allowlistSynchronizer.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf(
			"error reading status conditions of AllowlistSynchronizer %s: %w",
			dash0AllowlistSynchronizerName,
			err,
		)
	}
	if !found || len(conditions) == 0 {
		return fmt.Errorf(
			"the AllowlistSynchronizer %s has no status conditions yet",
			dash0AllowlistSynchronizerName,
		)
	}

	for _, conditionRaw := range conditions {
		condition, ok := conditionRaw.(map[string]any)
		if !ok {
			continue
		}
		conditionType, ok := condition["type"].(string)
		if !ok {
			continue
		}
		conditionStatus, ok := condition["status"].(string)
		if !ok {
			continue
		}
		if conditionType == allowlistSynchronizerReadyConditionType &&
			conditionStatus == allowlistSynchronizerReadyConditionStatus {
			h.logger.Info("found ready status, checking allowlist versions")
			return h.checkManagedAllowlistStatus(allowlistSynchronizer)
		}
	}

	return fmt.Errorf(
		"the AllowlistSynchronizer %s does not have condition %s=%s yet",
		dash0AllowlistSynchronizerName,
		allowlistSynchronizerReadyConditionType,
		allowlistSynchronizerReadyConditionStatus,
	)
}

func (h *OperatorPreInstallHandler) checkManagedAllowlistStatus(allowlistSynchronizer *unstructured.Unstructured) error {
	expectedFilePath := fmt.Sprintf(
		"Dash0/operator/%s/dash0-operator-manager-%s.yaml",
		h.allowlistVersion,
		h.allowlistVersion,
	)

	managedAllowlistStatus, found, err := unstructured.NestedSlice(
		allowlistSynchronizer.Object,
		"status",
		"managedAllowlistStatus",
	)
	if err != nil {
		return fmt.Errorf(
			"error reading managedAllowlistStatus of AllowlistSynchronizer %s: %w",
			dash0AllowlistSynchronizerName,
			err,
		)
	}
	if !found || len(managedAllowlistStatus) == 0 {
		return fmt.Errorf(
			"the AllowlistSynchronizer %s has no field managedAllowlistStatus",
			dash0AllowlistSynchronizerName,
		)
	}

	for _, entryRaw := range managedAllowlistStatus {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			continue
		}
		filePath, ok := entry["filePath"].(string)
		if !ok {
			continue
		}
		phase, ok := entry["phase"].(string)
		if !ok {
			continue
		}
		if filePath == expectedFilePath && phase == allowlistInstalledPhase {
			h.logger.Info("found expected managedAllowlistStatus", "filePath", filePath, "phase", phase)
			return nil
		}
	}

	return fmt.Errorf(
		"the AllowlistSynchronizer %s does not have a managedAllowlistStatus entry with filePath=%s and phase=%s yet",
		dash0AllowlistSynchronizerName,
		expectedFilePath,
		allowlistInstalledPhase,
	)
}
