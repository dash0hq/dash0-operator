// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	otelmetric "go.opentelemetry.io/otel/metric"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sretry "k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

type PersesDashboardCrdReconciler struct {
	client.Client
	apiReader                 client.Reader
	queue                     *ThirdPartyResourceSyncQueue
	leaderElectionAware       util.LeaderElectionAware
	mgr                       ctrl.Manager
	apiClientPool             *ApiClientPool
	skipNameValidation        bool
	persesDashboardReconciler *PersesDashboardReconciler
	persesDashboardCrdExists  atomic.Bool

	// crdVersion holds the perses.dev API version we are currently watching. It is populated by OnCrdChange based on which
	// versions the deployed CRD provides; clusters with an older CRD (up to version v0.2.0) only provide v1alpha1, while >= 0.3.0
	// provides both v1alpha1 and v1alpha2.
	crdVersion atomic.Pointer[string]

	conversionWebhookSettings PersesDashboardConversionWebhookSettings
	// uncachedClient is used to read the CRD again after a conflicting conversion webhook patch, and when the CA bundle
	// changes.
	uncachedClient client.Client
}

// PersesDashboardConversionWebhookSettings are the settings for auto-patching the PersesDashboard CRD's conversion webhook stanza
// when the Perses operator (which would normally own the conversion webhook) is not installed.
type PersesDashboardConversionWebhookSettings struct {
	AutoPatchConversionWebhook bool
	OperatorNamespace          string
	WebhookServiceName         string
	WebhookServicePort         int32
	// CaBundlePath is optional; defaults to the standard controller-runtime serving-certs mount.
	CaBundlePath string
	// CaBundleSource is optional; if set, the CA bundle is read from it instead of from CaBundlePath. It is set when the
	// operator manages the webhook certificates itself.
	CaBundleSource func() []byte
}

type PersesDashboardReconciler struct {
	client.Client
	// crdReconciler is a back-reference to the owning CRD reconciler so the inner reconciler can read the chosen API version (which
	// depends on which versions the cluster's CRD currently serves; see PersesDashboardCrdReconciler.OnCrdChange).
	crdReconciler              *PersesDashboardCrdReconciler
	apiReader                  client.Reader
	pseudoClusterUid           types.UID
	queue                      *ThirdPartyResourceSyncQueue
	apiClientPool              *ApiClientPool
	defaultApiConfigs          selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs       selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	namespacedSyncEnabled      sync.Map
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
}

const (
	// PersesDashboardCrdName is the name of the perses.dev PersesDashboard CRD.
	PersesDashboardCrdName = "persesdashboards.perses.dev"

	persesDashboardV1Alpha1         = "v1alpha1"
	persesDashboardV1Alpha2         = "v1alpha2"
	defaultWebhookServingCertCAPath = "/tmp/k8s-webhook-server/serving-certs/ca.crt"

	persesCaBundleUpdateTimeout = 30 * time.Second
)

var (
	persesDashboardCrdReconcileRequestMetric otelmetric.Int64Counter
	persesDashboardReconcileRequestMetric    otelmetric.Int64Counter

	// persesDashboardPreferredVersions is the version preference order — newer first. OnCrdChange picks the first version that the
	// CRD lists.
	//
	// We prefer v1alpha2 (the storage version as of Perses operator >= 0.3.0) when it is provided, because with a conversion webhook
	// in place it returns the canonical post-conversion shape. Clusters that still only have the older v1alpha1 CRD fall back to
	// v1alpha1.
	persesDashboardPreferredVersions = []string{persesDashboardV1Alpha2, persesDashboardV1Alpha1}
)

func NewPersesDashboardCrdReconciler(
	k8sClient client.Client,
	apiReader client.Reader,
	queue *ThirdPartyResourceSyncQueue,
	leaderElectionAware util.LeaderElectionAware,
	apiClientPool *ApiClientPool,
	conversionWebhookSettings PersesDashboardConversionWebhookSettings,
) *PersesDashboardCrdReconciler {
	if conversionWebhookSettings.CaBundlePath == "" {
		conversionWebhookSettings.CaBundlePath = defaultWebhookServingCertCAPath
	}
	return &PersesDashboardCrdReconciler{
		Client:                    k8sClient,
		apiReader:                 apiReader,
		queue:                     queue,
		leaderElectionAware:       leaderElectionAware,
		apiClientPool:             apiClientPool,
		conversionWebhookSettings: conversionWebhookSettings,
	}
}

func (r *PersesDashboardCrdReconciler) Manager() ctrl.Manager {
	return r.mgr
}

func (r *PersesDashboardCrdReconciler) KindDisplayName() string {
	return "Perses dashboard"
}

func (r *PersesDashboardCrdReconciler) Group() string {
	return "perses.dev"
}

func (r *PersesDashboardCrdReconciler) Kind() string {
	return "PersesDashboard"
}

// Version returns the perses.dev API version we are currently watching, as chosen by OnCrdChange based on the CRD's provided
// versions.
func (r *PersesDashboardCrdReconciler) Version() string {
	if v := r.crdVersion.Load(); v != nil {
		return *v
	}
	return ""
}

// OnCrdChange determines the perses.dev API version to watch and stores it in r.crdVersion. It is called when we detect the
// PersesDashboard CRD object at startup, on CRD Create events, and on CRD Update events. The chosen version can be read back via
// Version(). If the CRD does not provide any supported version, persesDashboardCrdExists is set to false so that
// maybeStartWatchingThirdPartyResources will skip starting the watch.
func (r *PersesDashboardCrdReconciler) OnCrdChange(crd *apiextensionsv1.CustomResourceDefinition, logger logd.Logger) {
	v, ok := chooseVersionFromCrd(crd, logger)
	if !ok {
		r.resetCrdVersion()
		return
	}
	r.recordCrdVersion(v)
}

