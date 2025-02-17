// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

// TODO: possible refactoring: introduce a new type ThirdPartyResourceSynchronizer struct, create a singleton of it,
// attach it to both third party resource reconciler and move most of the functions in this file to this new type as
// methods. Then we could get rid of a lot of the interface methods in the ThirdPartyResourceReconciler interface.

type ApiConfig struct {
	Endpoint string
	Dataset  string
}

type ApiClient interface {
	SetApiEndpointAndDataset(context.Context, *ApiConfig, *logr.Logger)
	RemoveApiEndpointAndDataset(context.Context, *logr.Logger)
	SetAuthToken(context.Context, string, *logr.Logger)
	RemoveAuthToken(context.Context, *logr.Logger)
}

type ThirdPartyCrdReconciler interface {
	handler.TypedEventHandler[client.Object, reconcile.Request]
	reconcile.TypedReconciler[reconcile.Request]

	Manager() ctrl.Manager
	KindDisplayName() string
	Group() string
	Kind() string
	Version() string
	QualifiedKind() string
	ControllerName() string
	DoesCrdExist() *atomic.Bool
	SetCrdExists(bool)
	SkipNameValidation() bool
	CreateResourceReconciler(types.UID, *http.Client)
	ResourceReconciler() ThirdPartyResourceReconciler
}

type ThirdPartyResourceReconciler interface {
	handler.TypedEventHandler[*unstructured.Unstructured, reconcile.Request]
	reconcile.TypedReconciler[reconcile.Request]

	KindDisplayName() string
	ShortName() string
	ControllerStopFunctionLock() *sync.Mutex
	GetControllerStopFunction() *context.CancelFunc
	SetControllerStopFunction(*context.CancelFunc)
	IsWatching() bool
	GetAuthToken() string
	GetApiConfig() *atomic.Pointer[ApiConfig]
	ControllerName() string
	K8sClient() client.Client
	Queue() *workqueue.Typed[ThirdPartyResourceSyncJob]
	HttpClient() *http.Client
	GetHttpRetryDelay() time.Duration
	IsSynchronizationEnabled(*dash0v1alpha1.Dash0Monitoring) bool

	// MapResourceToHttpRequests converts a third-party resource object to a list of HTTP requests that can be sent to
	// the Dash0 API. It returns:
	// - the total number of eligible items in the third-party Kubernetes resource,
	// - the request objects for which the conversion was successful,
	// - validation issues for items that were invalid and
	// - synchronization errors that occurred during the conversion.
	MapResourceToHttpRequests(
		*preconditionValidationResult,
		apiAction,
		*logr.Logger,
	) (
		int,
		[]HttpRequestWithItemName,
		map[string][]string,
		map[string]string,
	)
	UpdateSynchronizationResultsInStatus(
		monitoringResource *dash0v1alpha1.Dash0Monitoring,
		qualifiedName string,
		status dash0v1alpha1.SynchronizationStatus,
		itemsTotal int,
		successfullySynchronized []string,
		synchronizationErrorsPerItem map[string]string,
		validationIssuesPerItem map[string][]string,
	) interface{}
}

type ThirdPartyResourceSyncJob struct {
	resourceReconciler ThirdPartyResourceReconciler
	thirdPartyResource *unstructured.Unstructured
	action             apiAction
	actionLabel        string
}

type HttpRequestWithItemName struct {
	ItemName string
	Request  *http.Request
}

type apiAction int

const (
	upsert apiAction = iota
	delete
)

type preconditionValidationResult struct {
	synchronizeResource    bool
	thirdPartyResourceSpec map[string]interface{}
	monitoringResource     *dash0v1alpha1.Dash0Monitoring
	authToken              string
	apiEndpoint            string
	dataset                string
	k8sNamespace           string
	k8sName                string
}

type retryableError struct {
	err       error
	retryable bool
}

func (e *retryableError) Error() string {
	return e.err.Error()
}

