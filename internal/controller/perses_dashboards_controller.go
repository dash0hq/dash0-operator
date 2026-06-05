// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	otelmetric "go.opentelemetry.io/otel/metric"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	queue                     *workqueue.Typed[ThirdPartyResourceSyncJob]
	leaderElectionAware       util.LeaderElectionAware
	mgr                       ctrl.Manager
	httpClient                *http.Client
	skipNameValidation        bool
	persesDashboardReconciler *PersesDashboardReconciler
	persesDashboardCrdExists  atomic.Bool

	// crdVersion holds the perses.dev API version we are currently watching. It is populated by OnCrdChange based on which
	// versions the deployed CRD provides; clusters with an older CRD (up to version v0.2.0) only provide v1alpha1, while >= 0.3.0
	// provides both v1alpha1 and v1alpha2.
	crdVersion atomic.Pointer[string]

	conversionWebhookSettings PersesDashboardConversionWebhookSettings
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
}

type PersesDashboardReconciler struct {
	client.Client
	// crdReconciler is a back-reference to the owning CRD reconciler so the inner reconciler can read the chosen API version (which
	// depends on which versions the cluster's CRD currently serves; see PersesDashboardCrdReconciler.OnCrdChange).
	crdReconciler              *PersesDashboardCrdReconciler
	pseudoClusterUid           types.UID
	queue                      *workqueue.Typed[ThirdPartyResourceSyncJob]
	httpClient                 *http.Client
	defaultApiConfigs          selfmonitoringapiaccess.SynchronizedSlice[ApiConfig]
	namespacedApiConfigs       selfmonitoringapiaccess.SynchronizedMapSlice[ApiConfig]
	namespacedSyncEnabled      sync.Map
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
}