// recordCrdVersion stores the chosen perses.dev API version and marks the PersesDashboard CRD as usable for watch purposes.
func (r *PersesDashboardCrdReconciler) recordCrdVersion(version string) {
	r.crdVersion.Store(&version)
	r.persesDashboardCrdExists.Store(true)
}

// resetCrdVersion removes the stored perses.dev API version and marks the PersesDashboard CRD as unusable for watch purposes.
func (r *PersesDashboardCrdReconciler) resetCrdVersion() {
	r.crdVersion.Store(new(""))
	r.persesDashboardCrdExists.Store(false)
}

// chooseVersionFromCrd returns the preferred served version from a PersesDashboard CRD. If none of the preferred versions
// are served, it logs an error listing the served versions and returns ("", false); callers are expected to refuse starting the
// watch in that case.
func chooseVersionFromCrd(crd *apiextensionsv1.CustomResourceDefinition, logger logd.Logger) (string, bool) {
	served := map[string]bool{}
	servedNames := make([]string, 0, len(crd.Spec.Versions))
	for _, v := range crd.Spec.Versions {
		if v.Served {
			served[v.Name] = true
			servedNames = append(servedNames, v.Name)
		}
	}
	for _, candidate := range persesDashboardPreferredVersions {
		if served[candidate] {
			return candidate, true
		}
	}
	logger.Error(
		fmt.Errorf("no supported perses.dev PersesDashboard CRD version is served"),
		"The perses.dev PersesDashboard CRD does not serve any of the supported versions; the operator will not "+
			"watch for PersesDashboard resources.",
		"servedVersions", servedNames,
		"supportedVersions", persesDashboardPreferredVersions,
	)
	return "", false
}

func (r *PersesDashboardCrdReconciler) QualifiedKind() string {
	return PersesDashboardCrdName
}

func (r *PersesDashboardCrdReconciler) ControllerName() string {
	return "dash0_perses_dashboard_crd_controller"
}

func (r *PersesDashboardCrdReconciler) DoesCrdExist() *atomic.Bool {
	return &r.persesDashboardCrdExists
}

func (r *PersesDashboardCrdReconciler) SetCrdExists(exists bool) {
	r.persesDashboardCrdExists.Store(exists)
}

func (r *PersesDashboardCrdReconciler) SkipNameValidation() bool {
	return r.skipNameValidation
}

func (r *PersesDashboardCrdReconciler) WantsCrdUpdateEvents() bool {
	// Listen to update events, for two reasons:
	//   - When autoPatchConversionWebhook is enabled, we re-apply our spec.conversion patch, for example after a GitOps system's
	//     reconcile removes it. This is logged as a warning so the back and forth patching is observable.
	//   - Independent of auto-patch, we react to changes in the CRD's version set (e.g. when a cluster upgrades from a CRD
	//     manifest without v1alpha2 to a CRD manifest with v1alpha2), by stopping the watch and restarting the watch on the new
	//     CRD version.
	return true
}

func (r *PersesDashboardCrdReconciler) OperatorManagerIsLeader() bool {
	return r.leaderElectionAware.IsLeader()
}

func (r *PersesDashboardCrdReconciler) CreateThirdPartyResourceReconciler(pseudoClusterUid types.UID) {
	r.persesDashboardReconciler = &PersesDashboardReconciler{
		Client:               r.Client,
		apiReader:            r.apiReader,
		crdReconciler:        r,
		queue:                r.queue,
		pseudoClusterUid:     pseudoClusterUid,
		apiClientPool:        r.apiClientPool,
		defaultApiConfigs:    *selfmonitoringapiaccess.NewSynchronizedSlice[ApiConfig](),
		namespacedApiConfigs: *selfmonitoringapiaccess.NewSynchronizedMapSlice[ApiConfig](),
	}
}

func (r *PersesDashboardCrdReconciler) ThirdPartyResourceReconciler() ThirdPartyResourceReconciler {
	return r.persesDashboardReconciler
}

func (r *PersesDashboardCrdReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	startupK8sClient client.Client,
	logger logd.Logger,
) error {
	r.mgr = mgr
	r.uncachedClient = startupK8sClient
	if err := SetupThirdPartyCrdReconcilerWithManager(
		ctx,
		startupK8sClient,
		r,
		logger,
	); err != nil {
		return err
	}

	// SetupThirdPartyCrdReconcilerWithManager handles a pre-existing CRD by setting persesDashboardCrdExists and starting the
	// watch. We also need to patch the conversion webhook if needed, so the conversion webhook is in place right away.
	if r.persesDashboardCrdExists.Load() {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := startupK8sClient.Get(ctx, client.ObjectKey{Name: r.QualifiedKind()}, crd); err != nil {
			logger.Error(
				err,
				"Cannot read the perses.dev PersesDashboard CRD at startup to ensure its conversion webhook is configured; v1alpha1 "+
					"PersesDashboard resources may lose fields when being deployed.",
			)
		} else {
			r.ensurePersesConversionWebhookConfigured(ctx, crd, false, logger)
		}
	}

	return nil
}