func SetupThirdPartyCrdReconcilerWithManager(
	ctx context.Context,
	k8sClient client.Client,
	crdReconciler ThirdPartyCrdReconciler,
	logger *logr.Logger,
) error {
	kubeSystemNamespace := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "kube-system"}, kubeSystemNamespace); err != nil {
		msg := "unable to get the kube-system namespace uid"
		logger.Error(err, msg)
		return fmt.Errorf("%s: %w", msg, err)
	}

	crdReconciler.CreateResourceReconciler(
		kubeSystemNamespace.UID,
		&http.Client{},
	)

	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name: crdReconciler.QualifiedKind(),
	}, &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(
				err,
				fmt.Sprintf("unable to call client.Get(\"%s\") custom resource definition",
					crdReconciler.QualifiedKind()))
			return err
		}
	} else {
		crdReconciler.SetCrdExists(true)
		maybeStartWatchingThirdPartyResources(crdReconciler, true, logger)
	}

	controllerBuilder := ctrl.NewControllerManagedBy(crdReconciler.Manager()).
		Named(crdReconciler.ControllerName()).
		Watches(
			&apiextensionsv1.CustomResourceDefinition{},
			// Deliberately not using a convenience mechanism like &handler.EnqueueRequestForObject{} (which would
			// feed all events into the Reconcile method) here, since using the lower-level TypedEventHandler interface
			// directly allows us to distinguish between create and delete events more easily.
			crdReconciler,
			builder.WithPredicates(
				makeFilterPredicate(
					crdReconciler.Group(),
					crdReconciler.Kind(),
				)))
	if crdReconciler.SkipNameValidation() {
		controllerBuilder = controllerBuilder.WithOptions(controller.TypedOptions[reconcile.Request]{
			SkipNameValidation: ptr.To(true),
		})
	}
	if err := controllerBuilder.Complete(crdReconciler); err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"unable to build the controller for the %s CRD reconciler",
				crdReconciler.KindDisplayName(),
			))
		return err
	}

	return nil
}

