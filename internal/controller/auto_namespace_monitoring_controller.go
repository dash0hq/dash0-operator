// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"slices"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
// monitoring enabled, it starts watching namespaces. If automatic namespace is disabled, it stops watching namespaces.
type AutoNamespaceMonitoringReconciler struct {
	client.Client
	manager           ctrl.Manager
	operatorNamespace string
	namespaceWatcher  *NamespaceWatcher
}

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

func (r *AutoNamespaceMonitoringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		r.ensureNamespaceWatchIsStopped(ctx, logger)
		return ctrl.Result{}, nil
	} else if checkResourceResult.StopReconcile {
		return ctrl.Result{}, nil
	}

	operatorConfigurationResource := checkResourceResult.Resource.(*dash0v1alpha1.Dash0OperatorConfiguration)

	if !operatorConfigurationResource.IsAvailable() {
		r.ensureNamespaceWatchIsStopped(ctx, logger)
		return ctrl.Result{}, nil
	}

	if operatorConfigurationResource.Spec.AutoMonitorNamespaces.IsEnabled() {
		if err := r.ensureNamespaceWatchIsActiveWithCorrectLabelSelector(ctx, operatorConfigurationResource, logger); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		r.ensureNamespaceWatchIsStopped(ctx, logger)
	}
	return ctrl.Result{}, nil
}

func (r *AutoNamespaceMonitoringReconciler) ensureNamespaceWatchIsActiveWithCorrectLabelSelector(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	currentLabelSelector := operatorConfigurationResource.Spec.AutoMonitorNamespaces.LabelSelector
	previousLabelSelector := operatorConfigurationResource.Status.PreviousAutoMonitorNamespacesLabelSelector
	if previousLabelSelector != "" && previousLabelSelector != currentLabelSelector {
		// The label selector has changed; stop the existing watch so it will be recreated with the new selector.
		r.ensureNamespaceWatchIsStopped(ctx, logger)
	}

	r.ensureNamespaceWatchIsActive(logger, currentLabelSelector)

	if previousLabelSelector != currentLabelSelector {
		operatorConfigurationResource.Status.PreviousAutoMonitorNamespacesLabelSelector = currentLabelSelector
		if err := r.Status().Update(ctx, operatorConfigurationResource); err != nil {
			logger.Error(err, "failed to update operator configuration status with label selector")
			return err
		}
	}
	return nil
}

func (r *AutoNamespaceMonitoringReconciler) ensureNamespaceWatchIsActive(logger logd.Logger, labelSelector string) {
	r.namespaceWatcher.controllerStopFunctionLock.Lock()
	defer r.namespaceWatcher.controllerStopFunctionLock.Unlock()
	if r.namespaceWatcher.isWatching() {
		// we are already watching, do not start a second watch
		return
	}

	// Create or recreate the controller for watching namespaces.
	// Note: We cannot use the controller builder API here since it does not allow passing in a context for starting the
	// controller. Instead, we create the controller manually and start it in a goroutine.
	namespaceController, err :=
		controller.NewTyped[reconcile.Request](
			"namespace-controller",
			r.manager,
			controller.TypedOptions[reconcile.Request]{
				Reconciler:         r.namespaceWatcher,
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

	// Start the controller.
	backgroundCtx := context.Background()
	childContextForNamespaceController, stopNamespaceController := context.WithCancel(backgroundCtx)
	r.namespaceWatcher.controllerStopFunction = &stopNamespaceController
	go func() {
		if err = namespaceController.Start(childContextForNamespaceController); err != nil {
			r.namespaceWatcher.controllerStopFunction = nil
			logger.Error(err, "unable to start the namespace controller")
			return
		}
		r.namespaceWatcher.controllerStopFunction = nil
		logger.Info("the namespaces controller has been stopped")
	}()
}

func (r *AutoNamespaceMonitoringReconciler) ensureNamespaceWatchIsStopped(ctx context.Context, logger logd.Logger) {
	r.namespaceWatcher.controllerStopFunctionLock.Lock()
	defer r.namespaceWatcher.controllerStopFunctionLock.Unlock()

	if !r.namespaceWatcher.isWatching() {
		return
	}

	logger.Info("removing the namespace informer")
	if err := r.manager.GetCache().RemoveInformer(ctx, &corev1.Namespace{}); err != nil {
		logger.Error(err, "unable to remove the namespace informer")
	}
	logger.Info("stopping the namespace controller")
	(*r.namespaceWatcher.controllerStopFunction)()
	r.namespaceWatcher.controllerStopFunction = nil
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

func (w *NamespaceWatcher) isWatching() bool {
	return w.controllerStopFunction != nil
}

func (w *NamespaceWatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
			// This should not happen since it is supposed to be caught by the operator configuration resource validation
			// webhook.
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
		existingList := &dash0v1beta1.Dash0MonitoringList{}
		// TODO actually, check for _any_ monires, not only the ones with the label.
		// Treat differently though.
		if err := w.List(ctx, existingList,
			client.InNamespace(ns.Name),
			client.MatchingLabels{util.AutoMonitoringNamespaceLabel: "true"},
		); err != nil {
			return ctrl.Result{}, err
		}
		if len(existingList.Items) == 0 {
			if err := w.createMonitoringResource(ctx, ns.Name, monitoringTemplate, logger); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		existingList := &dash0v1beta1.Dash0MonitoringList{}
		if err := w.List(ctx, existingList,
			client.InNamespace(ns.Name),
			client.MatchingLabels{util.AutoMonitoringNamespaceLabel: "true"},
		); err != nil {
			return ctrl.Result{}, err
		}
		for i := range existingList.Items {
			if err := w.Delete(ctx, &existingList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (w *NamespaceWatcher) createMonitoringResource(
	ctx context.Context,
	namespaceName string,
	template dash0v1alpha1.MonitoringTemplate,
	logger logd.Logger,
) error {
	name := template.Name
	if name == "" {
		name = util.MonitoringAutoResourceName
	}

	resourceLabels := map[string]string{}
	for k, v := range template.Labels {
		resourceLabels[k] = v
	}
	resourceLabels[util.AutoMonitoringNamespaceLabel] = "true"

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