func (r *PersesDashboardCrdReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardCrdReconcileRequestMetric != nil {
		persesDashboardCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := logd.FromContext(ctx)
	r.persesDashboardCrdExists.Store(true)
	if crd, ok := e.Object.(*apiextensionsv1.CustomResourceDefinition); ok {
		r.OnCrdChange(crd, logger)
		r.ensurePersesConversionWebhookConfigured(ctx, crd, false, logger)
	}
	maybeStartWatchingThirdPartyResources(r, logger)
}

func (r *PersesDashboardCrdReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardCrdReconcileRequestMetric != nil {
		persesDashboardCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := logd.FromContext(ctx)
	if crd, ok := e.ObjectNew.(*apiextensionsv1.CustomResourceDefinition); ok {
		r.handleVersionChangeOnUpdate(ctx, crd, logger)
		r.ensurePersesConversionWebhookConfigured(ctx, crd, true, logger)
	}
}

// handleVersionChangeOnUpdate compares the currently watched perses.dev API version against the version implied by the CRD object
// we just observed. If they differ AND we have a running watch, the watch is stopped on the old version, crdVersion is swapped,
// and a new watch is started.
//
// If no watch is running yet, we simply update crdVersion — the next call to maybeStartWatchingThirdPartyResources will pick it
// up.
//
// If the updated CRD does not provide any supported version, the watch (if running) is stopped and persesDashboardCrdExists is
// set to false so that any subsequent calls to maybeStartWatchingThirdPartyResources are no-ops.
func (r *PersesDashboardCrdReconciler) handleVersionChangeOnUpdate(
	ctx context.Context,
	crd *apiextensionsv1.CustomResourceDefinition,
	logger logd.Logger,
) {
	previouslyHadCrd := r.persesDashboardCrdExists.Load()
	previousVersion := r.Version()

	newVersion, ok := chooseVersionFromCrd(crd, logger)
	if !ok {
		// The CRD no longer provides a supported version. Stop the watch if running and mark the CRD as not usable so future
		// maybeStartWatchingThirdPartyResources calls bail out.
		resourceReconciler := r.persesDashboardReconciler
		if previouslyHadCrd && resourceReconciler != nil && resourceReconciler.IsWatching() {
			logger.Info(
				"The perses.dev PersesDashboard CRD no longer provides a supported version; stopping the watch for " +
					"PersesDashboard resources.",
			)
			stopWatchingThirdPartyResources(ctx, r, logger)
		}
		r.resetCrdVersion()
		return
	}

	if previouslyHadCrd && previousVersion == newVersion {
		// No change, nothing to do.
		return
	}

	resourceReconciler := r.persesDashboardReconciler
	if resourceReconciler == nil || !resourceReconciler.IsWatching() {
		// Not yet watching — record the new state and check if we can start watching now.
		r.recordCrdVersion(newVersion)
		if !previouslyHadCrd {
			maybeStartWatchingThirdPartyResources(r, logger)
		}
		return
	}

	// Stop and restart the watch to reflect the CRD version change. The sequence of calls here matters:
	// stopWatchingThirdPartyResources reads crdVersion (via createUnstructuredGvk) to identify which informer to remove, so
	// swapping the stored version needs to happen after stopWatchingThirdPartyResources, and before
	// maybeStartWatchingThirdPartyResources.
	logger.Info(fmt.Sprintf(
		"Detected change in served versions of the perses.dev PersesDashboard CRD; restarting watch "+
			"(was watching %q, switching to %q).",
		previousVersion, newVersion,
	))
	// Stop first — this reads chosenVersion to compute the GVK for the informer we want to remove.
	stopWatchingThirdPartyResources(ctx, r, logger)
	// Now write the new version and restart the watch.
	r.recordCrdVersion(newVersion)
	maybeStartWatchingThirdPartyResources(r, logger)
}

func (r *PersesDashboardCrdReconciler) Delete(
	ctx context.Context,
	_ event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardCrdReconcileRequestMetric != nil {
		persesDashboardCrdReconcileRequestMetric.Add(ctx, 1)
	}
	logger := logd.FromContext(ctx)
	logger.Info("The PersesDashboard custom resource definition has been deleted.")
	r.persesDashboardCrdExists.Store(false)

	stopWatchingThirdPartyResources(ctx, r, logger)
}

func (r *PersesDashboardCrdReconciler) Generic(
	context.Context,
	event.TypedGenericEvent[client.Object],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Should not be called, we are not interested in generic events.
}

func (r *PersesDashboardCrdReconciler) Reconcile(
	_ context.Context,
	_ reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called for the PersesDashboardCrdReconciler CRD, as we are using the
	// TypedEventHandler interface directly when setting up the watch. We still need to implement the method, as the
	// controller builder's Complete method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

func (r *PersesDashboardCrdReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "persesdashboardcrd.reconcile_requests")
	var err error
	if persesDashboardCrdReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for persesdashboard CRD reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}

	r.persesDashboardReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		logger,
	)
}

func (r *PersesDashboardCrdReconciler) SetDefaultApiConfigs(
	ctx context.Context,
	apiConfigs []ApiConfig,
	logger logd.Logger,
) {
	r.persesDashboardReconciler.defaultApiConfigs.Set(apiConfigs)
	if len(filterValidApiConfigs(apiConfigs, logger, "default operator configuration")) > 0 {
		maybeStartWatchingThirdPartyResources(r, logger)
	} else {
		stopWatchingThirdPartyResources(ctx, r, logger)
	}
}

func (r *PersesDashboardCrdReconciler) RemoveDefaultApiConfigs(ctx context.Context, logger logd.Logger) {
	r.persesDashboardReconciler.defaultApiConfigs.Clear()
	stopWatchingThirdPartyResources(ctx, r, logger)
}

func (r *PersesDashboardCrdReconciler) SetNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	updatedApiConfigs []ApiConfig,
	logger logd.Logger,
) {
	if len(updatedApiConfigs) > 0 {
		previousApiConfigs, _ := r.persesDashboardReconciler.namespacedApiConfigs.Get(namespace)

		r.persesDashboardReconciler.namespacedApiConfigs.Set(namespace, updatedApiConfigs)

		if !slices.Equal(previousApiConfigs, updatedApiConfigs) {
			r.persesDashboardReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
		}
	}
}

