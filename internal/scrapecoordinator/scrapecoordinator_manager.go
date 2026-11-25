// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scrapecoordinator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/scrapecoordinator/scoresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

// ScrapeCoordinatorManager decides whether the scrape coordinator is deployed, and creates, updates or deletes its
// resources accordingly.
type ScrapeCoordinatorManager struct {
	client.Client
	resourceManager     *scoresources.ScrapeCoordinatorResourceManager
	extraConfig         atomic.Pointer[util.ExtraConfig]
	extraConfigDefaults util.ExtraConfig
	enabledViaHelm      bool
	developmentMode     bool
	reconcileGuard      util.ReconcileGuard
}

// ReconcileTrigger describes what triggered a reconciliation of the scrape coordinator resources.
type ReconcileTrigger string

const (
	TriggeredByWatchEvent             ReconcileTrigger = "watch"
	TriggeredByDash0ResourceReconcile ReconcileTrigger = "resource"
)

// NewScrapeCoordinatorManager creates a new ScrapeCoordinatorManager. The flag enabledViaHelm selects the scrape
// coordinator instead of the OpenTelemetry target-allocator as the implementation of prometheusCrdSupport.
func NewScrapeCoordinatorManager(
	k8sClient client.Client,
	extraConfig util.ExtraConfig,
	extraConfigDefaults util.ExtraConfig,
	enabledViaHelm bool,
	developmentMode bool,
	resourceManager *scoresources.ScrapeCoordinatorResourceManager,
) *ScrapeCoordinatorManager {
	m := &ScrapeCoordinatorManager{
		Client:              k8sClient,
		extraConfigDefaults: extraConfigDefaults,
		enabledViaHelm:      enabledViaHelm,
		developmentMode:     developmentMode,
		resourceManager:     resourceManager,
	}
	m.extraConfig.Store(&extraConfig)
	return m
}

// UpdateExtraConfig stores the new extra config and reconciles the scrape coordinator resources if it has changed.
func (m *ScrapeCoordinatorManager) UpdateExtraConfig(ctx context.Context, newConfig util.ExtraConfig, logger logd.Logger) {
	previousConfig := m.extraConfig.Swap(&newConfig)
	if previousConfig != nil && reflect.DeepEqual(*previousConfig, newConfig) {
		logger.Info("ignoring extra config map update, both the new and the old extra config map have the same content")
		return
	}
	hasBeenReconciled, err := m.ReconcileScrapeCoordinator(ctx, TriggeredByWatchEvent)
	if err != nil {
		logger.ErrorTelemetryCollectionIssue(err,
			"Failed to create/update scrape coordinator resources after extra config map update.")
	}
	if hasBeenReconciled {
		logger.Info("successfully reconciled scrape coordinator resources after extra config map update")
	}
}

// ReconcileScrapeCoordinator can be triggered by
//  1. a reconcile request from the Dash0OperatorConfiguration resource,
//  2. a reconcile request from a Dash0Monitoring resource in the cluster,
//  3. a change event on one of the scrape coordinator resources that the operator manages.
//
// It returns whether the reconciliation has been performed. A request that arrives while a reconciliation is in
// progress is served by the reconciliation in progress, see util.ReconcileGuard.
func (m *ScrapeCoordinatorManager) ReconcileScrapeCoordinator(
	ctx context.Context,
	trigger ReconcileTrigger,
) (bool, error) {
	logger := logd.FromContext(ctx)
	logger.Info("ReconcileScrapeCoordinator", "trigger", trigger)

	return m.reconcileGuard.Run(
		func() (bool, error) {
			return m.reconcileScrapeCoordinator(ctx, logger)
		},
		func() {
			if m.developmentMode {
				logger.Info("creation/update of the scrape coordinator resources is already in progress, the " +
					"additional reconciliation request will be served by the reconciliation which is in progress.")
			}
		},
		func() {
			logger.Warn("the reconciliation of the scrape coordinator resources kept being triggered while it was " +
				"running, stopped repeating it, the pending reconciliation request is dropped.")
		},
	)
}

