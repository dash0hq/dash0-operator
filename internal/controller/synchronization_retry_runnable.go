// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	// defaultSynchronizationRetryInterval is the fallback interval at which the SynchronizationRetryRunnable inspects the
	// iac resources in the cluster for failed synchronization attempts and retries them.
	defaultSynchronizationRetryInterval = 10 * time.Minute
)

// SynchronizationRetryRunnable is a recurring, leader-only task that periodically inspects the synchronization results
// recorded in the status of IaC resources/Dash0 monitoring resources and retries synchronizing the associated
// resource for which the previous synchronization attempt failed due to a transient error (a server error or rate
// limiting). Synchronization failures that are caused by validation issues or other client errors are not retried, as
// retrying them would not change the outcome.
//
// It runs every interval (configurable via the Helm value operator.iac.periodicRetry.interval), starting one interval
// after this operator manager replica has become leader.
type SynchronizationRetryRunnable struct {
	k8sClient                    client.Client
	persesDashboardCrdReconciler *PersesDashboardCrdReconciler
	prometheusRuleCrdReconciler  *PrometheusRuleCrdReconciler
	ownedIacResourceController   []OwnedIacResourceSynchronizationController
	interval                     time.Duration
}

// OwnedIacResourceSynchronizationController is implemented by the reconcilers of resource types that are owned by the Dash0
// operator (notification channels, sampling rules, signal-to-metrics, spam filters, synthetic checks and views). It
// allows the SynchronizationRetryRunnable to find resources of that type whose last synchronization failed with a
// retryable error and to re-trigger their synchronization.
type OwnedIacResourceSynchronizationController interface {
	reconcile.Reconciler

	KindDisplayName() string

	// CreateReconcileRequestsForRetryableSyncErrors returns a reconcile request for every resource of this type that
	// currently has a retryable synchronization error recorded in its status.
	CreateReconcileRequestsForRetryableSyncErrors(ctx context.Context) ([]reconcile.Request, error)
}

func NewSynchronizationRetryRunnable(
	k8sClient client.Client,
	persesDashboardCrdReconciler *PersesDashboardCrdReconciler,
	prometheusRuleCrdReconciler *PrometheusRuleCrdReconciler,
	ownedIacResourceController []OwnedIacResourceSynchronizationController,
	interval time.Duration,
	logger logd.Logger,
) *SynchronizationRetryRunnable {
	if interval <= 0 {
		logger.Warn(fmt.Sprintf(
			"The synchronization retry runnable was created with a non-positive interval (%s); "+
				"falling back to the default interval of %s.",
			interval,
			defaultSynchronizationRetryInterval,
		))
		interval = defaultSynchronizationRetryInterval
	}
	return &SynchronizationRetryRunnable{
		k8sClient:                    k8sClient,
		persesDashboardCrdReconciler: persesDashboardCrdReconciler,
		prometheusRuleCrdReconciler:  prometheusRuleCrdReconciler,
		ownedIacResourceController:   ownedIacResourceController,
		interval:                     interval,
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates that the
// SynchronizationRetryRunnable requires leader election, that is, it is only started on the leader replica.
func (r *SynchronizationRetryRunnable) NeedLeaderElection() bool {
	return true
}

func (r *SynchronizationRetryRunnable) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.retryFailedSynchronizations(ctx)
		case <-ctx.Done():
			log.FromContext(ctx).Info("Stopping SynchronizationRetryRunnable")
			return nil
		}
	}
}

// retryFailedSynchronizations checks whether any IaC resource had retryable synchronization issues, and if so, triggers
// the synchronization for it again.
func (r *SynchronizationRetryRunnable) retryFailedSynchronizations(ctx context.Context) {
	r.retryFailedSynchronizationsForOwnedResources(ctx)
	r.retryFailedSynchronizationsForThirdPartyResources(ctx)
}

