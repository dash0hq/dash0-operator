// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package postdelete

import (
	"context"
	"fmt"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/retry"
)

const (
	defaultClearAllowlistPathsTimeout  = 60 * time.Second
	defaultSynchronizerDeletionTimeout = 30 * time.Second
	defaultLeftoverAllowlistsTimeout   = 60 * time.Second
	defaultPollingInterval             = 500 * time.Millisecond
	// GKE has been observed to recreate allowlists a few seconds after removing them, hence the absence of allowlists
	// is only accepted once it has lasted for this long.
	defaultQuietPeriod = 15 * time.Second

	gkeAutopilotAllowlistSynchronizerGroup   = "auto.gke.io"
	gkeAutopilotAllowlistSynchronizerKind    = "AllowlistSynchronizer"
	gkeAutopilotAllowlistSynchronizerVersion = "v1"
	dash0AllowlistSynchronizerName           = "dash0-allowlist-synchronizer"

	gkeAutopilotWorkloadAllowlistKind = "WorkloadAllowlist"

	// The label GKE puts on every WorkloadAllowlist installed by the Dash0 AllowlistSynchronizer.
	managedBySynchronizerLabelKey   = "auto.gke.io/managed-by-synchronizer-" + dash0AllowlistSynchronizerName
	managedBySynchronizerLabelValue = "true"
)

var (
	allowlistSynchronizerGvk = schema.GroupVersionKind{
		Group:   gkeAutopilotAllowlistSynchronizerGroup,
		Kind:    gkeAutopilotAllowlistSynchronizerKind,
		Version: gkeAutopilotAllowlistSynchronizerVersion,
	}
	workloadAllowlistListGvk = schema.GroupVersionKind{
		Group:   gkeAutopilotAllowlistSynchronizerGroup,
		Kind:    gkeAutopilotWorkloadAllowlistKind + "List",
		Version: gkeAutopilotAllowlistSynchronizerVersion,
	}

	clearAllowlistPathsBackoff = wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1.5,
		Steps:    5,
	}
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
	client                      client.WithWatch
	logger                      logd.Logger
	clearAllowlistPathsTimeout  time.Duration
	synchronizerDeletionTimeout time.Duration
	leftoverAllowlistsTimeout   time.Duration
	pollingInterval             time.Duration
	quietPeriod                 time.Duration
}

func NewOperatorPostDeleteHandler() (*OperatorPostDeleteHandler, error) {
	config := ctrl.GetConfigOrDie()
	return NewOperatorPostDeleteHandlerFromConfig(config)
}

func NewOperatorPostDeleteHandlerFromConfig(config *rest.Config) (*OperatorPostDeleteHandler, error) {
	logger := logd.NewLogger(ctrl.Log.WithName("dash0-remove-allowlist-synchronizer"))
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
		client:                      k8sClient,
		logger:                      logger,
		clearAllowlistPathsTimeout:  defaultClearAllowlistPathsTimeout,
		synchronizerDeletionTimeout: defaultSynchronizerDeletionTimeout,
		leftoverAllowlistsTimeout:   defaultLeftoverAllowlistsTimeout,
		pollingInterval:             defaultPollingInterval,
		quietPeriod:                 defaultQuietPeriod,
	}, nil
}

func (h *OperatorPostDeleteHandler) setTimeout(timeout time.Duration) {
	h.clearAllowlistPathsTimeout = timeout
	h.synchronizerDeletionTimeout = timeout
	h.leftoverAllowlistsTimeout = timeout
}

func (h *OperatorPostDeleteHandler) setQuietPeriod(quietPeriod time.Duration) {
	h.quietPeriod = quietPeriod
}

// DeleteGkeAutopilotAllowlistSynchronizer removes the Dash0 AllowlistSynchronizer and all WorkloadAllowlists it has
// installed. It first removes all paths from the AllowlistSynchronizer, which makes GKE uninstall the allowlists, then
// deletes the AllowlistSynchronizer, and finally deletes the allowlists that are still left.
func (h *OperatorPostDeleteHandler) DeleteGkeAutopilotAllowlistSynchronizer(ctx context.Context, logger logd.Logger) error {
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

	if h.clearAllowlistPathsAndWait(ctx, logger) {
		if err := h.deleteAllowlistSynchronizerAndWait(ctx, logger); err != nil {
			return err
		}
	}
	h.waitUntilNoAllowlistsAreLeft(ctx, logger, h.leftoverAllowlistsTimeout, true)
	return nil
}

