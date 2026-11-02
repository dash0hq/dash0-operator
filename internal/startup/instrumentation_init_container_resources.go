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

// watchInstrumentationInitContainerResources keeps the init container resources from spec.components up to date on
// every replica, since the instrumentation webhook runs on all of them. The first read bypasses the unstarted cache.
func watchInstrumentationInitContainerResources(
	ctx context.Context,
	mgr ctrl.Manager,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	extraConfigDefaults util.ExtraConfig,
) error {
	if err := updateInstrumentationInitContainerResources(
		ctx, startupTasksK8sClient, clusterInstrumentationConfig, extraConfigDefaults, setupLog,
	); err != nil {
		setupLog.Error(err, "cannot read the init container resources at startup, they will be applied once the "+
			"cache has synced")
	}

	informer, err := mgr.GetCache().GetInformer(ctx, &dash0v1alpha1.Dash0OperatorConfiguration{})
	if err != nil {
		return fmt.Errorf("cannot get the informer for operator configuration resources: %w", err)
	}
	logger := logd.NewLogger(ctrl.Log.WithName("instrumentation-init-container-resources"))
	update := func() {
		if err := updateInstrumentationInitContainerResources(
			ctx, mgr.GetClient(), clusterInstrumentationConfig, extraConfigDefaults, logger,
		); err != nil {
			logger.Error(err, "cannot update the init container resources from the operator configuration resource")
		}
	}
	if _, err = informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { update() },
		UpdateFunc: func(any, any) { update() },
		DeleteFunc: func(any) { update() },
	}); err != nil {
		return fmt.Errorf("cannot watch operator configuration resources for init container resources: %w", err)
	}
	return nil
}

func updateInstrumentationInitContainerResources(
	ctx context.Context,
	k8sClient client.Client,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
	extraConfigDefaults util.ExtraConfig,
	logger logd.Logger,
) error {
	operatorConfigurationResource, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx, k8sClient, "", &dash0v1alpha1.Dash0OperatorConfiguration{}, logger)
	if err != nil {
		return err
	}
	var initContainerResources *corev1.ResourceRequirements
	if operatorConfigurationResource != nil {
		initContainerResources = util.EffectiveInstrumentationInitContainerResources(
			extraConfigDefaults,
			util.OperatorConfigurationComponentsOf(
				operatorConfigurationResource.(*dash0v1alpha1.Dash0OperatorConfiguration)),
		)
	}
	logger.Debug("Updating the init container resources from the operator configuration resource.",
		"resources", initContainerResources)
	clusterInstrumentationConfig.UpdateInstrumentationInitContainerResources(initContainerResources)
	return nil
}