func (r *PersesDashboardCrdReconciler) RemoveNamespacedApiConfigs(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	if _, exists := r.persesDashboardReconciler.namespacedApiConfigs.Get(namespace); exists {
		r.persesDashboardReconciler.namespacedApiConfigs.Delete(namespace)
		r.persesDashboardReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *PersesDashboardCrdReconciler) SetSynchronizationEnabled(
	ctx context.Context,
	namespace string,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	logger logd.Logger,
) {
	enabled := r.persesDashboardReconciler.IsSynchronizationEnabled(monitoringResource)
	previous, hadPrevious := r.persesDashboardReconciler.namespacedSyncEnabled.Swap(namespace, enabled)
	if prev, ok := previous.(bool); hadPrevious && ok && !prev && enabled {
		logger.Info(fmt.Sprintf(
			"Synchronization of dashboards has been enabled for namespace %s, triggering resync.",
			namespace,
		))
		r.persesDashboardReconciler.synchronizeNamespacedResources(ctx, namespace, logger)
	}
}

func (r *PersesDashboardCrdReconciler) RemoveSynchronizationEnabled(namespace string) {
	r.persesDashboardReconciler.namespacedSyncEnabled.Delete(namespace)
}

func (r *PersesDashboardReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "persesdashboard.reconcile_requests")
	var err error
	if persesDashboardReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for perses dashboard reconcile requests"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (r *PersesDashboardReconciler) KindDisplayName() string {
	return "Perses dashboard"
}

func (r *PersesDashboardReconciler) ShortName() string {
	return "dashboard"
}

func (r *PersesDashboardReconciler) ControllerStopFunctionLock() *sync.Mutex {
	return &r.controllerStopFunctionLock
}

func (r *PersesDashboardReconciler) GetControllerStopFunction() *context.CancelFunc {
	return r.controllerStopFunction
}

func (r *PersesDashboardReconciler) SetControllerStopFunction(controllerStopFunction *context.CancelFunc) {
	r.controllerStopFunction = controllerStopFunction
}

func (r *PersesDashboardReconciler) IsWatching() bool {
	return r.controllerStopFunction != nil
}

func (r *PersesDashboardReconciler) GetDefaultApiConfigs() []ApiConfig {
	return r.defaultApiConfigs.Get()
}

func (r *PersesDashboardReconciler) GetNamespacedApiConfigs(namespace string) ([]ApiConfig, bool) {
	return r.namespacedApiConfigs.Get(namespace)
}

func (r *PersesDashboardReconciler) ControllerName() string {
	return "dash0_perses_dashboard_controller"
}

func (r *PersesDashboardReconciler) K8sClient() client.Client {
	return r.Client
}

func (r *PersesDashboardReconciler) Queue() *ThirdPartyResourceSyncQueue {
	return r.queue
}

func (r *PersesDashboardReconciler) ApiReader() client.Reader {
	return r.apiReader
}

func (r *PersesDashboardReconciler) IsSynchronizationEnabled(monitoringResource *dash0v1beta1.Dash0Monitoring) bool {
	if monitoringResource == nil {
		return false
	}
	boolPtr := monitoringResource.Spec.SynchronizePersesDashboards
	if boolPtr == nil {
		return true
	}
	return *boolPtr
}

