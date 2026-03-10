// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"

	otelmetric "go.opentelemetry.io/otel/metric"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

// The AutoNamespaceMonitoringReconciler watches the operator configuration resource (in parallel to the operator
// configuration controller). If the operator configuration resource becomes available and has automatic namespace
// monitoring enabled, it starts watching namespaces. If automatic namespace monitoring is disabled, it stops watching
// namespaces.
type AutoNamespaceMonitoringReconciler struct {
	client.Client
	manager           ctrl.Manager
	operatorNamespace string
	namespaceWatcher  *NamespaceWatcher
}

var (
	autoNamespaceMonitoringOperatorConfigurationReconcileRequestMetric otelmetric.Int64Counter
	namespaceReconcileRequestMetric                                    otelmetric.Int64Counter
)

func NewAutoNamespaceMonitoringReconciler(
	k8sClient client.Client,
	operatorNamespace string,
) *AutoNamespaceMonitoringReconciler {
	return &AutoNamespaceMonitoringReconciler{
		Client:            k8sClient,
		operatorNamespace: operatorNamespace,
	}
}

func (r *AutoNamespaceMonitoringReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.manager = mgr
	r.namespaceWatcher = NewNamespaceWatcher(r.Client, r.operatorNamespace)
	return ctrl.NewControllerManagedBy(mgr).
		For(&dash0v1alpha1.Dash0OperatorConfiguration{}).
		Named("autoNamespaceMonitoring").
		Complete(r)
}

func (r *AutoNamespaceMonitoringReconciler) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName :=
		fmt.Sprintf("%s%s", metricNamePrefix, "autonamespacemonitoring.operatorconfiguration.reconcile_requests")
	var err error
	if autoNamespaceMonitoringOperatorConfigurationReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription(
			"Counter for operatorconfiguration CRD reconcile requests in the auto-namespace-monitoring controller"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}

	r.namespaceWatcher.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		logger,
	)
}

func (r *AutoNamespaceMonitoringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if autoNamespaceMonitoringOperatorConfigurationReconcileRequestMetric != nil {
		autoNamespaceMonitoringOperatorConfigurationReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a operator configuration in auto-namespace-monitoring controller")

	checkResourceResult, err := resources.VerifyThatResourceExists(
		ctx,
		r.Client,
		req,
		&dash0v1alpha1.Dash0OperatorConfiguration{},
		logger,
	)
	if err != nil {
		logger.Error(err, "operator configuration resource existence check failed")
		return ctrl.Result{}, err
	} else if checkResourceResult.ResourceDoesNotExist {
		logger.Debug("operator configuration resource does not exist, stopping namespace watch")
		r.ensureNamespaceWatchIsStopped(ctx, logger)
		return ctrl.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		return ctrl.Result{}, nil
	}

	operatorConfigurationResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0OperatorConfiguration)

	if !operatorConfigurationResource.IsAvailable() {
		logger.Debug("operator configuration unavailable, stopping namespace watch")
		r.ensureNamespaceWatchIsStopped(ctx, logger)
		return ctrl.Result{}, nil
	}

	if operatorConfigurationResource.Spec.AutoMonitorNamespaces.IsEnabled() {
		logger.Debug("AutoMonitorNamespaces is enabled, starting namespace watch")
		if err := r.ensureNamespaceWatchIsActiveWithCorrectLabelSelector(
			ctx,
			operatorConfigurationResource,
			logger,
		); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		logger.Debug("AutoMonitorNamespaces disabled, stopping namespace watch")
		r.ensureNamespaceWatchIsStopped(ctx, logger)
	}
	return ctrl.Result{}, nil
}