func makeFilterPredicate(group string, kind string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isMatchingCrd(group, kind, e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// We are not interested in updates, but we still need to define a filter predicate for it, otherwise _all_
			// update events for CRDs would be passed to our event handler. We always return false to ignore update
			// events entirely. Same for generic events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isMatchingCrd(group, kind, e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

func isMatchingCrd(group string, kind string, crd client.Object) bool {
	if crdCasted, ok := crd.(*apiextensionsv1.CustomResourceDefinition); ok {
		return crdCasted.Spec.Group == group &&
			crdCasted.Spec.Names.Kind == kind
	} else {
		return false
	}
}

func maybeStartWatchingThirdPartyResources(
	crdReconciler ThirdPartyCrdReconciler,
	isStartup bool,
	logger *logr.Logger,
) {
	resourceReconciler := crdReconciler.ResourceReconciler()

	resourceReconciler.ControllerStopFunctionLock().Lock()
	defer resourceReconciler.ControllerStopFunctionLock().Unlock()

	if resourceReconciler.IsWatching() {
		// we are already watching, do not start a second watch
		return
	}

	if !crdReconciler.DoesCrdExist().Load() {
		logger.Info(
			fmt.Sprintf("The %s custom resource definition does not exist in this cluster, the operator will not "+
				"watch for %s resources.",
				crdReconciler.QualifiedKind(),
				crdReconciler.KindDisplayName(),
			))
		return
	}

	apiConfig := resourceReconciler.GetApiConfig().Load()
	authToken := resourceReconciler.GetAuthToken()
	if !isValidApiConfig(apiConfig) || authToken == "" {
		// Silently ignore this missing precondition if it happens during the startup of the operator. It will
		// be remedied automatically once the operator configuration resource is reconciled for the first time, or
		// once the secret ref has been resolved via the secret ref resolver.
		if !isStartup {
			if !isValidApiConfig(apiConfig) {
				logger.Info(
					fmt.Sprintf(
						"The %s custom resource definition is present in this cluster, but no Dash0 API endpoint has "+
							"been provided via the operator configuration resource, or the operator configuration "+
							"resource has not been reconciled yet. The operator will not watch for %s resources. "+
							"(If there is an operator configuration resource with an API endpoint and a Dash0 auth "+
							"token or a secret ref present in the cluster, it will be reconciled in a few seconds "+
							"and this message can be safely ignored.)",
						crdReconciler.QualifiedKind(),
						crdReconciler.KindDisplayName(),
					))
			}
			if authToken == "" {
				logger.Info(
					fmt.Sprintf(
						"The %s custom resource definition is present in this cluster, but the Dash0 auth token is "+
							"not available yet. Either it has not been provided via the operator configuration "+
							"resource, or the operator configuration resource has not been reconciled yet, or it has "+
							"been provided as a secret reference which has not been resolved to a token yet. The "+
							"operator will not watch for %s resources just yet. "+
							"(If there is an operator configuration resource with an API endpoint and a Dash0 auth "+
							"token or a secret ref present in the cluster, it will be reconciled and the secret ref "+
							"(if any) resolved to a token in a few seconds and this message can be safely ignored.)",
						crdReconciler.QualifiedKind(),
						crdReconciler.KindDisplayName(),
					))
			}
		}
		return
	}

	logger.Info(
		fmt.Sprintf(
			"The %s custom resource definition is present in this cluster, and a Dash0 API endpoint and a Dash0 auth "+
				"token have been provided. The operator will watch for %s resources.",
			crdReconciler.QualifiedKind(),
			crdReconciler.KindDisplayName(),
		),
	)

	logger.Info(fmt.Sprintf("Setting up a watch for %s custom resources.", crdReconciler.KindDisplayName()))

	// Create or recreate the controller for the third-party resource type.
	// Note: We cannot use the controller builder API here since it does not allow passing in a context for starting the
	// controller. Instead, we create the controller manually and start it in a goroutine.
	resourceController, err :=
		controller.NewTypedUnmanaged[reconcile.Request](
			resourceReconciler.ControllerName(),
			crdReconciler.Manager(),
			controller.Options{
				Reconciler: resourceReconciler,
				// We stop the controller everytime the third-party CRD is deleted, and then recreate it if the
				// third-party CRD is deployed to the cluster again. But the controller-runtime library does not remove
				// the controller name from the set of controller names when the controller is stopped, so we need to
				// skip the duplicate name validation check.
				// See also: https://github.com/kubernetes-sigs/controller-runtime/issues/2983#issuecomment-2440089997.
				SkipNameValidation: ptr.To(true),
			})
	if err != nil {
		logger.Error(err, fmt.Sprintf("cannot create a new %s", resourceReconciler.ControllerName()))
		return
	}
	logger.Info(fmt.Sprintf("successfully created a new %s", resourceReconciler.ControllerName()))

	// Add the watch for the third-party resource type to the controller.
	//
	// Note: We cannot watch for the specific third-party resource type here directly, that is, we cannot establish the
	// watch using &persesv1alpha1.PersesDashboard{} (or &prometheusv1.PrometheusRule{}) as the second argument when
	// constructing source.Kind. Doing so, the validation for the resource in question would be triggered before the
	// resource is handed over to us. This would potentially fail for dashboards, because Dash0 dashboards allow
	// variable types (like Dash0FilterVariable) that plain vanilla Perses dashboards do not support, and the following
	// validation error would be triggered:
	// reflector.go:158] "Unhandled Error"
	// err="pkg/mod/k8s.io/client-go@v0.31.2/tools/cache/reflector.go:243:
	// Failed to watch *v1alpha1.PersesDashboard: failed to list *v1alpha1.PersesDashboard: unknown variable.kind
	// \"Dash0FilterVariable\" used" logger="UnhandledError"
	if err = resourceController.Watch(
		source.Kind(
			crdReconciler.Manager().GetCache(),
			createUnstructuredGvk(crdReconciler),
			resourceReconciler,
		),
	); err != nil {
		logger.Error(err, fmt.Sprintf("unable to create a new watch for %s resources", crdReconciler.KindDisplayName()))
		return
	}
	logger.Info(fmt.Sprintf("successfully created a new watch for %s resources", crdReconciler.KindDisplayName()))

	// Start the controller.
	backgroundCtx := context.Background()
	childContextForResourceController, stopResourceController := context.WithCancel(backgroundCtx)
	resourceReconciler.SetControllerStopFunction(&stopResourceController)
	go func() {
		if err = resourceController.Start(childContextForResourceController); err != nil {
			resourceReconciler.SetControllerStopFunction(nil)
			logger.Error(err, fmt.Sprintf("unable to start the controller %s", resourceReconciler.ControllerName()))
			return
		}
		resourceReconciler.SetControllerStopFunction(nil)
		logger.Info(fmt.Sprintf("the controller %s has been stopped", resourceReconciler.ControllerName()))
	}()
}

func stopWatchingThirdPartyResources(
	ctx context.Context,
	crdReconciler ThirdPartyCrdReconciler,
	logger *logr.Logger) {
	resourceReconciler := crdReconciler.ResourceReconciler()
	resourceReconciler.ControllerStopFunctionLock().Lock()
	defer resourceReconciler.ControllerStopFunctionLock().Unlock()

	cancelFunc := resourceReconciler.GetControllerStopFunction()
	if cancelFunc == nil {
		logger.Info(fmt.Sprintf("ignoring attempt to stop the controller %s which seems to be stopped already", resourceReconciler.ControllerName()))
		return
	}

	logger.Info(fmt.Sprintf("removing the informer for %s", crdReconciler.KindDisplayName()))
	if err := crdReconciler.Manager().GetCache().RemoveInformer(
		ctx,
		createUnstructuredGvk(crdReconciler),
	); err != nil {
		logger.Error(err, fmt.Sprintf("unable to remove the informer for %s", crdReconciler.KindDisplayName()))
	}
	logger.Info(fmt.Sprintf("stopping the controller %s now", resourceReconciler.ControllerName()))
	(*cancelFunc)()
	resourceReconciler.SetControllerStopFunction(nil)
}

func isValidApiConfig(apiConfig *ApiConfig) bool {
	return apiConfig != nil && apiConfig.Endpoint != ""
}

func createUnstructuredGvk(crdReconciler ThirdPartyCrdReconciler) *unstructured.Unstructured {
	unstructuredGvkForThirdPartyResourceType := &unstructured.Unstructured{}
	unstructuredGvkForThirdPartyResourceType.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    crdReconciler.Kind(),
		Group:   crdReconciler.Group(),
		Version: crdReconciler.Version(),
	})
	return unstructuredGvkForThirdPartyResourceType
}

