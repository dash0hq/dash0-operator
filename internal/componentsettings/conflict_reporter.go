// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package componentsettings

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// ConflictReporter logs new, changed and resolved conflicts between Helm values and spec.components of the Signal
// Control resource (see util.ComponentSettingConflicts). It only logs on the leader, where the reconcilers that report
// on changes of the resources run.
type ConflictReporter struct {
	client               client.Client
	defaults             util.ExtraConfig
	signalControlEnabled bool
	leaderElectionAware  util.LeaderElectionAware
	extraConfig          atomic.Pointer[util.ExtraConfig]

	mutex        sync.Mutex
	lastReported []util.ComponentSettingConflict
}

// NewConflictReporter creates a reporter. The defaults need to match the defaults of the Helm chart, see
// util.ExtraConfigDefaultsFor. The Signal Control resource is only taken into account if signalControlEnabled is true,
// since its custom resource definition is only installed in that case.
func NewConflictReporter(
	k8sClient client.Client,
	extraConfig util.ExtraConfig,
	defaults util.ExtraConfig,
	signalControlEnabled bool,
	leaderElectionAware util.LeaderElectionAware,
) *ConflictReporter {
	r := &ConflictReporter{
		client:               k8sClient,
		defaults:             defaults,
		signalControlEnabled: signalControlEnabled,
		leaderElectionAware:  leaderElectionAware,
	}
	r.extraConfig.Store(&extraConfig)
	return r
}

// NotifyOperatorManagerJustBecameLeader reports the conflicts, since only the leader logs them.
func (r *ConflictReporter) NotifyOperatorManagerJustBecameLeader(ctx context.Context, logger logd.Logger) {
	r.Report(ctx, logger)
}

// UpdateExtraConfig stores the updated extra config map and reports the conflicts with its values.
func (r *ConflictReporter) UpdateExtraConfig(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) {
	r.extraConfig.Store(&extraConfig)
	r.Report(ctx, logger)
}

// Report looks up the Signal Control resource and logs the changes of the settings that are configured both via the
// Helm chart and via this resource since the last report.
func (r *ConflictReporter) Report(ctx context.Context, logger logd.Logger) {
	if r == nil || !r.leaderElectionAware.IsLeader() {
		return
	}
	// Reports are serialized, so that a report based on an outdated state cannot overwrite a more recent one.
	r.mutex.Lock()
	defer r.mutex.Unlock()

	var signalControlComponents *dash0v1alpha1.SignalControlComponents
	if r.signalControlEnabled {
		signalControlResource, err := resources.FindUniqueOrMostRecentResourceInScope(
			ctx, r.client, "", &dash0v1alpha1.Dash0SignalControl{}, logger)
		// Without the Dash0SignalControl CRD, listing fails with a no-match error, which means that no Signal Control
		// resource can exist.
		if err != nil && !meta.IsNoMatchError(err) && !apierrors.IsNotFound(err) {
			logger.Debug("cannot look up the Signal Control resource to report component settings conflicts",
				"error", err.Error())
			return
		}
		if err == nil && signalControlResource != nil {
			signalControlComponents = util.SignalControlComponentsOf(
				signalControlResource.(*dash0v1alpha1.Dash0SignalControl))
		}
	}

	conflicts := util.ComponentSettingConflicts(*r.extraConfig.Load(), r.defaults, signalControlComponents)
	for _, conflict := range conflicts {
		if slices.Contains(r.lastReported, conflict) {
			continue
		}
		logger.Warn(
			fmt.Sprintf("%s %s overrides the custom Helm value %s.",
				conflict.Resource, conflict.Path, conflict.HelmValue),
			"resource", conflict.Resource,
			"path", conflict.Path,
			"value", conflict.ResourceSetting,
			"helmValue", conflict.HelmValue,
			"helmSetting", conflict.HelmSetting,
		)
	}
	for _, conflict := range r.lastReported {
		if !slices.ContainsFunc(conflicts, conflict.IsSameSetting) {
			logger.Info(fmt.Sprintf("%s %s no longer overrides the custom Helm value %s.",
				conflict.Resource, conflict.Path, conflict.HelmValue))
		}
	}
	r.lastReported = conflicts
}