// clearAllowlistPathsAndWait removes all paths from the Dash0 AllowlistSynchronizer and waits until GKE has uninstalled
// the WorkloadAllowlists. It returns false if the Dash0 AllowlistSynchronizer does not exist.
func (h *OperatorPostDeleteHandler) clearAllowlistPathsAndWait(ctx context.Context, logger logd.Logger) bool {
	var err error
	_ = retry.Retry(
		fmt.Sprintf("removing the allowlist paths from the AllowlistSynchronizer %s", dash0AllowlistSynchronizerName),
		func() error {
			err = h.client.Patch(
				ctx,
				newDash0AllowlistSynchronizer(),
				client.RawPatch(types.MergePatchType, []byte(`{"spec":{"allowlistPaths":[]}}`)),
			)
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) || apierrors.IsForbidden(err) {
				return retry.NewRetryableError(err, false)
			}
			return err
		},
		clearAllowlistPathsBackoff,
		&logger,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(fmt.Sprintf("The AllowlistSynchronizer %s does not exist.", dash0AllowlistSynchronizerName))
			return false
		}
		logger.ErrorAsWarn(err, fmt.Sprintf(
			"Removing the allowlist paths from the AllowlistSynchronizer %s has failed, deleting it right away.",
			dash0AllowlistSynchronizerName))
		return true
	}
	logger.Info(fmt.Sprintf(
		"The allowlist paths have been removed from the AllowlistSynchronizer %s, waiting for GKE to uninstall the "+
			"WorkloadAllowlists.", dash0AllowlistSynchronizerName))
	h.waitUntilNoAllowlistsAreLeft(ctx, logger, h.clearAllowlistPathsTimeout, false)
	return true
}

func (h *OperatorPostDeleteHandler) deleteAllowlistSynchronizerAndWait(ctx context.Context, logger logd.Logger) error {
	if err := h.client.Delete(ctx, newDash0AllowlistSynchronizer()); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		logger.Error(
			err,
			fmt.Sprintf(
				"error when attempting to delete the Dash0 AllowlistSynchronizer (%s: %s)",
				dash0AllowlistSynchronizerName,
				gkeAutopilotAllowlistSynchronizerCrdName,
			))
		return err
	}

	logger.Info(fmt.Sprintf(
		"The deletion of the AllowlistSynchronizer %s has been requested, waiting for it to be gone.",
		dash0AllowlistSynchronizerName))
	err := wait.PollUntilContextTimeout(ctx, h.pollingInterval, h.synchronizerDeletionTimeout, true,
		func(ctx context.Context) (bool, error) {
			if err := h.client.Get(
				ctx,
				client.ObjectKey{Name: dash0AllowlistSynchronizerName},
				newDash0AllowlistSynchronizer(),
			); err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				logger.Error(err, fmt.Sprintf(
					"Failed to read the AllowlistSynchronizer %s while waiting for its deletion.",
					dash0AllowlistSynchronizerName))
			}
			return false, nil
		})
	if err != nil {
		logger.Warn(fmt.Sprintf(
			"The AllowlistSynchronizer %s has not been deleted within the timeout of %d seconds.",
			dash0AllowlistSynchronizerName, int(h.synchronizerDeletionTimeout/time.Second)))
		return nil
	}
	logger.Info(fmt.Sprintf("The AllowlistSynchronizer %s has been deleted.", dash0AllowlistSynchronizerName))
	return nil
}