func urlEncodePathSegment(s string) string {
	return url.PathEscape(
		// For now the Dash0 backend treats %2F the same as "/", so we need to replace forward slashes with
		// something other than %2F.
		// See https://stackoverflow.com/questions/71581828/gin-problem-accessing-url-encoded-path-param-containing-forward-slash
		strings.ReplaceAll(s, "/", "|"),
	)
}

func upsertViaApi(
	resourceReconciler ThirdPartyResourceReconciler,
	thirdPartyResource *unstructured.Unstructured,
) {
	// The create/update/delete/... events we receive from K8s are sequential per resource type, that is, we only
	// receive the next event for a Perses dashboard resource once we have processed the previous one; same for
	// Prometheus rules. However, we might receive events for different resource types concurrently, that is, one event
	// each for a Perses dashboard and a Prometheus rules might be processed concurrently. All third party resource
	// synchronization attempts end with writing to the Dash0 monitoring resource status in the same namespace. These
	// write attempts will fail if they happen concurrently. Therefore, we add all synchronization attempts to one
	// queue that is shared across resource types, and only process them one at a time.
	resourceReconciler.Queue().Add(
		ThirdPartyResourceSyncJob{
			resourceReconciler: resourceReconciler,
			thirdPartyResource: thirdPartyResource,
			action:             upsert,
			actionLabel:        "Creating/updating",
		},
	)
}