const (
	persesDashboardV1Alpha1         = "v1alpha1"
	persesDashboardV1Alpha2         = "v1alpha2"
	defaultWebhookServingCertCAPath = "/tmp/k8s-webhook-server/serving-certs/ca.crt"
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
	queue *workqueue.Typed[ThirdPartyResourceSyncJob],
	leaderElectionAware util.LeaderElectionAware,
	httpClient *http.Client,
	conversionWebhookSettings PersesDashboardConversionWebhookSettings,
) *PersesDashboardCrdReconciler {
	if conversionWebhookSettings.CaBundlePath == "" {
		conversionWebhookSettings.CaBundlePath = defaultWebhookServingCertCAPath
	}
	return &PersesDashboardCrdReconciler{
		Client:                    k8sClient,
		queue:                     queue,
		leaderElectionAware:       leaderElectionAware,
		httpClient:                httpClient,
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
	return "persesdashboards.perses.dev"
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
		crdReconciler:        r,
		queue:                r.queue,
		pseudoClusterUid:     pseudoClusterUid,
		httpClient:           r.httpClient,
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

func (r *PersesDashboardReconciler) Queue() *workqueue.Typed[ThirdPartyResourceSyncJob] {
	return r.queue
}

func (r *PersesDashboardReconciler) HttpClient() *http.Client {
	return r.httpClient
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
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
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
	ctx context.Context,
	e event.TypedGenericEvent[*unstructured.Unstructured],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	if persesDashboardReconcileRequestMetric != nil {
		persesDashboardReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info(
		"Reconciling dashboard triggered by config event (updated API config or authorization).",
		"namespace",
		e.Object.GetNamespace(),
		"name",
		e.Object.GetName(),
	)

	upsertViaApi(r, e.Object)
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

func (r *PersesDashboardReconciler) FetchExistingResourceOriginsRequests(
	_ *preconditionValidationResult,
	_ ApiConfig,
) ([]*http.Request, error) {
	// The mechanism to delete individual dashboards when synchronizing one Kubernetes PersesDashboard resource is not
	// required, since each PersesDashboard only contains one dashboard. It is only needed when the resource type holds
	// multiple objects that are synchronized (as it is the case for PrometheusRule). Thus, this controller does not
	// need to implement this method.
	return nil, nil
}

func (r *PersesDashboardReconciler) MapResourceToHttpRequests(
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	action apiAction,
	logger logd.Logger,
) *ResourceToRequestsResult {
	itemName := preconditionChecksResult.k8sName

	dashboardUrl, dashboardOrigin := r.renderDashboardUrl(preconditionChecksResult, apiConfig.Endpoint, apiConfig.Dataset)

	var req *http.Request
	var method string
	var err error

	//nolint:ineffassign
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

		serializedDashboard, _ := json.Marshal(dashboard)
		requestPayload := bytes.NewBuffer(serializedDashboard)

		method = http.MethodPut
		req, err = http.NewRequest(
			method,
			dashboardUrl,
			requestPayload,
		)
	case deleteAction:
		method = http.MethodDelete
		req, err = http.NewRequest(
			method,
			dashboardUrl,
			nil,
		)
	default:
		unknownActionErr := fmt.Errorf("unknown API action: %d", action)
		logger.Error(unknownActionErr, "unknown API action")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, unknownActionErr.Error())
	}

	if err != nil {
		httpError := fmt.Errorf(
			"unable to create a new HTTP request to synchronize the dashboard: %s %s: %w",
			method,
			dashboardUrl,
			err,
		)
		logger.Error(httpError, "error creating http request")
		return NewResourceToRequestsResultSingleItemError(apiConfig, itemName, httpError.Error())
	}

	addAuthorizationHeader(req, apiConfig.Token)
	if action == upsertAction {
		req.Header.Set(util.ContentTypeHeaderName, util.ApplicationJsonMediaType)
	}

	return NewResourceToRequestsResultSingleItemSuccess(apiConfig, req, itemName, dashboardOrigin)
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
	datasetUrlEncoded := url.QueryEscape(dataset)
	dashboardOrigin := fmt.Sprintf(
		// we deliberately use _ as the separator, since that is an illegal character in Kubernetes names. This avoids
		// any potential naming collisions (e.g. namespace="abc" & name="def-ghi" vs. namespace="abc-def" & name="ghi").
		"dash0-operator_%s_%s_%s_%s",
		r.pseudoClusterUid,
		datasetUrlEncoded,
		preconditionChecksResult.k8sNamespace,
		preconditionChecksResult.k8sName,
	)
	return fmt.Sprintf(
		"%sapi/dashboards/%s?dataset=%s",
		endpoint,
		dashboardOrigin,
		datasetUrlEncoded,
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

func (r *PersesDashboardReconciler) ExtractIdFromResponseBody(
	responseBytes []byte,
	logger logd.Logger,
) (id string, err error) {
	objectWithMetadata := Dash0ApiObjectWithMetadata{}
	if err := json.Unmarshal(responseBytes, &objectWithMetadata); err != nil {
		logger.Error(
			err,
			"cannot parse response, will not extract the synchronized object's ID",
			"response",
			string(responseBytes),
		)
		return "", err
	}
	return objectWithMetadata.Metadata.Labels.Id, nil
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

		for i := range allDashboardResourcesInNamespace.Items {
			dashboardResource := &allDashboardResourcesInNamespace.Items[i]
			evt := event.TypedGenericEvent[*unstructured.Unstructured]{
				Object: dashboardResource,
			}
			r.Generic(ctx, evt, nil)

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
		return
	}

	if r.conversionStanzaMatchesOurs(crd) {
		return
	}

	if hasForeignConversionWebhook(crd) {
		if !onUpdate {
			logger.Info(
				"Detected pre-existing conversion webhook on the perses.dev PersesDashboard CRD " +
					"(not pointing at the Dash0 operator); leaving the CRD untouched.",
			)
		}
		return
	}

	caBundle, err := os.ReadFile(r.conversionWebhookSettings.CaBundlePath)
	if err != nil {
		logger.Error(
			err,
			"Cannot read CA bundle for patching the perses.dev PersesDashboard CRD conversion webhook; "+
				"skipping the patch. v1alpha1 PersesDashboard resources may lose fields when stored.",
			"caBundlePath", r.conversionWebhookSettings.CaBundlePath,
		)
		return
	}
	// Skip the patch on an empty bundle: this happens transiently while cert-manager (or whatever provisions the serving cert)
	// has not yet materialized the file. Patching with an empty caBundle would make the API server reject every conversion
	// request until we re-reconcile and rewrite the bundle, so it's strictly worse than leaving the previous stanza in place.
	if len(bytes.TrimSpace(caBundle)) == 0 {
		logger.Error(
			fmt.Errorf("CA bundle file is empty"),
			"CA bundle for patching the perses.dev PersesDashboard CRD conversion webhook is empty; skipping the patch. "+
				"This is expected briefly while the serving cert is being provisioned; the next CRD reconciliation will "+
				"retry. If it persists, v1alpha1 PersesDashboard resources may lose fields when stored.",
			"caBundlePath", r.conversionWebhookSettings.CaBundlePath,
		)
		return
	}

	patched := crd.DeepCopy()
	patched.Spec.Conversion = r.buildConversionStanza(caBundle)

	if err := r.Patch(ctx, patched, client.MergeFrom(crd)); err != nil {
		logger.Error(
			err,
			"Failed to patch the perses.dev PersesDashboard CRD with the Dash0-operator conversion webhook configuration. "+
				"v1alpha1 PersesDashboard resources will lose fields when stored.",
		)
		return
	}

	if onUpdate {
		logger.Warn(
			"The conversion stanza on the perses.dev PersesDashboard CRD was missing or pointing elsewhere on a CRD update; " +
				"re-applied the Dash0-operator conversion webhook configuration. If this happens repeatedly, a GitOps reconciler " +
				"(Argo CD, Flux, etc.) is likely overwriting the CRD. Configure your GitOps tooling to leave spec.conversion " +
				"alone (preferred), install the Perses operator to let it handle the conversion, or set the Helm value " +
				"operator.persesDashboard.autoPatchConversionWebhook=false.",
		)
	} else {
		logger.Info(
			"Patched the perses.dev PersesDashboard CRD to route conversions through the Dash0 operator's conversion webhook.",
		)
	}
}

// conversionStanzaMatchesOurs reports whether the CRD's conversion stanza is already pointing
// at our webhook (same service/namespace/path). The caBundle is intentionally not compared,
// since it can rotate independently of who owns the patch.
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