func (r *PersesDashboardReconciler) Create(
	ctx context.Context,
	e event.TypedCreateEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info(
		"Detected a new Perses dashboard resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	upsertViaApi(r, e.Object)
}

func (r *PersesDashboardReconciler) Update(
	ctx context.Context,
	e event.TypedUpdateEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	logger := logd.FromContext(ctx)
	if hasUnchangedResourceVersion(e) {
		logger.Debug(
			"Ignoring an update event for an unchanged Perses dashboard resource",
			"namespace",
			e.ObjectNew.GetNamespace(),
			"name",
			e.ObjectNew.GetName(),
			"resourceVersion",
			e.ObjectNew.GetResourceVersion(),
		)
		return
	}

	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger.Info(
		"Detected a change for a Perses dashboard resource",
		"namespace",
		e.ObjectNew.GetNamespace(),
		"name",
		e.ObjectNew.GetName(),
	)

	upsertViaApi(r, e.ObjectNew)
}

func (r *PersesDashboardReconciler) Delete(
	ctx context.Context,
	e event.TypedDeleteEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info(
		"Detected the deletion of a Perses dashboard resource",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	deleteViaApi(r, e.Object)
}

func (r *PersesDashboardReconciler) Generic(
	context.Context,
	event.TypedGenericEvent[*unstructured.Unstructured],
	workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	// Should not be called, the watch is backed by source.Kind, which never emits generic events.
}

func (r *PersesDashboardReconciler) resyncViaApi(
	ctx context.Context,
	dashboardResource *unstructured.Unstructured,
	originsInNamespace *existingOriginsInNamespace,
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info(
		"Reconciling dashboard triggered by config event (updated API config or authorization).",
		"namespace",
		dashboardResource.GetNamespace(),
		"name",
		dashboardResource.GetName(),
	)

	upsertViaApiWithOriginsInNamespace(r, dashboardResource, originsInNamespace)
}

func (r *PersesDashboardReconciler) Reconcile(
	context.Context,
	reconcile.Request,
) (reconcile.Result, error) {
	// Reconcile should not be called on the PersesDashboardReconciler, as we are using the TypedEventHandler interface
	// directly when setting up the watch. We still need to implement the method, as the controller builder's Complete
	// method requires implementing the Reconciler interface.
	return reconcile.Result{}, nil
}

func (r *PersesDashboardReconciler) MapsToMultipleApiObjects() bool {
	return false
}

func (r *PersesDashboardReconciler) FetchExistingResourceOrigins(
	_ context.Context,
	_ *preconditionValidationResult,
	_ ApiConfig,
	_ logd.Logger,
) ([]string, error) {
	// The mechanism to delete individual dashboards when synchronizing one Kubernetes PersesDashboard resource is not
	// required, since each PersesDashboard only contains one dashboard. It is only needed when the resource type holds
	// multiple objects that are synchronized (as it is the case for PrometheusRule). Thus, this controller does not
	// need to implement this method.
	return nil, nil
}

func (r *PersesDashboardReconciler) FetchExistingNamespaceOrigins(
	ctx context.Context,
	namespace string,
	apiConfig ApiConfig,
	logger logd.Logger,
) ([]string, error) {
	// The Dash0 API does not support filtering the list of dashboards by origin, so this fetches the origins of all
	// dashboards in the dataset. IsOriginOfResource is used to pick the origins that belong to a particular resource.
	dataset := apiConfig.Dataset
	actionLabel := fmt.Sprintf(
		"fetch existing origins: %s %sapi/dashboards?dataset=%s",
		http.MethodGet,
		apiConfig.Endpoint,
		url.QueryEscape(dataset),
	)
	logger.Info(fmt.Sprintf("executing HTTP request to %s", actionLabel))
	apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
	if err != nil {
		err = convertApiClientError(err, actionLabel, false)
		logger.Error(err, "cannot fetch existing origins", "namespace", namespace)
		return nil, err
	}
	dashboards, err := apiClient.ListDashboards(ctx, &dataset)
	if err != nil {
		err = convertApiClientError(err, actionLabel, false)
		logger.Error(err, "cannot fetch existing origins", "namespace", namespace)
		return nil, err
	}
	var origins []string
	for _, dashboard := range dashboards {
		if dashboard != nil && dashboard.Origin != nil && *dashboard.Origin != "" {
			origins = append(origins, *dashboard.Origin)
		}
	}
	return origins, nil
}

func (r *PersesDashboardReconciler) IsOriginOfResource(
	origin string,
	preconditionValidationResult *preconditionValidationResult,
	apiConfig ApiConfig,
) bool {
	_, dashboardOrigin := r.renderDashboardUrl(preconditionValidationResult, apiConfig.Endpoint, apiConfig.Dataset)
	return origin == dashboardOrigin
}

func (r *PersesDashboardReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	dashboardUrl, dashboardOrigin := r.renderDashboardUrl(preconditionChecksResult, apiConfig.Endpoint, apiConfig.Dataset)

	dataset := apiConfig.Dataset

	var apiClientCall *ApiClientCall
	switch action {
	case upsertAction:
		dashboard := preconditionChecksResult.resource
		specOrConfig := r.normalizeV1Alpha1V1Alpha2(dashboard)
		displayRaw := specOrConfig["display"]
		displayRaw = r.addDisplaySectionIfMissing(displayRaw, specOrConfig)
		display, ok := displayRaw.(map[string]any)
		if !ok {
			logger.Warn("Perses dashboard spec.display is not a map, the dashboard will not be updated in Dash0.")
			return NewResourceToRequestsResultSingleItemValidationIssue(apiConfig, itemName, "spec.display is not a map")
		}
		r.setDisplayNameIfMissing(preconditionChecksResult, display)

		dashboardDefinition, err := mapToDashboardDefinition(dashboard)
		if err != nil {
			conversionErr := fmt.Errorf("unable to convert the dashboard to the Dash0 API format: %w", err)
			logger.Error(conversionErr, "error converting dashboard")
			return NewResourceToRequestsResultSingleItemConversionError(apiConfig, itemName, conversionErr.Error())
		}
		apiClientCall = &ApiClientCall{
			Method: http.MethodPut,
			Url:    dashboardUrl,
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				updatedDashboard, err := apiClient.UpdateDashboard(ctx, dashboardOrigin, dashboardDefinition, &dataset)
				if err != nil {
					return "", err
				}
				if updatedDashboard == nil {
					return "", errors.New("unexpected nil/empty response body")
				}
				return dash0apiclient.GetDashboardID(updatedDashboard), nil
			},
		}
	case deleteAction:
		apiClientCall = &ApiClientCall{
			Method: http.MethodDelete,
			Url:    dashboardUrl,
			Execute: func(ctx context.Context) (string, error) {
				apiClient, err := r.apiClientPool.Get(apiConfig.Endpoint, apiConfig.Token)
				if err != nil {
					return "", err
				}
				return "", apiClient.DeleteDashboard(ctx, dashboardOrigin, &dataset)
			},
		}
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	return NewResourceToRequestsResultSingleItemApiClientCall(apiConfig, apiClientCall, itemName, dashboardOrigin)
}

// mapToDashboardDefinition converts the normalized Perses dashboard resource (spec.config already unwrapped, display
// name already set) to the typed dashboard definition of the Dash0 API client. The resource is decoded directly instead
// of using dash0apiclient.ConvertPersesDashboardToDashboard, since that would replace kind with "Dashboard" and
// metadata.name with the display name (which is often not a valid Kubernetes name). Metadata that the Dash0 API does
// not define for dashboards (for example arbitrary annotations, metadata.namespace) is dropped.
func mapToDashboardDefinition(dashboard map[string]any) (*dash0apiclient.DashboardDefinition, error) {
	serializedDashboard, err := json.Marshal(dashboard)
	if err != nil {
		return nil, err
	}
	dashboardDefinition := &dash0apiclient.DashboardDefinition{}
	if err = json.Unmarshal(serializedDashboard, dashboardDefinition); err != nil {
		return nil, err
	}
	dash0apiclient.StripDashboardServerFields(dashboardDefinition)
	return dashboardDefinition, nil
}

func (r *PersesDashboardReconciler) normalizeV1Alpha1V1Alpha2(dashboard map[string]any) map[string]any {
	specOrConfig := (dashboard["spec"]).(map[string]any)
	configRaw := specOrConfig["config"]
	if configRaw != nil {
		// See https://github.com/perses/perses-operator/pull/128, the CRD spec has been changed, a new wrapper
		// object "config" has been added around the dashboard spec. This has later been reverted for version
		// v1alpha1 and added as a new CRD version v1alpha2, see
		// https://github.com/perses/perses-operator/blob/main/api/v1alpha2.
		if config, ok := configRaw.(map[string]any); ok {
			specOrConfig = config
			dashboard["spec"] = specOrConfig
		}
	}
	return specOrConfig
}

func (r *PersesDashboardReconciler) addDisplaySectionIfMissing(
	displayRaw any,
	specOrConfig map[string]any,
) any {
	if displayRaw == nil {
		specOrConfig["display"] = map[string]any{}
		displayRaw = specOrConfig["display"]
	}
	return displayRaw
}

func (r *PersesDashboardReconciler) setDisplayNameIfMissing(
	preconditionChecksResult *preconditionValidationResult,
	display map[string]any,
) {
	displayName, ok := display["name"]
	if !ok || displayName == "" {
		// Let the dashboard name default to the perses dashboard resource's namespace + name, if unset.
		display["name"] = fmt.Sprintf("%s/%s", preconditionChecksResult.k8sNamespace, preconditionChecksResult.k8sName)
	}
}

func (r *PersesDashboardReconciler) renderDashboardUrl(
	preconditionChecksResult *preconditionValidationResult,
	endpoint string,
	dataset string,
) (string, string) {
	dashboardOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetInOrigin(dataset),
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/dashboards/%s?dataset=%s",
		endpoint,
		url.PathEscape(dashboardOrigin),
		url.QueryEscape(dataset),
	), dashboardOrigin
}

func (r *PersesDashboardReconciler) CreateDeleteRequests(
	_ ApiConfig,
	_ []string,
	_ []string,
	_ logd.Logger,
) ([]WrappedApiRequest, map[string]string) {
	// The mechanism to delete individual dashboards when synchronizing one Kubernetes PersesDashboard resource is not
	// required, since each PersesDashboard only contains one dashboard. It is only needed when the resource type holds
	// multiple objects that are synchronized (as it is the case for PrometheusRule). Thus, this controller does not
	// need to implement this method.
	return nil, nil
}

func (*PersesDashboardReconciler) UpdateSynchronizationResultsInDash0MonitoringStatus(
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	qualifiedName string,
	status dash0common.ThirdPartySynchronizationStatus,
	syncResults synchronizationResults,
) any {
	previousResults := monitoringResource.Status.PersesDashboardSynchronizationResults
	if previousResults == nil {
		previousResults = make(map[string]dash0common.PersesDashboardSynchronizationResults)
		monitoringResource.Status.PersesDashboardSynchronizationResults = previousResults
	}

	persesDashboardSyncResults := make([]dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset, 0,
		len(syncResults.resultsPerApiConfig))
	for _, syncResult := range syncResults.resultsPerApiConfig {
		var persesSyncResult dash0common.PersesDashboardSynchronizationResultPerEndpointAndDataset
		persesSyncResult.Dash0ApiEndpoint = syncResult.apiConfig.Endpoint
		persesSyncResult.Dash0Dataset = syncResult.apiConfig.Dataset

		if len(syncResult.resourceToRequestsResult.SynchronizationErrors) > 0 {
			// there can only be at most one synchronization error for a Perses dashboard resource
			errorItemName := slices.Collect(maps.Keys(syncResult.resourceToRequestsResult.SynchronizationErrors))[0]
			persesSyncResult.SynchronizationError = syncResult.resourceToRequestsResult.SynchronizationErrors[errorItemName]
			persesSyncResult.HttpStatusCode = syncResult.resourceToRequestsResult.SynchronizationErrorStatusCodes[errorItemName]
		}
		if len(syncResult.successfullySynchronized) > 0 {
			// there can only be at most one successfullySynchronized object for a Perses dashboard resource
			apiObjectLabels := syncResult.successfullySynchronized[0].Labels
			persesSyncResult.Dash0Origin = apiObjectLabels.Origin
		}

		persesDashboardSyncResults = append(persesDashboardSyncResults, persesSyncResult)
	}

	// A Perses dashboard resource can only contain one dashboard, so its SynchronizationResults struct is considerably
	// simpler than the PrometheusRuleSynchronizationResults struct.
	result := dash0common.PersesDashboardSynchronizationResults{
		SynchronizedAt:         metav1.Time{Time: time.Now()},
		SynchronizationStatus:  status,
		SynchronizationResults: persesDashboardSyncResults,
	}

	if len(syncResults.validationIssues) > 0 {
		// there can only be at most one list of validation issues for a Perses dashboard resource
		result.ValidationIssues = slices.Collect(maps.Values(syncResults.validationIssues))[0]
	}

	previousResults[qualifiedName] = result
	return result
}

// synchronizeNamespacedResources explicitly triggers a resync of dashboards in a given namespace in response to an
// updated API endpoint, dataset or auth token, or when synchronization has been enabled for the namespace.
func (r *PersesDashboardReconciler) synchronizeNamespacedResources(
	ctx context.Context,
	namespace string,
	logger logd.Logger,
) {
	// do nothing if we are not currently watching the CRDs
	if !r.IsWatching() {
		return
	}

	logger.Info(fmt.Sprintf("Running synchronization of dashboards in namespace %s now.", namespace))

	go func() {
		allDashboardResourcesInNamespace := &unstructured.UnstructuredList{}
		allDashboardResourcesInNamespace.SetGroupVersionKind(
			schema.GroupVersionKind{
				Group:   "perses.dev",
				Version: r.crdReconciler.Version(),
				Kind:    "PersesDashboardList",
			},
		)
		if err := r.List(
			ctx,
			allDashboardResourcesInNamespace,
			&client.ListOptions{
				Namespace: namespace,
			},
		); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to list dashboard resources in namespace %s.", namespace))
			return
		}

		originsInNamespace := newExistingOriginsInNamespace(namespace)
		for i := range allDashboardResourcesInNamespace.Items {
			r.resyncViaApi(ctx, &allDashboardResourcesInNamespace.Items[i], originsInNamespace)

			// stagger API requests a bit
			time.Sleep(50 * time.Millisecond)
		}
		logger.Info(fmt.Sprintf("Triggering synchronization of dashboards in namespace %s has finished.", namespace))
	}()
}