func (r *AutoNamespaceMonitoringReconciler) ensureNamespaceWatchIsActiveWithCorrectLabelSelector(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	removeMonitoringForPreviousLabelSelector := false
	currentLabelSelector := operatorConfigurationResource.Spec.AutoMonitorNamespaces.LabelSelector
	previousLabelSelector := operatorConfigurationResource.Status.PreviousAutoMonitorNamespacesLabelSelector
	if previousLabelSelector != "" && previousLabelSelector != currentLabelSelector {
		// The label selector has changed; stop the existing watch so it will be recreated with the new selector.
		logger.Info("The AutoMonitorNamespaces label selector has changed, recreating the namespace controller and watch.")
		r.ensureNamespaceWatchIsStopped(ctx, logger)
		removeMonitoringForPreviousLabelSelector = true
	}

	r.ensureNamespaceWatchIsActive(currentLabelSelector, logger)

	if removeMonitoringForPreviousLabelSelector {
		logger.Info("removing monitoring from namespaces that no longer match the label selector")
		r.unmonitorNamespacesThatNoLongerMatchTheChangedLabelSelector(
			ctx,
			logger,
			previousLabelSelector,
			currentLabelSelector,
		)
	}

	monitoringTemplateHasChanged := compareMonitoringTemplates(
		operatorConfigurationResource.Status.PreviousMonitoringTemplate,
		operatorConfigurationResource.Spec.MonitoringTemplate,
	)
	if monitoringTemplateHasChanged && operatorConfigurationResource.Spec.MonitoringTemplate != nil {
		r.updateAllAutoMonitoringResourcesWithNewTemplate(
			ctx,
			*operatorConfigurationResource.Spec.MonitoringTemplate,
			currentLabelSelector,
			logger,
		)
	}

	err := r.updateOperatorConfigurationStatus(ctx, operatorConfigurationResource, previousLabelSelector, currentLabelSelector, monitoringTemplateHasChanged, logger)
	if err != nil {
		return err
	}

	return nil
}

func (r *AutoNamespaceMonitoringReconciler) ensureNamespaceWatchIsActive(labelSelector string, logger logd.Logger) {
	r.namespaceWatcher.controllerStopFunctionLock.Lock()
	defer r.namespaceWatcher.controllerStopFunctionLock.Unlock()

	if r.namespaceWatcher.isWatching() {
		// we are already watching, do not start a second watch
		logger.Debug("namespace watch is already active")
		return
	}

	// Create or recreate the controller for watching namespaces.
	// Note: We cannot use the controller builder API here since it does not allow passing in a context for starting the
	// controller. Instead, we create the controller manually and start it in a goroutine. We can also not use
	// controller.NewTyped because that adds the controller to the manager internally, and the controller will be started
	// implicitly. Using controller.NewTypedUnmanaged is the only way that allows full control over stopping and
	// recreating/restarting it on demand.
	logger.Debug("(re)creating the namespace controller")
	namespaceController, err :=
		controller.NewTypedUnmanaged(
			"namespace-controller",
			controller.TypedOptions[reconcile.Request]{
				Reconciler: r.namespaceWatcher,
				// We stop the controller everytime auto-monitoring namespaces is disabled or the namespace label selector
				// changes, and then potentially recreate and restart it later . But the controller-runtime library does not
				// remove the controller name from the set of controller names when the controller is stopped, so we need to
				// skip the duplicate name validation check.
				// See also: https://github.com/kubernetes-sigs/controller-runtime/issues/2983#issuecomment-2440089997.
				SkipNameValidation: new(true),
			})
	if err != nil {
		logger.Error(err, "cannot create new namespace controller")
		return
	}
	logger.Info("successfully created a new namespace controller")

	// Add the watch for namespaces to the controller, with an optional label selector predicate.
	var labelSelectorPredicate predicate.TypedPredicate[*corev1.Namespace]
	if labelSelector != "" {
		selector, err := labels.Parse(labelSelector)
		if err != nil {
			logger.Error(err, "failed to parse label selector for namespace watch predicate, watching all namespaces")
		} else {
			labelSelectorPredicate = &namespaceLabelSelectorPredicate{selector: selector}
		}
	}
	watchPredicates := make([]predicate.TypedPredicate[*corev1.Namespace], 0, 1)
	if labelSelectorPredicate != nil {
		watchPredicates = append(watchPredicates, labelSelectorPredicate)
	}
	logger.Debug("(re)creating the namespace watch")
	if err = namespaceController.Watch(
		source.TypedKind[*corev1.Namespace, reconcile.Request](
			r.manager.GetCache(),
			&corev1.Namespace{},
			&handler.TypedEnqueueRequestForObject[*corev1.Namespace]{},
			watchPredicates...,
		),
	); err != nil {
		logger.Error(err, "unable to create a new watch for namespaces")
		return
	}
	logger.Info("successfully created a new watch for namespaces")

	// start the controller
	backgroundCtx := context.Background()
	childContextForNamespaceController, stopNamespaceController := context.WithCancel(backgroundCtx)
	stopFuncPtr := &stopNamespaceController
	r.namespaceWatcher.controllerStopFunction = stopFuncPtr
	go func() {
		logger.Info("starting the namespace controller")
		if err = namespaceController.Start(childContextForNamespaceController); err != nil {
			r.namespaceWatcher.controllerStopFunctionLock.Lock()

			// Only nil the controllerStopFunction if it is the still the same function pointer that this current invocation
			// of ensureNamespaceWatchIsActive has created.
			if r.namespaceWatcher.controllerStopFunction == stopFuncPtr {
				r.namespaceWatcher.controllerStopFunction = nil
			}
			r.namespaceWatcher.controllerStopFunctionLock.Unlock()
			logger.Error(err, "unable to start the namespace controller")
			return
		}

		logger.Info("the namespace controller has been stopped")
		r.namespaceWatcher.controllerStopFunctionLock.Lock()
		if r.namespaceWatcher.controllerStopFunction == stopFuncPtr {
			r.namespaceWatcher.controllerStopFunction = nil
		}
		r.namespaceWatcher.controllerStopFunctionLock.Unlock()
	}()
}