// retryFailedSynchronizationsForOwnedResources inspects all resource types that are owned by the Dash0 operator
// (notification channels, sampling rules, synthetic checks, views, ...) for resources whose last synchronization to the
// Dash0 API failed with a retryable error, and triggers the synchronization for each such resource again. In contrast
// to third-party resources, the synchronization results for owned resources are recorded in the status of the resource
// itself, so each owned resource reconciler knows how to find its own resources with retryable synchronization errors.
func (r *SynchronizationRetryRunnable) retryFailedSynchronizationsForOwnedResources(ctx context.Context) {
	logger := logd.FromContext(ctx)
	for _, controller := range r.ownedIacResourceController {
		requests, err := controller.CreateReconcileRequestsForRetryableSyncErrors(ctx)
		if err != nil {
			logger.Error(
				err,
				fmt.Sprintf("failed to list %s resources for synchronization retry", controller.KindDisplayName()),
			)
			continue
		}
		for _, request := range requests {
			logger.Info(fmt.Sprintf(
				"retrying the synchronization of the %s %s due to a previous retryable synchronization error",
				controller.KindDisplayName(),
				request.NamespacedName,
			))
			if _, err := controller.Reconcile(ctx, request); err != nil {
				logger.Error(err, fmt.Sprintf(
					"failed to retry the synchronization of the %s %s",
					controller.KindDisplayName(),
					request.NamespacedName,
				))
			}
		}
	}
}

// retryFailedSynchronizationsForThirdPartyResources lists all Dash0 monitoring resources and, for each one, inspects
// the recorded synchronization results for Prometheus rules and Perses dashboards. For every associated third-party
// resource that has at least one retryable synchronization error, the synchronization is triggered again.
func (r *SynchronizationRetryRunnable) retryFailedSynchronizationsForThirdPartyResources(ctx context.Context) {
	logger := logd.FromContext(ctx)

	// Note: We deliberately do not paginate this list (no Limit/Continue). The k8sClient is backed by the
	// controller-runtime cache, which holds all objects in memory and does not support pagination (it ignores Limit and
	// rejects a non-empty Continue token). There is also at most one monitoring resource per namespace, so the result
	// set is small.
	monitoringResources := &dash0v1beta1.Dash0MonitoringList{}
	if err := r.k8sClient.List(ctx, monitoringResources); err != nil {
		logger.Error(err, "failed to list Dash0 monitoring resources for synchronization retry")
		return
	}

	for i := range monitoringResources.Items {
		monitoringResource := &monitoringResources.Items[i]
		r.retryPrometheusRuleSynchronizations(ctx, monitoringResource, logger)
		r.retryPersesDashboardSynchronizations(ctx, monitoringResource, logger)
	}
}

func (r *SynchronizationRetryRunnable) retryPrometheusRuleSynchronizations(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) {
	if len(monitoringResource.Status.PrometheusRuleSynchronizationResults) == 0 {
		return
	}
	resourceReconciler := r.prometheusRuleCrdReconciler.ThirdPartyResourceReconciler()
	if !resourceReconciler.IsWatching() {
		// The PrometheusRule CRD is not present, no valid Dash0 API config is available, or this replica is not (yet)
		// the leader. In all these cases there is no point in retrying.
		return
	}
	crdVersion := r.prometheusRuleCrdReconciler.Version()
	if crdVersion == "" {
		return
	}

	for qualifiedName, result := range monitoringResource.Status.PrometheusRuleSynchronizationResults {
		if !prometheusRuleSynchronizationResultHasRetryableError(result) {
			continue
		}
		r.retryThirdPartyResource(
			ctx,
			resourceReconciler,
			schema.GroupVersionKind{
				Group:   r.prometheusRuleCrdReconciler.Group(),
				Version: crdVersion,
				Kind:    r.prometheusRuleCrdReconciler.Kind(),
			},
			qualifiedName,
			logger,
		)
	}
}

func (r *SynchronizationRetryRunnable) retryPersesDashboardSynchronizations(
	ctx context.Context,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) {
	if len(monitoringResource.Status.PersesDashboardSynchronizationResults) == 0 {
		return
	}
	resourceReconciler := r.persesDashboardCrdReconciler.ThirdPartyResourceReconciler()
	if !resourceReconciler.IsWatching() {
		// The PersesDashboard CRD is not present, no valid Dash0 API config is available, or this replica is not (yet)
		// the leader. In all these cases there is no point in retrying.
		return
	}
	crdVersion := r.persesDashboardCrdReconciler.Version()
	if crdVersion == "" {
		return
	}

	for qualifiedName, result := range monitoringResource.Status.PersesDashboardSynchronizationResults {
		if !persesDashboardSynchronizationResultHasRetryableError(result) {
			continue
		}
		r.retryThirdPartyResource(
			ctx,
			resourceReconciler,
			schema.GroupVersionKind{
				Group:   r.persesDashboardCrdReconciler.Group(),
				Version: crdVersion,
				Kind:    r.persesDashboardCrdReconciler.Kind(),
			},
			qualifiedName,
			logger,
		)
	}
}