// ensurePersesConversionWebhookConfigured patches the perses.dev PersesDashboard CRD to point its conversion stanza at the
// operator manager's webhook endpoint, but only if no conversion webhook is already configured. The CRD has two versions
// (v1alpha1, v1alpha2) with v1alpha2 as the storage version, and without a working conversion webhook the API server falls back
// to strategy "None", which prunes v1alpha1 fields at write time (since v1alpha2 wraps them under spec.config). Some users
// install only a stand-alone Perses CRD without the Perses operator (which would normally ship the conversion webhook), so this
// patch is the only thing keeping v1alpha1 dashboards from silently losing data.
//
// onUpdate distinguishes "first observation" (CRD created event) from "subsequent update" (CRD reconciled by GitOps or kubectl
// apply). Re-patching during an update is logged as a warning so a patch-war between us and an external reconciler is observable.
func (r *PersesDashboardCrdReconciler) ensurePersesConversionWebhookConfigured(
	ctx context.Context,
	crd *apiextensionsv1.CustomResourceDefinition,
	onUpdate bool,
	logger logd.Logger,
) {
	if !r.conversionWebhookSettings.AutoPatchConversionWebhook {
		logger.Info("PersesDashboard CRD conversion webhook auto-patch is disabled via " +
			"operator.iac.persesDashboard.autoPatchConversionWebhook=false.")
		return
	}

	logger.Info("Checking whether the PersesDashboard CRD conversion webhook needs patching (auto-patch is enabled, " +
		"operator.iac.persesDashboard.autoPatchConversionWebhook=true).")

	stanzaMatches := false
	firstAttempt := true
	err := k8sretry.RetryOnConflict(k8sretry.DefaultRetry, func() error {
		if !firstAttempt {
			// The CRD has been changed since it has been read, for example by another replica of the operator manager.
			crd = &apiextensionsv1.CustomResourceDefinition{}
			if err := r.crdReader().Get(ctx, client.ObjectKey{Name: r.QualifiedKind()}, crd); err != nil {
				return err
			}
		}
		firstAttempt = false
		var err error
		stanzaMatches, err = r.patchConversionWebhookIfNecessary(ctx, crd, onUpdate, logger)
		return err
	})
	if err == nil {
		return
	}
	if stanzaMatches {
		logger.Error(
			err,
			"Failed to update the CA bundle of the Dash0-operator conversion webhook on the perses.dev "+
				"PersesDashboard CRD. Requests that require a conversion of PersesDashboard resources may fail with a "+
				"certificate error.",
		)
	} else {
		logger.Error(
			err,
			"Failed to patch the perses.dev PersesDashboard CRD with the Dash0-operator conversion webhook "+
				"configuration. v1alpha1 PersesDashboard resources will lose fields when stored.",
		)
	}
}