func (r *AutoNamespaceMonitoringReconciler) ensureNamespaceWatchIsStopped(ctx context.Context, logger logd.Logger) {
	r.namespaceWatcher.controllerStopFunctionLock.Lock()
	defer r.namespaceWatcher.controllerStopFunctionLock.Unlock()

	if !r.namespaceWatcher.isWatching() {
		logger.Debug("the namespace watch is already inactive")
		return
	}

	logger.Debug("removing the namespace informer")
	if err := r.manager.GetCache().RemoveInformer(ctx, &corev1.Namespace{}); err != nil {
		logger.Error(err, "unable to remove the namespace informer")
	}
	logger.Info("triggering the namespace controller stop")
	(*r.namespaceWatcher.controllerStopFunction)()
	r.namespaceWatcher.controllerStopFunction = nil
}

func (r *AutoNamespaceMonitoringReconciler) unmonitorNamespacesThatNoLongerMatchTheChangedLabelSelector(
	ctx context.Context,
	logger logd.Logger,
	previousLabelSelector string,
	currentLabelSelector string,
) {
	// The label selector has changed. Namespaces that now match the new label selector will be picked up by Reconcile,
	// since we create a new namespace controller and watch. When the watch starts up, it will reconcile all matching
	// namespaces once. We still need to remove the monitoring resources from the namespaces that no longer match the
	// new label selector.
	go func() {
		previousSelector, err := labels.Parse(previousLabelSelector)
		if err != nil {
			logger.Error(err, "cannot parse previous label selector after the auto-namespace-monitoring label selector has changed")
			return
		}
		currentSelector, err := labels.Parse(currentLabelSelector)
		if err != nil {
			logger.Error(err, "cannot parse current label selector after the auto-namespace-monitoring label selector has changed")
			return
		}

		namespaceList := &corev1.NamespaceList{}
		if err := r.List(ctx, namespaceList, client.MatchingLabelsSelector{Selector: previousSelector}); err != nil {
			logger.Error(err, "cannot list namespaces after the auto-namespace-monitoring label selector has changed")
			return
		}
		for _, ns := range namespaceList.Items {
			if currentSelector.Matches(labels.Set(ns.Labels)) {
				// This namespace still matches the current label selector, even if it also matched the previous label selector.
				// Nothing to do here, this namespace should already be monitored, if not, the new namespace watch will pick
				// it up and reconcile it.
				continue
			}
			if err := r.namespaceWatcher.ensureNamespaceIsUnmonitored(ctx, &ns, logger); err != nil {
				logger.Error(
					err,
					"cannot unmonitor namespace after the auto-namespace-monitoring label selector has changed",
					"namespace",
					ns.Name,
				)
			}
		}
	}()
}