// retryThirdPartyResource fetches the third-party resource identified by qualifiedName (in the form "namespace/name")
// and re-enqueues it for synchronization via the shared synchronization queue.
func (r *SynchronizationRetryRunnable) retryThirdPartyResource(
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	gvk schema.GroupVersionKind,
	qualifiedName string,
	logger logd.Logger,
) {
	namespace, name, ok := splitQualifiedName(qualifiedName)
	if !ok {
		logger.Warn(fmt.Sprintf(
			"cannot parse the qualified name %q of a %s with a failed synchronization, skipping retry",
			qualifiedName,
			resourceReconciler.KindDisplayName(),
		))
		return
	}

	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(gvk)
	if err := r.k8sClient.Get(
		ctx,
		types.NamespacedName{Namespace: namespace, Name: name},
		resource,
	); err != nil {
		// The resource may have been deleted in the meantime; nothing to retry then.
		logger.Info(fmt.Sprintf(
			"cannot fetch the %s %s for synchronization retry (it may have been deleted), skipping: %v",
			resourceReconciler.KindDisplayName(),
			qualifiedName,
			err,
		))
		return
	}

	logger.Info(fmt.Sprintf(
		"retrying the synchronization of the %s %s due to a previous retryable synchronization error",
		resourceReconciler.KindDisplayName(),
		qualifiedName,
	))
	upsertViaApi(resourceReconciler, resource)
}

// prometheusRuleSynchronizationResultHasRetryableError reports whether the given Prometheus rule synchronization result
// contains at least one synchronization error that should be retried.
func prometheusRuleSynchronizationResultHasRetryableError(
	result dash0common.PrometheusRuleSynchronizationResult,
) bool {
	for _, perEndpointAndDataset := range result.SynchronizationResults {
		for ruleName := range perEndpointAndDataset.SynchronizationErrors {
			statusCode := perEndpointAndDataset.SynchronizationErrorHttpStatusCodes[ruleName]
			if isRetryableHttpStatusCode(statusCode) {
				return true
			}
		}
	}
	return false
}

// persesDashboardSynchronizationResultHasRetryableError reports whether the given Perses dashboard synchronization
// result contains a synchronization error that should be retried.
func persesDashboardSynchronizationResultHasRetryableError(
	result dash0common.PersesDashboardSynchronizationResults,
) bool {
	for _, perEndpointAndDataset := range result.SynchronizationResults {
		if perEndpointAndDataset.SynchronizationError == "" {
			continue
		}
		if isRetryableHttpStatusCode(perEndpointAndDataset.HttpStatusCode) {
			return true
		}
	}
	return false
}

// isRetryableSynchronizationError reports whether a recorded synchronization error (a non-empty error message together
// with the HTTP status code the Dash0 API returned) should be retried. This is used for resource types owned by the
// operator, whose status records a single synchronization error message plus its HTTP status code per endpoint/dataset.
func isRetryableSynchronizationError(synchronizationError string, httpStatusCode int) bool {
	return synchronizationError != "" && isRetryableHttpStatusCode(httpStatusCode)
}

// isRetryableHttpStatusCode reports whether a synchronization failure with the given HTTP status code should be retried.
// A synchronization is retried if it failed due to a server error (5xx) or rate limiting (429), or due to a
// transport-level error where no HTTP response was received (status code 0, e.g. a network error or timeout). Client
// errors (4xx other than 429), which typically indicate a validation issue or another permanent problem, are not
// retried.
func isRetryableHttpStatusCode(statusCode int) bool {
	return statusCode == 0 ||
		statusCode == http.StatusTooManyRequests ||
		statusCode >= http.StatusInternalServerError
}

// splitQualifiedName splits a qualified name in the form "namespace/name" into its namespace and name parts. The
// boolean return value is false if the input is not a valid qualified name.
func splitQualifiedName(qualifiedName string) (string, string, bool) {
	namespace, name, found := strings.Cut(qualifiedName, "/")
	if !found || namespace == "" || name == "" {
		return "", "", false
	}
	return namespace, name, true
}