// OnCaBundleChange updates the CA bundle of the conversion webhook stanza after the operator-managed webhook CA bundle
// has changed. It reads the CRD without the cache and runs on every replica, independent of leader election.
func (r *PersesDashboardCrdReconciler) OnCaBundleChange(ctx context.Context, logger logd.Logger) {
	if !r.conversionWebhookSettings.AutoPatchConversionWebhook {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, persesCaBundleUpdateTimeout)
	defer cancel()
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.crdReader().Get(ctx, client.ObjectKey{Name: r.QualifiedKind()}, crd); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(
				err,
				"Cannot read the perses.dev PersesDashboard CRD to update the CA bundle of its conversion webhook.",
			)
		}
		return
	}
	r.ensurePersesConversionWebhookConfigured(ctx, crd, false, logger)
}

func (r *PersesDashboardCrdReconciler) crdReader() client.Reader {
	if r.uncachedClient != nil {
		return r.uncachedClient
	}
	return r.Client
}

// patchConversionWebhookIfNecessary patches the conversion stanza unless it is up to date or a foreign conversion webhook
// is configured. The patch carries the resourceVersion of the given CRD, so it fails with a conflict if the CRD has been
// changed in the meantime. The returned bool reports whether the stanza already pointed at the Dash0 operator.
func (r *PersesDashboardCrdReconciler) patchConversionWebhookIfNecessary(
	ctx context.Context,
	crd *apiextensionsv1.CustomResourceDefinition,
	onUpdate bool,
	logger logd.Logger,
) (bool, error) {
	// stanzaMatches tells us whether the conversion stanza already points at our webhook service. It intentionally
	// ignores the caBundle, so we also need caBundleMatches to detect a rotated CA on an otherwise-unchanged stanza
	// (see below).
	stanzaMatches := r.conversionStanzaMatchesOurs(crd)

	// Only defer to a foreign conversion webhook when the stanza is not ours to begin with. If stanzaMatches is true we
	// own the stanza and are merely refreshing a rotated caBundle, so the foreign-webhook check does not apply.
	if !stanzaMatches && hasForeignConversionWebhook(crd) {
		if !onUpdate {
			logger.Info(
				"Detected pre-existing conversion webhook on the perses.dev PersesDashboard CRD " +
					"(not pointing at the Dash0 operator); leaving the CRD untouched.",
			)
		}
		return stanzaMatches, nil
	}

	// The CA bundle might need changes independently of the conversion webhook stanza, if certs get rotated (for example
	// when using cert-manager, or when the operator rotates its own CA).
	caBundle, ok := r.readCaBundle(logger)
	if !ok {
		return stanzaMatches, nil
	}

	if stanzaMatches && caBundleMatches(crd, caBundle) {
		// Fully up to date: our service target and the current CA bundle are both already in place.
		logger.Info("PersesDashboard CRD conversion webhook (stanza and caBundle) is up to date, no need to patch.")
		return stanzaMatches, nil
	}

	patched := crd.DeepCopy()
	patched.Spec.Conversion = r.buildConversionStanza(caBundle)

	if err := r.Patch(ctx, patched, client.MergeFromWithOptions(crd, client.MergeFromWithOptimisticLock{})); err != nil {
		return stanzaMatches, err
	}

	switch {
	case stanzaMatches:
		// The stanza already pointed at us and only the caBundle changed: this is a routine CA rotation (e.g. after a
		// `helm upgrade`), not a patch-war, so log it at info level.
		logger.Info(
			"Refreshed the CA bundle on the perses.dev PersesDashboard CRD conversion webhook after a certificate rotation.",
		)
	case onUpdate:
		logger.Warn(
			"The conversion stanza on the perses.dev PersesDashboard CRD was missing or pointing elsewhere on a CRD update; " +
				"re-applied the Dash0-operator conversion webhook configuration. If this happens repeatedly, a GitOps reconciler " +
				"(Argo CD, Flux, etc.) is likely overwriting the CRD. Configure your GitOps tooling to leave spec.conversion " +
				"alone (preferred), install the Perses operator to let it handle the conversion, or set the Helm value " +
				"operator.persesDashboard.autoPatchConversionWebhook=false.",
		)
	default:
		logger.Info(
			"Patched the perses.dev PersesDashboard CRD to route conversions through the Dash0 operator's conversion webhook.",
		)
	}
	return stanzaMatches, nil
}