// waitUntilNoAllowlistsAreLeft polls until no WorkloadAllowlist installed by the Dash0 AllowlistSynchronizer has been
// seen for the quiet period, or the timeout has passed. With deleteRemaining, it requests the deletion of every remaining
// allowlist that is not already being deleted, including allowlists that are recreated in the meantime.
func (h *OperatorPostDeleteHandler) waitUntilNoAllowlistsAreLeft(
	ctx context.Context,
	logger logd.Logger,
	timeout time.Duration,
	deleteRemaining bool,
) {
	var lastSeenRemainingAllowlists []string
	listSucceededAtLeastOnce := false
	var noAllowlistsSince time.Time
	err := wait.PollUntilContextTimeout(ctx, h.pollingInterval, timeout, true,
		func(ctx context.Context) (bool, error) {
			remainingAllowlists := &unstructured.UnstructuredList{}
			remainingAllowlists.SetGroupVersionKind(workloadAllowlistListGvk)
			if err := h.client.List(
				ctx,
				remainingAllowlists,
				client.MatchingLabels{managedBySynchronizerLabelKey: managedBySynchronizerLabelValue},
			); err != nil {
				if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
					lastSeenRemainingAllowlists = nil
					return true, nil
				}
				logger.Error(err, "Failed to list the WorkloadAllowlists while waiting for their removal.")
				return false, nil
			}
			listSucceededAtLeastOnce = true
			if len(remainingAllowlists.Items) == 0 {
				lastSeenRemainingAllowlists = nil
				if noAllowlistsSince.IsZero() {
					noAllowlistsSince = time.Now()
				}
				return time.Since(noAllowlistsSince) >= h.quietPeriod, nil
			}
			noAllowlistsSince = time.Time{}
			if len(lastSeenRemainingAllowlists) != len(remainingAllowlists.Items) {
				logger.Info(fmt.Sprintf(
					"Waiting for the removal of %d WorkloadAllowlist(s).", len(remainingAllowlists.Items)))
			}
			lastSeenRemainingAllowlists = namesOf(remainingAllowlists)
			if deleteRemaining {
				h.requestDeletionOfAllowlistsNotBeingDeletedYet(ctx, logger, remainingAllowlists)
			}
			return false, nil
		})

	if err == nil || (listSucceededAtLeastOnce && lastSeenRemainingAllowlists == nil) {
		logger.Info("No WorkloadAllowlist installed by the Dash0 AllowlistSynchronizer is left.")
		return
	}
	remaining := "an unknown number of WorkloadAllowlist(s) are left"
	if lastSeenRemainingAllowlists != nil {
		remaining = fmt.Sprintf("%d WorkloadAllowlist(s) are left: %s",
			len(lastSeenRemainingAllowlists), strings.Join(lastSeenRemainingAllowlists, ", "))
	}
	logger.Warn(fmt.Sprintf(
		"The removal of the WorkloadAllowlists installed by the Dash0 AllowlistSynchronizer has not completed within "+
			"the timeout of %d seconds, %s.",
		int(timeout/time.Second), remaining))
}

func (h *OperatorPostDeleteHandler) requestDeletionOfAllowlistsNotBeingDeletedYet(
	ctx context.Context,
	logger logd.Logger,
	allowlists *unstructured.UnstructuredList,
) {
	for i := range allowlists.Items {
		allowlist := &allowlists.Items[i]
		if allowlist.GetDeletionTimestamp() != nil {
			continue
		}
		if err := h.client.Delete(ctx, allowlist); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, fmt.Sprintf("Failed to delete the WorkloadAllowlist %s.", allowlist.GetName()))
			continue
		}
		logger.Info(fmt.Sprintf("Successfully requested the deletion of the WorkloadAllowlist %s.", allowlist.GetName()))
	}
}

func newDash0AllowlistSynchronizer() *unstructured.Unstructured {
	dash0AllowlistSynchronizer := &unstructured.Unstructured{}
	dash0AllowlistSynchronizer.SetGroupVersionKind(allowlistSynchronizerGvk)
	dash0AllowlistSynchronizer.SetName(dash0AllowlistSynchronizerName)
	return dash0AllowlistSynchronizer
}

func namesOf(allowlists *unstructured.UnstructuredList) []string {
	names := make([]string, 0, len(allowlists.Items))
	for _, allowlist := range allowlists.Items {
		names = append(names, allowlist.GetName())
	}
	return names
}