func (r *AutoNamespaceMonitoringReconciler) updateAllAutoMonitoringResourcesWithNewTemplate(
	ctx context.Context,
	monitoringTemplate dash0v1alpha1.MonitoringTemplate,
	labelSelector string,
	logger logd.Logger,
) {
	// The monitoring template has changed. All automatically managed monitoring resources need to be updated.
	go func() {
		selector, err := labels.Parse(labelSelector)
		if err != nil {
			logger.Error(
				err,
				"cannot parse label selector for auto-namespace-monitoring after the monitoring template has changed",
			)
			return
		}

		namespaceList := &corev1.NamespaceList{}
		if err := r.List(ctx, namespaceList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			logger.Error(
				err,
				"cannot list namespaces for auto-namespace-monitoring after the monitoring template has changed",
			)
			return
		}
		for _, ns := range namespaceList.Items {
			list := &dash0v1beta1.Dash0MonitoringList{}
			if err := r.List(ctx, list,
				client.InNamespace(ns.Name),
				client.MatchingLabels{util.AutoMonitoredNamespaceLabel: util.TrueString},
			); err != nil {
				logger.Error(
					err,
					"cannot list monitoring resources in namespace to update it after the monitoring template has changed",
					"namespace",
					ns.Name,
				)
				continue
			}
			if len(list.Items) == 0 {
				// No auto-monitoring resource in this namespace yet. This will be reconciled by the namespace watch later.
				logger.Debug(
					"namespace does not have a auto-monitoring resource yet, nothing to update",
					"namespace",
					ns.Name,
				)
				continue
			}
			if err := r.namespaceWatcher.reconcileResourceWithMonitoringTemplate(
				ctx,
				list.Items[0],
				monitoringTemplate,
				logger,
			); err != nil {
				logger.Error(
					err,
					"cannot update monitoring resource in namespace to reflect the updated monitoring template",
					"namespace",
					ns.Name,
				)
			}
		}
	}()
}

func (r *AutoNamespaceMonitoringReconciler) updateOperatorConfigurationStatus(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	previousLabelSelector string,
	currentLabelSelector string,
	monitoringTemplateHasChanged bool,
	logger logd.Logger,
) error {
	if previousLabelSelector == currentLabelSelector && !monitoringTemplateHasChanged {
		return nil
	}

	if previousLabelSelector != currentLabelSelector {
		logger.Info(
			"Writing new label selector to operator configuration resource status",
			"previous label selector",
			previousLabelSelector,
			"new label selector",
			currentLabelSelector,
		)
	}

	if monitoringTemplateHasChanged {
		logger.Info(
			"Writing new monitoring template to operator configuration resource status",
			"previous monitoring template",
			operatorConfigurationResource.Status.PreviousMonitoringTemplate,
			"new monitoring template",
			operatorConfigurationResource.Spec.MonitoringTemplate,
		)
	}

	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: "",
			Name:      operatorConfigurationResource.Name,
		},
		operatorConfigurationResource,
	); err != nil {
		logger.Error(err, "failed to reload the operator configuration to update its status with the previous label selector")
		return err
	}
	operatorConfigurationResource.Status.PreviousAutoMonitorNamespacesLabelSelector = currentLabelSelector
	// Potential optimization: instead of storing the full previous template, we could only store a hash. This would be
	// good enough to find out if something has changed.
	operatorConfigurationResource.Status.PreviousMonitoringTemplate = operatorConfigurationResource.Spec.MonitoringTemplate
	logger.Debug("updating operator configuration resource status")
	if err := r.Status().Update(ctx, operatorConfigurationResource); err != nil {
		logger.Error(err, "failed to update operator configuration status with the previous label selector")
		return err
	}
	return nil
}

type NamespaceWatcher struct {
	client.Client
	operatorNamespace          string
	controllerStopFunctionLock sync.Mutex
	controllerStopFunction     *context.CancelFunc
}

func NewNamespaceWatcher(k8sClient client.Client, operatorNamespace string) *NamespaceWatcher {
	return &NamespaceWatcher{
		Client:            k8sClient,
		operatorNamespace: operatorNamespace,
	}
}

// Checks whether the namespace watcher is currently watching by checking if controllerStopFunction is no nil.
// The caller is responsible for acquiring the w.controllerStopFunctionLock before calling this function.
func (w *NamespaceWatcher) isWatching() bool {
	return w.controllerStopFunction != nil
}

func (w *NamespaceWatcher) InitializeSelfMonitoringMetrics(
	meter otelmetric.Meter,
	metricNamePrefix string,
	logger logd.Logger,
) {
	reconcileRequestMetricName := fmt.Sprintf("%s%s", metricNamePrefix, "autonamespacemonitoring.namespace.reconcile_requests")
	var err error
	if namespaceReconcileRequestMetric, err = meter.Int64Counter(
		reconcileRequestMetricName,
		otelmetric.WithUnit("1"),
		otelmetric.WithDescription("Counter for namespace reconcile requests in the auto-namespace-monitoring controller"),
	); err != nil {
		logger.Error(err, fmt.Sprintf("Cannot initialize the metric %s.", reconcileRequestMetricName))
	}
}