func (m *ScrapeCoordinatorManager) reconcileScrapeCoordinator(ctx context.Context, logger logd.Logger) (bool, error) {
	operatorConfigurationResource, err := resources.FindOperatorConfigurationResource(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}
	allMonitoringResources, err := resources.FindAllMonitoringResources(ctx, m.Client, logger)
	if err != nil {
		return false, err
	}
	namespacesWithPrometheusScraping := make([]string, 0, len(allMonitoringResources))
	for _, monitoringResource := range allMonitoringResources {
		if pointers.ReadBoolPointerWithDefault(monitoringResource.Spec.PrometheusScraping.Enabled, true) {
			namespacesWithPrometheusScraping = append(namespacesWithPrometheusScraping, monitoringResource.Namespace)
		}
	}

	fileExtraConfig := m.extraConfig.Load()
	if fileExtraConfig == nil {
		return false, errors.New("extra config is nil in ScrapeCoordinatorManager#ReconcileScrapeCoordinator")
	}
	extraConfig := util.EffectiveExtraConfig(*fileExtraConfig, m.extraConfigDefaults, operatorConfigurationResource, nil)

	if reason := m.reasonForNotDeploying(operatorConfigurationResource, namespacesWithPrometheusScraping); reason != "" {
		logger.Info(fmt.Sprintf("%s No Dash0 scrape coordinator will be created, the existing Dash0 scrape coordinator "+
			"(if present) will be removed.", reason))
		err = m.removeScrapeCoordinator(ctx, extraConfig, logger)
		return err == nil, err
	}

	logNamespacesWithoutSecretAccess(
		namespacesWithPrometheusScraping,
		extraConfig.ScrapeCoordinatorAllowSecretAccessInNamespaces,
		logger,
	)
	logger.Info(fmt.Sprintf("Telemetry collection and support for Prometheus CRDs has been enabled via the operator "+
		"configuration resource (\"%s\"), the Dash0 scrape coordinator will be created or updated.",
		operatorConfigurationResource.Name))
	err = m.createOrUpdateScrapeCoordinator(
		ctx,
		operatorConfigurationResource,
		namespacesWithPrometheusScraping,
		extraConfig,
		logger,
	)
	return err == nil, err
}

// reasonForNotDeploying returns why the scrape coordinator must not be deployed, or an empty string if it is deployed.
func (m *ScrapeCoordinatorManager) reasonForNotDeploying(
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	namespacesWithPrometheusScraping []string,
) string {
	switch {
	case !m.enabledViaHelm:
		return "The scrape coordinator has not been selected via the Helm value operator.scrapeCoordinator.enabled."
	case operatorConfigurationResource == nil:
		return "The Dash0OperatorConfiguration resource is missing or has been deleted."
	case !pointers.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.TelemetryCollection.Enabled, true):
		return fmt.Sprintf("Telemetry collection has been disabled via the operator configuration resource (\"%s\"), "+
			"property telemetryCollection.enabled=false.", operatorConfigurationResource.Name)
	case !pointers.ReadBoolPointerWithDefault(operatorConfigurationResource.Spec.PrometheusCrdSupport.Enabled, false):
		return fmt.Sprintf("Support for Prometheus CRDs has been disabled via the operator configuration resource "+
			"(\"%s\"), property prometheusCrdSupport.enabled=false.", operatorConfigurationResource.Name)
	case len(namespacesWithPrometheusScraping) == 0:
		return fmt.Sprintf("Support for Prometheus CRDs has been enabled via the operator configuration resource "+
			"(\"%s\"), but not a single namespace has `prometheusScraping.enabled` via the Dash0Monitoring resource.",
			operatorConfigurationResource.Name)
	default:
		return ""
	}
}

func logNamespacesWithoutSecretAccess(
	namespacesWithPrometheusScraping []string,
	allowSecretAccessInNamespaces []string,
	logger logd.Logger,
) {
	if len(allowSecretAccessInNamespaces) == 0 {
		return
	}
	for _, namespace := range namespacesWithPrometheusScraping {
		if slices.Contains(allowSecretAccessInNamespaces, namespace) {
			continue
		}
		logger.Debug(fmt.Sprintf("The Dash0Monitoring resource in namespace \"%s\" has Prometheus scraping enabled, "+
			"but the namespace is not listed in the Helm value operator.scrapeCoordinator.allowSecretAccessInNamespaces. "+
			"The scrape coordinator cannot read secrets in this namespace. Prometheus CRDs (PodMonitor, ServiceMonitor, "+
			"ScrapeConfig) in this namespace that reference a secret will be rejected by the scrape coordinator.",
			namespace))
	}
}

func (m *ScrapeCoordinatorManager) createOrUpdateScrapeCoordinator(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	namespacesWithPrometheusScraping []string,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	created, updated, err := m.resourceManager.CreateOrUpdateScrapeCoordinatorResources(
		ctx,
		extraConfig,
		operatorConfigurationResource,
		namespacesWithPrometheusScraping,
		logger,
	)
	if err != nil {
		logger.ErrorTelemetryCollectionIssue(err, "failed to create one or more of the scrape coordinator resources, "+
			"support for Prometheus CRDs might not work")
		return err
	}
	switch {
	case created && updated:
		logger.Info("Scrape coordinator Kubernetes resources have been created and updated.")
	case created:
		logger.Info("Scrape coordinator Kubernetes resources have been created.")
	case updated:
		logger.Info("Scrape coordinator Kubernetes resources have been updated.")
	default:
		logger.Debug("Scrape coordinator Kubernetes resources are already up to date, no changes required")
	}
	return nil
}

func (m *ScrapeCoordinatorManager) removeScrapeCoordinator(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) error {
	deleted, err := m.resourceManager.DeleteResources(ctx, extraConfig, logger)
	if err != nil {
		logger.Error(err, "Failed to delete the scrape coordinator Kubernetes resources, requeuing reconcile request.")
		return err
	}
	if deleted {
		logger.Info("Scrape coordinator Kubernetes resources have been deleted.")
	} else {
		logger.Debug("no scrape coordinator Kubernetes resources to delete")
	}
	return nil
}