func deleteViaApi(
	resourceReconciler ThirdPartyResourceReconciler,
	thirdPartyResource *unstructured.Unstructured,
) {
	// See comment in upsertViaApi for an explanation why we use a shared queue for all resource types.
	resourceReconciler.Queue().Add(
		ThirdPartyResourceSyncJob{
			resourceReconciler: resourceReconciler,
			thirdPartyResource: thirdPartyResource,
			action:             delete,
			actionLabel:        "Deleting",
		},
	)
}

func StartProcessingThirdPartySynchronizationQueue(
	resourceReconcileQueue *workqueue.Typed[ThirdPartyResourceSyncJob],
	setupLog *logr.Logger,
) {
	setupLog.Info("Starting the third-party resource synchronization queue.")
	go func() {
		for {
			ctx := context.Background()
			logger := log.FromContext(ctx)
			item, queueShutdown := resourceReconcileQueue.Get()
			if queueShutdown {
				logger.Info("The third-party resource synchronization queue has been shut down.")
				return
			}
			logger.Info(
				fmt.Sprintf(
					"starting to process third party resource synchronization for %s %s/%s.",
					item.resourceReconciler.KindDisplayName(),
					item.thirdPartyResource.GetNamespace(),
					item.thirdPartyResource.GetName(),
				))

			synchronizeViaApi(
				ctx,
				item.resourceReconciler,
				item.thirdPartyResource,
				item.action,
				item.actionLabel,
				&logger,
			)
			logger.Info(
				fmt.Sprintf(
					"finished processing third party resource synchronization for %s %s/%s.",
					item.resourceReconciler.KindDisplayName(),
					item.thirdPartyResource.GetNamespace(),
					item.thirdPartyResource.GetName(),
				))
			resourceReconcileQueue.Done(item)
		}
	}()
}

func StopProcessingThirdPartySynchronizationQueue(
	resourceReconcileQueue *workqueue.Typed[ThirdPartyResourceSyncJob],
	logger *logr.Logger,
) {
	logger.Info("Shutting down the third-party resource synchronization queue.")
	resourceReconcileQueue.ShutDown()
}

func synchronizeViaApi(
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	thirdPartyResource *unstructured.Unstructured,
	action apiAction,
	actionLabel string,
	logger *logr.Logger,
) {
	preconditionChecksResult := validatePreconditions(
		ctx,
		resourceReconciler,
		thirdPartyResource,
		logger,
	)
	if !preconditionChecksResult.synchronizeResource {
		return
	}

	itemsTotal, httpRequests, validationIssues, synchronizationErrors :=
		resourceReconciler.MapResourceToHttpRequests(preconditionChecksResult, action, logger)

	if len(httpRequests) == 0 && len(validationIssues) == 0 && len(synchronizationErrors) == 0 {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s did not contain any %s, skipping.",
				resourceReconciler.KindDisplayName(),
				thirdPartyResource.GetNamespace(),
				thirdPartyResource.GetName(),
				resourceReconciler.ShortName(),
			))
	}

	var successfullySynchronized []string
	var httpErrors map[string]string
	if len(httpRequests) > 0 {
		successfullySynchronized, httpErrors =
			executeAllHttpRequests(resourceReconciler, httpRequests, actionLabel, logger)
	}
	if len(httpErrors) > 0 {
		if synchronizationErrors == nil {
			synchronizationErrors = make(map[string]string)
		}
		// In theory, the following map merge could overwrite synchronization errors from the MapResourceToHttpRequests
		// stage with errors occurring in executeAllHttpRequests, but items that have an error in
		// MapResourceToHttpRequests are never converted to requests, so the two maps are disjoint.
		maps.Copy(synchronizationErrors, httpErrors)
	}
	if len(validationIssues) == 0 && len(synchronizationErrors) == 0 {
		logger.Info(
			fmt.Sprintf("%s %s %s/%s: %d %s(s), %d successfully synchronized",
				actionLabel,
				resourceReconciler.KindDisplayName(),
				thirdPartyResource.GetNamespace(),
				thirdPartyResource.GetName(),
				itemsTotal,
				resourceReconciler.ShortName(),
				len(successfullySynchronized),
			))
	} else {
		logger.Error(
			fmt.Errorf("validation issues and/or synchronization issues occurred"),
			fmt.Sprintf("%s %s %s/%s: %d %s(s), %d successfully synchronized, validation issues: %v, synchronization errors: %v",
				actionLabel,
				resourceReconciler.KindDisplayName(),
				thirdPartyResource.GetNamespace(),
				thirdPartyResource.GetName(),
				itemsTotal,
				resourceReconciler.ShortName(),
				len(successfullySynchronized),
				validationIssues,
				synchronizationErrors,
			))
	}
	writeSynchronizationResult(
		ctx,
		resourceReconciler,
		preconditionChecksResult.monitoringResource,
		thirdPartyResource,
		itemsTotal,
		successfullySynchronized,
		validationIssues,
		synchronizationErrors,
		logger,
	)
}