func (w *NamespaceWatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if namespaceReconcileRequestMetric != nil {
		namespaceReconcileRequestMetric.Add(ctx, 1)
	}

	logger := logd.FromContext(ctx)
	logger.Info("processing reconcile request for a namespace")

	if slices.Contains(util.RestrictedNamespaces, req.Name) {
		// Note: Instead of skipping restricted namespaces, we could also install a monitoring resource with reduced
		// capabilities, e.g. only logging and event collection. For now, we don't do that. Should probably require an
		// additional opt-in flag.
		logger.Debug("Skipping restricted namespace for auto-namespace monitoring.", "namespace", req.Name)
		return ctrl.Result{}, nil
	}
	if req.Name == w.operatorNamespace {
		logger.Debug("Skipping operator namespace for auto-namespace monitoring.", "namespace", req.Name)
		return ctrl.Result{}, nil
	}

	ns := &corev1.Namespace{}
	if err := w.Get(ctx, req.NamespacedName, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if ns.DeletionTimestamp != nil && !ns.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	operatorConfigList := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := w.List(ctx, operatorConfigList); err != nil {
		return ctrl.Result{}, err
	}
	var availableOperatorConfiguration *dash0v1alpha1.Dash0OperatorConfiguration
	for i := range operatorConfigList.Items {
		if operatorConfigList.Items[i].IsAvailable() {
			availableOperatorConfiguration = &operatorConfigList.Items[i]
			break
		}
	}

	if availableOperatorConfiguration == nil {
		// This should not happen (or it should self-heal) since we only enable the namespace watch if there is an operator
		// configuration resource with status available and autoMonitorNamespaces enabled.
		logger.Error(
			fmt.Errorf("no available Dash0OperatorConfiguration resource"),
			"Aborting auto-namespace monitoring.",
		)
		return ctrl.Result{}, nil
	}

	shouldMonitor := false
	var monitoringTemplate dash0v1alpha1.MonitoringTemplate
	autoMonitor := availableOperatorConfiguration.Spec.AutoMonitorNamespaces

	if autoMonitor.IsEnabled() {
		if availableOperatorConfiguration.Spec.MonitoringTemplate == nil {
			// This should not happen since the operator configuration mutating webhook sets a default monitoring template
			// if automatic namespace monitoring is enabled.
			logger.Error(
				fmt.Errorf(
					"namespace auto-monitoring is enabled, but the monitoring template is not set"),
				"cannot auto-monitor namespace",
			)
			// do not retry reconcile request, this will not self-heal until the operator configuration is fixed
			return ctrl.Result{}, nil

		}
		if namespaceMatchesLabelSelector(ns, autoMonitor.LabelSelector) {
			shouldMonitor = true
			monitoringTemplate = *availableOperatorConfiguration.Spec.MonitoringTemplate
		}
	}

	if shouldMonitor {
		// The namespace is supposed to be auto-monitored. If there is no monitoring resource, create one.
		if err := w.ensureNamespaceIsMonitored(ctx, ns, monitoringTemplate, logger); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// The namespace should not be auto-monitored. If there is an automatically created monitoring resource, remove it.
		if err := w.ensureNamespaceIsUnmonitored(ctx, ns, logger); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (w *NamespaceWatcher) ensureNamespaceIsMonitored(
	ctx context.Context,
	ns *corev1.Namespace,
	monitoringTemplate dash0v1alpha1.MonitoringTemplate,
	logger logd.Logger,
) error {
	existingList := &dash0v1beta1.Dash0MonitoringList{}
	if err := w.List(ctx, existingList, client.InNamespace(ns.Name)); err != nil {
		return err
	}
	if len(existingList.Items) == 0 {
		if err := w.createMonitoringResource(ctx, ns.Name, monitoringTemplate, logger); err != nil {
			return err
		}
		return nil
	}

	existingMonitoringResource := existingList.Items[0]
	if existingMonitoringResource.Labels[util.AutoMonitoredNamespaceLabel] == util.TrueString {
		// The automatically created monitoring resource already exists. It might need to be updated in case the monitoring
		// template in the operator configuration has changed since creating it.
		return w.reconcileResourceWithMonitoringTemplate(
			ctx,
			existingMonitoringResource,
			monitoringTemplate,
			logger,
		)
	}
	logger.Info(
		"There already is a Dash0Monitoring resource in this namespace that has not been created via auto-namespace "+
			"monitoring, skipping this namespace.",
		"namespace",
		ns.Name,
	)
	return nil
}

func (w *NamespaceWatcher) ensureNamespaceIsUnmonitored(
	ctx context.Context,
	ns *corev1.Namespace,
	logger logd.Logger,
) error {
	existingList := &dash0v1beta1.Dash0MonitoringList{}
	if err := w.List(ctx, existingList,
		client.InNamespace(ns.Name),
		client.MatchingLabels{util.AutoMonitoredNamespaceLabel: util.TrueString},
	); err != nil {
		return err
	}
	for i := range existingList.Items {
		logger.Info(
			"removing monitoring resource from namespace after the auto-namespace-monitoring label selector has changed",
			"namespace",
			ns.Name,
		)
		if err := w.Delete(ctx, &existingList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (w *NamespaceWatcher) createMonitoringResource(
	ctx context.Context,
	namespaceName string,
	template dash0v1alpha1.MonitoringTemplate,
	logger logd.Logger,
) error {
	name := template.Name
	if name == "" {
		name = util.MonitoringAutoResourceDefaultName
	}

	resourceLabels := map[string]string{}
	for k, v := range template.Labels {
		resourceLabels[k] = v
	}
	resourceLabels[util.AutoMonitoredNamespaceLabel] = util.TrueString

	resourceAnnotations := map[string]string{}
	for k, v := range template.Annotations {
		resourceAnnotations[k] = v
	}

	monitoring := &dash0v1beta1.Dash0Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespaceName,
			Labels:      resourceLabels,
			Annotations: resourceAnnotations,
		},
		Spec: template.Spec,
	}

	if err := w.Create(ctx, monitoring); err != nil {
		logger.Error(err, "failed to create auto Dash0Monitoring resource", "namespace", namespaceName)
		return err
	}
	logger.Info("created auto Dash0Monitoring resource", "namespace", namespaceName, "name", name)
	return nil
}

// reconcileResourceWithMonitoringTemplate makes sure the monitoring resource reflects the current monitoring template.
func (w *NamespaceWatcher) reconcileResourceWithMonitoringTemplate(
	ctx context.Context,
	monitoringResource dash0v1beta1.Dash0Monitoring,
	monitoringTemplate dash0v1alpha1.MonitoringTemplate,
	logger logd.Logger,
) error {

	// TODO: The current approach is to compare all relevant spec fields in the monitoring resource spec with the
	// monitoring template, and update the monitoring resource spec if needed. This is probably not a good long term
	// solution. If we add more fields to the Dash0MonitoringSpec type, we might easily forget to update this particular
	// method. A better approach might be to use reflect.DeepEqual to compare the monitoring spec with the monitoring
	// template. This needs to be carefully tested so the reflect.DeepEqual does not lead to false positives and
	// a lot of unnecessary updates, in particular due to normalization of the existing monitoring resource having gone
	// through internal/webhooks/monitoring_mutating_webhook.go.
	// This is also tracked in
	// https://linear.app/dash0/issue/OPE-263/revisit-approach-to-apply-changed-monitoring-template-in.

	// Compare spec fields individually. For fields that the mutating webhook fills in when the template leaves them
	// unset (nil/*empty*), we skip the comparison when the template value is the zero value — those fields are managed
	// by the webhook and are intentionally absent from the template.
	// NormalizedTransformSpec is always skipped: it is derived from Transform by the webhook and never present in the
	// template.

	if hasBeenUpdated :=
		compareMonitoringResourceToMonitoringTemplateAndUpdate(monitoringTemplate, &monitoringResource); !hasBeenUpdated {
		return nil
	}

	logger.Debug(
		"auto Dash0Monitoring resource does not match the monitoring template, updating",
		"namespace", monitoringResource.Namespace,
		"name", monitoringResource.Name,
	)
	if err := w.Update(ctx, &monitoringResource); err != nil {
		logger.Error(
			err,
			"failed to update auto Dash0Monitoring resource to match the monitoring template",
			"namespace", monitoringResource.Namespace,
			"name", monitoringResource.Name,
		)
		return err
	}
	logger.Info(
		"updated auto Dash0Monitoring resource to match the monitoring template",
		"namespace", monitoringResource.Namespace,
		"name", monitoringResource.Name,
	)
	return nil
}

func compareMonitoringResourceToMonitoringTemplateAndUpdate(
	monitoringTemplate dash0v1alpha1.MonitoringTemplate,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
) bool {
	objectMetaHasBeenUpdated := compareAndUpdateObjectMeta(
		monitoringTemplate.ObjectMeta,
		&monitoringResource.ObjectMeta,
		true,
	)
	specHasBeenUpdate := compareAndUpdateSpec(monitoringTemplate.Spec, &monitoringResource.Spec)
	return objectMetaHasBeenUpdated || specHasBeenUpdate
}

func compareMonitoringTemplates(
	t1 *dash0v1alpha1.MonitoringTemplate,
	t2 *dash0v1alpha1.MonitoringTemplate,
) bool {
	if t1 == nil && t2 == nil {
		return false
	}
	if t1 == nil {
		return true
	}
	if t2 == nil {
		return true
	}

	// We are re-using the compareAndUpdate functions that are also used to update the monitoring resource from a
	// template. In this case here, we don't want any object to be updated, we only need to find out whether the two
	// templates are different. This achieved by handing the comparison functions clones.
	t1Cloned := t1.DeepCopy()
	t2Cloned := t2.DeepCopy()
	objectMetaHasBeenUpdated := compareAndUpdateObjectMeta(
		t1Cloned.ObjectMeta,
		&t2Cloned.ObjectMeta,
		false,
	)
	specHasBeenUpdate := compareAndUpdateSpec(t1Cloned.Spec, &t2Cloned.Spec)
	return objectMetaHasBeenUpdated || specHasBeenUpdate
}

func compareAndUpdateObjectMeta(
	monitoringTemplateObjectMeta metav1.ObjectMeta,
	monitoringResourceObjectMeta *metav1.ObjectMeta,
	addAutoMonitoredNamespaceLabel bool,
) bool {
	// Deliberately not comparing/updating the name of the resource, even if the monitoring template has a new name. This
	// would require to delete the resource under the old name and create a new resource.

	expectedLabels := map[string]string{}
	for k, v := range monitoringTemplateObjectMeta.Labels {
		expectedLabels[k] = v
	}
	if addAutoMonitoredNamespaceLabel {
		expectedLabels[util.AutoMonitoredNamespaceLabel] = util.TrueString
	}
	// Normalize nil maps to empty maps for comparison.
	existingLabels := monitoringResourceObjectMeta.Labels
	if existingLabels == nil {
		existingLabels = map[string]string{}
	}
	needsLabelsUpdate := !reflect.DeepEqual(existingLabels, expectedLabels)
	if needsLabelsUpdate {
		monitoringResourceObjectMeta.Labels = expectedLabels
	}

	expectedAnnotations := map[string]string{}
	for k, v := range monitoringTemplateObjectMeta.Annotations {
		expectedAnnotations[k] = v
	}
	existingAnnotations := monitoringResourceObjectMeta.Annotations
	if existingAnnotations == nil {
		existingAnnotations = map[string]string{}
	}
	needsAnnotationsUpdate := !reflect.DeepEqual(existingAnnotations, expectedAnnotations)
	if needsAnnotationsUpdate {
		monitoringResourceObjectMeta.Annotations = expectedAnnotations
	}

	return needsLabelsUpdate || needsAnnotationsUpdate
}

func compareAndUpdateSpec(
	monitoringTemplateSpec dash0v1beta1.Dash0MonitoringSpec,
	monitoringResourceSpec *dash0v1beta1.Dash0MonitoringSpec,
) bool {
	specUpdated := false
	if monitoringResourceSpec.InstrumentWorkloads.Mode != monitoringTemplateSpec.InstrumentWorkloads.Mode {
		monitoringResourceSpec.InstrumentWorkloads.Mode = monitoringTemplateSpec.InstrumentWorkloads.Mode
		specUpdated = true
	}
	if monitoringResourceSpec.InstrumentWorkloads.LabelSelector != monitoringTemplateSpec.InstrumentWorkloads.LabelSelector {
		monitoringResourceSpec.InstrumentWorkloads.LabelSelector = monitoringTemplateSpec.InstrumentWorkloads.LabelSelector
		specUpdated = true
	}
	// InstrumentWorkloads.TraceContext.Propagators
	if util.IsStringPointerValueDifferent(
		monitoringResourceSpec.InstrumentWorkloads.TraceContext.Propagators,
		monitoringTemplateSpec.InstrumentWorkloads.TraceContext.Propagators,
	) {
		monitoringResourceSpec.InstrumentWorkloads.TraceContext.Propagators =
			monitoringTemplateSpec.InstrumentWorkloads.TraceContext.Propagators
		specUpdated = true
	}
	if util.ReadBoolPointerWithDefault(monitoringResourceSpec.LogCollection.Enabled, true) !=
		util.ReadBoolPointerWithDefault(monitoringTemplateSpec.LogCollection.Enabled, true) {
		monitoringResourceSpec.LogCollection.Enabled = monitoringTemplateSpec.LogCollection.Enabled
		specUpdated = true
	}
	if util.ReadBoolPointerWithDefault(monitoringResourceSpec.EventCollection.Enabled, true) !=
		util.ReadBoolPointerWithDefault(monitoringTemplateSpec.EventCollection.Enabled, true) {
		monitoringResourceSpec.EventCollection.Enabled = monitoringTemplateSpec.EventCollection.Enabled
		specUpdated = true
	}
	if util.ReadBoolPointerWithDefault(monitoringResourceSpec.PrometheusScraping.Enabled, true) !=
		util.ReadBoolPointerWithDefault(monitoringTemplateSpec.PrometheusScraping.Enabled, true) {
		monitoringResourceSpec.PrometheusScraping.Enabled = monitoringTemplateSpec.PrometheusScraping.Enabled
		specUpdated = true
	}

	if !reflect.DeepEqual(monitoringResourceSpec.Filter, monitoringTemplateSpec.Filter) {
		monitoringResourceSpec.Filter = monitoringTemplateSpec.Filter
		specUpdated = true
	}
	if !reflect.DeepEqual(monitoringResourceSpec.Transform, monitoringTemplateSpec.Transform) {
		monitoringResourceSpec.Transform = monitoringTemplateSpec.Transform
		specUpdated = true
	}
	if util.ReadBoolPointerWithDefault(monitoringResourceSpec.SynchronizePersesDashboards, true) !=
		util.ReadBoolPointerWithDefault(monitoringTemplateSpec.SynchronizePersesDashboards, true) {
		monitoringResourceSpec.SynchronizePersesDashboards = monitoringTemplateSpec.SynchronizePersesDashboards
		specUpdated = true
	}
	if util.ReadBoolPointerWithDefault(monitoringResourceSpec.SynchronizePrometheusRules, true) !=
		util.ReadBoolPointerWithDefault(monitoringTemplateSpec.SynchronizePrometheusRules, true) {
		monitoringResourceSpec.SynchronizePrometheusRules = monitoringTemplateSpec.SynchronizePrometheusRules
		specUpdated = true
	}
	// Note: The operator configuration validating webhook disallows Export/Exports on the monitoring template. Therefore,
	// we can ignore these two fields
	return specUpdated
}

// namespaceLabelSelectorPredicate filters namespace watch events by a label selector. For Update events, it fires if
// either the old or new namespace labels match, so that monitoring resources are cleaned up when labels are removed.
type namespaceLabelSelectorPredicate struct {
	selector labels.Selector
}

func (p *namespaceLabelSelectorPredicate) Create(e event.TypedCreateEvent[*corev1.Namespace]) bool {
	return p.selector.Matches(labels.Set(e.Object.GetLabels()))
}

func (p *namespaceLabelSelectorPredicate) Update(e event.TypedUpdateEvent[*corev1.Namespace]) bool {
	return p.selector.Matches(labels.Set(e.ObjectOld.GetLabels())) ||
		p.selector.Matches(labels.Set(e.ObjectNew.GetLabels()))
}

func (p *namespaceLabelSelectorPredicate) Delete(_ event.TypedDeleteEvent[*corev1.Namespace]) bool {
	// Namespace deletion removes all contained resources automatically; no reconcile needed.
	return false
}

func (p *namespaceLabelSelectorPredicate) Generic(e event.TypedGenericEvent[*corev1.Namespace]) bool {
	return p.selector.Matches(labels.Set(e.Object.GetLabels()))
}

func namespaceMatchesLabelSelector(ns *corev1.Namespace, selectorStr string) bool {
	if selectorStr == "" {
		return true
	}
	selector, err := labels.Parse(selectorStr)
	if err != nil {
		return false
	}
	return selector.Matches(labels.Set(ns.Labels))
}
