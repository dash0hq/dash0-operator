// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	toolscache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// watchOperatorConfigurationInstrumentationSettings keeps the instrumentation settings of the operator configuration
// resource (the init container resources from spec.components and spec.instrumentWorkloads.instrumentationDelivery) up
// to date on every replica, since the instrumentation webhook runs on all of them, while the operator configuration
// reconciler only runs on the leader. The first read bypasses the unstarted cache. It only applies the init container
// resources, since the Kubernetes versions that the instrumentation delivery is resolved against are not known yet; the
// delivery is initialized from the command line flag until the informer delivers the resource.
func watchOperatorConfigurationInstrumentationSettings(
	ctx context.Context,
	mgr ctrl.Manager,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	extraConfigDefaults util.ExtraConfig,
) error {
	if err := updateOperatorConfigurationInstrumentationSettings(
		ctx, startupTasksK8sClient, clusterInstrumentationConfig, extraConfigDefaults, false, setupLog,
	); err != nil {
		setupLog.Error(err, "cannot read the instrumentation settings of the operator configuration resource at "+
			"startup, they will be applied once the cache has synced")
	}

	informer, err := mgr.GetCache().GetInformer(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})
	if err != nil {
		return fmt.Errorf("cannot get the informer for operator configuration resources: %w", err)
	}
	logger := logd.NewLogger(ctrl.Log.WithName("operator-configuration-instrumentation-settings"))
	update := func() {
		if err := updateOperatorConfigurationInstrumentationSettings(
			ctx, mgr.GetClient(), clusterInstrumentationConfig, extraConfigDefaults, true, logger,
		); err != nil {
			logger.Error(err, "cannot update the instrumentation settings from the operator configuration resource")
		}
	}
	if _, err = informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { update() },
		UpdateFunc: func(any, any) { update() },
		DeleteFunc: func(any) { update() },
	}); err != nil {
		return fmt.Errorf("cannot watch operator configuration resources for instrumentation settings: %w", err)
	}
	return nil
}

func updateOperatorConfigurationInstrumentationSettings(
	ctx context.Context,
	k8sClient client.Client,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	extraConfigDefaults util.ExtraConfig,
	updateDelivery bool,
	logger logd.Logger,
) error {
	resource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx, k8sClient, "", &dash0v1alpha1.Dash0OperatorConfiguration{}, logger)
	if err != nil {
		return err
	}
	var initContainerResources *corev1.ResourceRequirements
	if resource != nil {
		operatorConfigurationResource := resource.(*dash0v1alpha1.Dash0OperatorConfiguration)
		initContainerResources = util.EffectiveInstrumentationInitContainerResources(
			extraConfigDefaults,
			util.OperatorConfigurationComponentsOf(operatorConfigurationResource),
		)
		if updateDelivery {
			updateRequestedInstrumentationDelivery(clusterInstrumentationConfig, operatorConfigurationResource, logger)
		}
	}
	logger.Debug("Updating the init container resources from the operator configuration resource.",
		"resources", initContainerResources)
	clusterInstrumentationConfig.UpdateInstrumentationInitContainerResources(initContainerResources)
	return nil
}

// updateRequestedInstrumentationDelivery resolves spec.instrumentWorkloads.instrumentationDelivery against the detected
// Kubernetes version knowledge (the Kubernetes API server version and the minimum kubelet version) and stores the
// result into the clusterInstrumentationConfig. Subsequent workload instrumentation actions then use the new resolved
// delivery mechanism. Existing instrumented workloads are not re-instrumented, even if the delivery mechanism changes.
func updateRequestedInstrumentationDelivery(
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) {
	originalDeliverySetting := operatorConfigurationResource.Spec.InstrumentWorkloads.InstrumentationDelivery
	previousResolvedDelivery, newResolvedDelivery :=
		clusterInstrumentationConfig.UpdateRequestedInstrumentationDelivery(originalDeliverySetting, true, logger)
	if previousResolvedDelivery != newResolvedDelivery {
		logger.Info(
			"The instrumentation delivery mechanism resolved from spec.instrumentWorkloads.instrumentationDelivery has "+
				"changed. The new setting will become effective for subsequent workload instrumentations. (Existing "+
				"instrumented workloads will not be re-instrumented.)",
			"instrumentation delivery setting in operator configuration", originalDeliverySetting,
			"previous resolved instrumentation delivery setting", previousResolvedDelivery,
			"updated resolved instrumentation delivery setting", newResolvedDelivery,
		)
	}
}