func validatePreconditions(
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	thirdPartyResource *unstructured.Unstructured,
	logger *logr.Logger,
) *preconditionValidationResult {
	namespace := thirdPartyResource.GetNamespace()
	name := thirdPartyResource.GetName()

	monitoringRes, err := util.FindUniqueOrMostRecentResourceInScope(
		ctx,
		resourceReconciler.K8sClient(),
		thirdPartyResource.GetNamespace(),
		&dash0v1alpha1.Dash0Monitoring{},
		logger,
	)
	if err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"An error occurred when looking up the Dash0 monitoring resource in namespace %s while trying to synchronize the %s resource %s.",
				namespace,
				resourceReconciler.KindDisplayName(),
				name,
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}
	if monitoringRes == nil {
		logger.Info(
			fmt.Sprintf(
				"There is no Dash0 monitoring resource in namespace %s, will not synchronize the %s resource %s.",
				namespace,
				resourceReconciler.KindDisplayName(),
				name,
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}
	monitoringResource := monitoringRes.(*dash0v1alpha1.Dash0Monitoring)

	if !resourceReconciler.IsSynchronizationEnabled(monitoringResource) {
		logger.Info(
			fmt.Sprintf(
				"Synchronization for %ss is disabled via the settings of the Dash0 monitoring resource in namespace %s, will not synchronize the %s resource %s.",
				resourceReconciler.KindDisplayName(),
				namespace,
				resourceReconciler.ShortName(),
				name,
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	apiConfig := resourceReconciler.GetApiConfig().Load()
	authToken := resourceReconciler.GetAuthToken()
	if !isValidApiConfig(apiConfig) {
		logger.Info(
			fmt.Sprintf(
				"No Dash0 API endpoint has been provided via the operator configuration resource, "+
					"the %s(s) from %s/%s will not be updated in Dash0.",
				resourceReconciler.ShortName(),
				namespace,
				name,
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}
	if authToken == "" {
		logger.Info(
			fmt.Sprintf(
				"No Dash0 auth token is available, the %s(s) from %s/%s will not be updated in Dash0.",
				resourceReconciler.ShortName(),
				namespace,
				name,
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	dataset := apiConfig.Dataset
	if dataset == "" {
		dataset = util.DatasetDefault
	}

	specRaw := thirdPartyResource.Object["spec"]
	if specRaw == nil {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s has no spec, the %s(s) from will not be updated in Dash0.",
				resourceReconciler.KindDisplayName(),
				namespace,
				name,
				resourceReconciler.ShortName(),
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}
	spec, ok := specRaw.(map[string]interface{})
	if !ok {
		logger.Info(
			fmt.Sprintf(
				"The %s spec in %s/%s is not a map, the %s(s) will not be updated in Dash0.",
				resourceReconciler.KindDisplayName(),
				namespace,
				name,
				resourceReconciler.ShortName(),
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	return &preconditionValidationResult{
		synchronizeResource:    true,
		thirdPartyResourceSpec: spec,
		monitoringResource:     monitoringResource,
		authToken:              authToken,
		apiEndpoint:            apiConfig.Endpoint,
		dataset:                dataset,
		k8sNamespace:           namespace,
		k8sName:                name,
	}
}

// executeAllHttpRequests executes all HTTP requests in the given list and returns the names of the items that were
// successfully synchronized, as well as a map of name to error message for items that were rejected by the Dash0 API.
func executeAllHttpRequests(
	resourceReconciler ThirdPartyResourceReconciler,
	allRequests []HttpRequestWithItemName,
	actionLabel string,
	logger *logr.Logger,
) ([]string, map[string]string) {
	successfullySynchronized := make([]string, 0)
	httpErrors := make(map[string]string)
	for _, req := range allRequests {
		if err := executeSingleHttpRequestWithRetry(resourceReconciler, &req, actionLabel, logger); err != nil {
			httpErrors[req.ItemName] = err.Error()
		} else {
			successfullySynchronized = append(successfullySynchronized, req.ItemName)
		}
	}
	if len(successfullySynchronized) == 0 {
		successfullySynchronized = nil
	}
	return successfullySynchronized, httpErrors
}

func executeSingleHttpRequestWithRetry(
	resourceReconciler ThirdPartyResourceReconciler,
	req *HttpRequestWithItemName,
	actionLabel string,
	logger *logr.Logger,
) error {
	logger.Info(
		fmt.Sprintf(
			"%s %s \"%s\" at %s in Dash0",
			actionLabel,
			resourceReconciler.ShortName(),
			req.ItemName,
			req.Request.URL.String(),
		))

	return retry.OnError(
		wait.Backoff{
			Steps:    3,
			Duration: resourceReconciler.GetHttpRetryDelay(),
			Factor:   1.5,
		},
		func(err error) bool {
			var retryErr *retryableError
			if errors.As(err, &retryErr) {
				return retryErr.retryable
			}
			return false
		},
		func() error {
			return executeSingleHttpRequest(
				resourceReconciler,
				req,
				logger,
			)
		},
	)
}

func executeSingleHttpRequest(
	resourceReconciler ThirdPartyResourceReconciler,
	req *HttpRequestWithItemName,
	logger *logr.Logger,
) error {
	res, err := resourceReconciler.HttpClient().Do(req.Request)
	if err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"unable to execute the HTTP request to create/update/delete the %s \"%s\" at %s",
				resourceReconciler.ShortName(),
				req.ItemName,
				req.Request.URL.String(),
			))
		return &retryableError{
			err:       err,
			retryable: true,
		}
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		// HTTP status is not 2xx, treat this as an error
		// convertNon2xxStatusCodeToError will also consume and close the response body
		statusCodeError := convertNon2xxStatusCodeToError(resourceReconciler, req, res)
		retryableStatusCodeError := &retryableError{
			err: statusCodeError,
		}

		if res.StatusCode >= http.StatusBadRequest && res.StatusCode < http.StatusInternalServerError {
			// HTTP 4xx status codes are not retryable
			retryableStatusCodeError.retryable = false
			logger.Error(statusCodeError, "unexpected status code")
			return retryableStatusCodeError
		} else {
			// everything else, in particular HTTP 5xx status codes can be retried
			retryableStatusCodeError.retryable = true
			logger.Error(statusCodeError, "unexpected status code, request might be retried")
			return retryableStatusCodeError
		}
	}

	// HTTP status code was 2xx, discard the response body and close it
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()
	return nil
}

func convertNon2xxStatusCodeToError(
	resourceReconciler ThirdPartyResourceReconciler,
	req *HttpRequestWithItemName,
	res *http.Response,
) error {
	defer func() {
		_ = res.Body.Close()
	}()
	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		readBodyErr := fmt.Errorf("unable to read the API response payload after receiving status code %d when "+
			"trying to update/create/delete the %s \"%s\" at %s",
			res.StatusCode,
			resourceReconciler.ShortName(),
			req.ItemName,
			req.Request.URL.String(),
		)
		return readBodyErr
	}

	statusCodeErr := fmt.Errorf(
		"unexpected status code %d when updating/creating/deleting the %s \"%s\" at %s, response body is %s",
		res.StatusCode,
		resourceReconciler.ShortName(),
		req.ItemName,
		req.Request.URL.String(),
		string(responseBody),
	)
	return statusCodeErr
}

func writeSynchronizationResult(
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	monitoringResource *dash0v1alpha1.Dash0Monitoring,
	thirdPartyResource *unstructured.Unstructured,
	itemsTotal int,
	successfullySynchronized []string,
	validationIssuesPerItem map[string][]string,
	synchronizationErrorsPerItem map[string]string,
	logger *logr.Logger,
) {
	qualifiedName := fmt.Sprintf("%s/%s", thirdPartyResource.GetNamespace(), thirdPartyResource.GetName())

	result := dash0v1alpha1.Failed
	if len(successfullySynchronized) > 0 && len(validationIssuesPerItem) == 0 && len(synchronizationErrorsPerItem) == 0 {
		result = dash0v1alpha1.Successful
	} else if len(successfullySynchronized) > 0 {
		result = dash0v1alpha1.PartiallySuccessful
	}

	errAfterRetry := retry.OnError(
		wait.Backoff{
			Steps:    3,
			Duration: 1 * time.Second,
			Factor:   1.3,
		},
		func(err error) bool {
			return true
		},
		func() error {
			// re-fetch monitoring resource in case it has been modified since the start of the synchronization
			// operation
			if err := resourceReconciler.K8sClient().Get(
				ctx,
				types.NamespacedName{
					Namespace: monitoringResource.GetNamespace(),
					Name:      monitoringResource.GetName(),
				},
				monitoringResource); err != nil {
				logger.Info(
					fmt.Sprintf("failed attempt (might be retried) to fetch the Dash0 monitoring resource %s/%s "+
						"before updating it with the synchronization results for %s \"%s\": items total %d, "+
						"successfully synchronized: %v, validation issues: %v, synchronization errors: %v; "+
						"fetch error: %v",
						monitoringResource.GetNamespace(),
						monitoringResource.GetName(),
						resourceReconciler.ShortName(),
						qualifiedName,
						itemsTotal,
						successfullySynchronized,
						validationIssuesPerItem,
						synchronizationErrorsPerItem,
						err,
					))
				return err
			}
			resultForThisResource := resourceReconciler.UpdateSynchronizationResultsInStatus(
				monitoringResource,
				qualifiedName,
				result,
				itemsTotal,
				successfullySynchronized,
				synchronizationErrorsPerItem,
				validationIssuesPerItem,
			)
			if err := resourceReconciler.K8sClient().Status().Update(ctx, monitoringResource); err != nil {
				logger.Info(
					fmt.Sprintf("failed attempt (might be retried) to update the Dash0 monitoring resource "+
						"%s/%s with the synchronization results for %s \"%s\": %v; update error: %v",
						monitoringResource.GetNamespace(),
						monitoringResource.GetName(),
						resourceReconciler.ShortName(),
						qualifiedName,
						resultForThisResource,
						err,
					))
				return err
			}

			logger.Info(
				fmt.Sprintf("successfully updated the Dash0 monitoring resource %s/%s with the synchronization "+
					"results for %s \"%s\": %v",
					monitoringResource.GetNamespace(),
					monitoringResource.GetName(),
					resourceReconciler.ShortName(),
					qualifiedName,
					resultForThisResource,
				))
			return nil
		})

	if errAfterRetry != nil {
		logger.Error(
			errAfterRetry,
			fmt.Sprintf("finally failed (no more retries) to update the Dash0 monitoring resource %s/%s with the "+
				"synchronization results for %s \"%s\": items total %d, successfully synchronized: %v, validation "+
				"issues: %v, synchronization errors: %v",
				monitoringResource.GetNamespace(),
				monitoringResource.GetName(),
				resourceReconciler.ShortName(),
				qualifiedName,
				itemsTotal,
				successfullySynchronized,
				validationIssuesPerItem,
				synchronizationErrorsPerItem,
			))
	}
}