// readCaBundle returns the CA bundle for the conversion webhook stanza, or false if it is not available (yet).
func (r *PersesDashboardCrdReconciler) readCaBundle(logger logd.Logger) ([]byte, bool) {
	var caBundle []byte
	caBundleSource := "operator-managed webhook certificates"
	if r.conversionWebhookSettings.CaBundleSource != nil {
		caBundle = r.conversionWebhookSettings.CaBundleSource()
	} else {
		caBundleSource = r.conversionWebhookSettings.CaBundlePath
		var err error
		if caBundle, err = os.ReadFile(r.conversionWebhookSettings.CaBundlePath); err != nil {
			logger.Error(
				err,
				"Cannot read CA bundle for patching the perses.dev PersesDashboard CRD conversion webhook; "+
					"skipping the patch. v1alpha1 PersesDashboard resources may lose fields when stored.",
				"caBundlePath", r.conversionWebhookSettings.CaBundlePath,
			)
			return nil, false
		}
	}
	// Skip the patch on an empty bundle: this happens transiently while the serving certificate is being provisioned.
	// Patching with an empty caBundle would make the API server reject every conversion request until we re-reconcile and
	// rewrite the bundle, so it's strictly worse than leaving the previous stanza in place.
	if len(bytes.TrimSpace(caBundle)) == 0 {
		logger.Error(
			fmt.Errorf("the CA bundle is empty"),
			"CA bundle for patching the perses.dev PersesDashboard CRD conversion webhook is empty; skipping the patch. "+
				"This is expected briefly while the serving certificate is being provisioned; the patch will be "+
				"attempted again. If it persists, v1alpha1 PersesDashboard resources may lose fields when stored.",
			"caBundleSource", caBundleSource,
		)
		return nil, false
	}
	return caBundle, true
}

// conversionStanzaMatchesOurs reports whether the CRD's conversion stanza is already pointing at our webhook (same
// service/namespace/path). The caBundle is intentionally not compared here, since it can rotate independently of who
// owns the patch. Whether the caBundle needs to be updated is checked separately via caBundleMatches.
func (r *PersesDashboardCrdReconciler) conversionStanzaMatchesOurs(crd *apiextensionsv1.CustomResourceDefinition) bool {
	c := crd.Spec.Conversion
	if c == nil || c.Strategy != apiextensionsv1.WebhookConverter || c.Webhook == nil {
		return false
	}
	svc := c.Webhook.ClientConfig.Service
	if svc == nil {
		return false
	}
	if svc.Name != r.conversionWebhookSettings.WebhookServiceName || svc.Namespace != r.conversionWebhookSettings.OperatorNamespace {
		return false
	}
	if svc.Port == nil || *svc.Port != r.conversionWebhookSettings.WebhookServicePort {
		return false
	}
	if svc.Path == nil || *svc.Path != util.PersesDashboardConversionWebhookPath {
		return false
	}
	return true
}

// caBundleMatches reports whether the caBundle currently stored on the CRD's conversion webhook stanza is equal to the
// given bundle. Returns false when the stanza (or its client config) is absent, so that a missing bundle is treated as
// needing a patch.
func caBundleMatches(crd *apiextensionsv1.CustomResourceDefinition, caBundle []byte) bool {
	c := crd.Spec.Conversion
	if c == nil || c.Webhook == nil || c.Webhook.ClientConfig == nil {
		return false
	}
	return bytes.Equal(c.Webhook.ClientConfig.CABundle, caBundle)
}

// hasForeignConversionWebhook reports whether the CRD already has a Webhook conversion strategy configured but pointing somewhere
// other than our service. That means another controller (typically the Perses operator) is managing the conversion. If that is
// the case we will not interfere.
func hasForeignConversionWebhook(crd *apiextensionsv1.CustomResourceDefinition) bool {
	c := crd.Spec.Conversion
	if c == nil || c.Strategy != apiextensionsv1.WebhookConverter || c.Webhook == nil {
		return false
	}
	return c.Webhook.ClientConfig.Service != nil || (c.Webhook.ClientConfig.URL != nil && *c.Webhook.ClientConfig.URL != "")
}

func (r *PersesDashboardCrdReconciler) buildConversionStanza(caBundle []byte) *apiextensionsv1.CustomResourceConversion {
	path := util.PersesDashboardConversionWebhookPath
	port := r.conversionWebhookSettings.WebhookServicePort
	return &apiextensionsv1.CustomResourceConversion{
		Strategy: apiextensionsv1.WebhookConverter,
		Webhook: &apiextensionsv1.WebhookConversion{
			ConversionReviewVersions: []string{"v1"},
			ClientConfig: &apiextensionsv1.WebhookClientConfig{
				CABundle: caBundle,
				Service: &apiextensionsv1.ServiceReference{
					Name:      r.conversionWebhookSettings.WebhookServiceName,
					Namespace: r.conversionWebhookSettings.OperatorNamespace,
					Path:      &path,
					Port:      &port,
				},
			},
		},
	}
}
