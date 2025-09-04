// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
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

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

// ThirdPartyCrdReconciler is an interface for reconcilers that act on CRDs of third-party resource types (i.e. when a
// particular CRD is deployed to the cluster or removed).
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
	OperatorManagerIsLeader() bool
	CreateThirdPartyResourceReconciler(types.UID)
	ThirdPartyResourceReconciler() ThirdPartyResourceReconciler
}

// ThirdPartyResourceReconciler extends the ApiSyncReconciler interface with methods that are specific to
// reconcilers for third-party resource types (like Perses dashboards or Prometheus rules).
type ThirdPartyResourceReconciler interface {
	ApiSyncReconciler

	handler.TypedEventHandler[*unstructured.Unstructured, reconcile.Request]
	reconcile.TypedReconciler[reconcile.Request]

	IsSynchronizationEnabled(*dash0v1beta1.Dash0Monitoring) bool
	ControllerStopFunctionLock() *sync.Mutex
	GetControllerStopFunction() *context.CancelFunc
	SetControllerStopFunction(*context.CancelFunc)
	IsWatching() bool

	Queue() *workqueue.Typed[ThirdPartyResourceSyncJob]

	// FetchExistingResourceIdsRequest creates an HTTP request for retrieving the existing IDs from the Dash0
	// API for a given Kubernetes resource.
	// FetchExistingResourceIdsRequest is only used for resource types where one Kubernetes resource (say, a
	// PrometheusRule) is potentially associated with multiple Dash0 api objects (multiple checks). Controllers
	// which manage objects with a one-to-one relation (like Perses dashboards) should return nil, nil.
	FetchExistingResourceIdsRequest(*preconditionValidationResult) (*http.Request, error)

	// CreateDeleteRequests produces an HTTP DELETE requests for the resources that still exist in Dash0, but should
	// not. It does so by comparing the list of IDs of objects that exist in the Dash0 backend with the list
	// of IDs found in a given Kubernetes resource. This mechanism is only used for resource types where one Kubernetes
	// resource (say, a PrometheusRule) is potentially associated with multiple Dash0 api objects (multiple checks).
	// Controllers which manage objects with a one-to-one relation (like Perses dashboards, synthetic checks, view)
	// should return nil, nil.
	CreateDeleteRequests(*preconditionValidationResult, []string, []string, *logr.Logger) ([]HttpRequestWithItemName, map[string]string)

	// UpdateSynchronizationResultsInDash0MonitoringStatus Modifies the status of the provided Dash0Monitoring resource
	// to reflect the results of the synchronization operation for one third-party Kubernetes resource.
	UpdateSynchronizationResultsInDash0MonitoringStatus(
		monitoringResource *dash0v1beta1.Dash0Monitoring,
		qualifiedName string,
		status dash0common.ThirdPartySynchronizationStatus,
		itemsTotal int,
		successfullySynchronized []string,
		synchronizationErrorsPerItem map[string]string,
		validationIssuesPerItem map[string][]string,
	) interface{}
}

type ThirdPartyResourceSyncJob struct {
	thirdPartyResourceReconciler ThirdPartyResourceReconciler
	dash0ApiResource             *unstructured.Unstructured
	action                       apiAction
}

// SetupThirdPartyCrdReconcilerWithManager sets up a ThirdPartyCrdReconciler with the provided manager. It establishes
// watches for the relevant CRD and starts/stops watching for the corresponding third-party resources as needed.
// In particular, it
//   - creates an ThirdPartyResourceReconciler for the respective third-party resource type,
//   - checks if the relevant third-party CRD already exists in the cluster, and if so, calls
//     maybeStartWatchingThirdPartyResources,
//   - otherwise, maybeStartWatchingThirdPartyResources will be called later on different trigger points, for example
//     when the third-party CRD is created in the cluster, or when a Dash0 API token is provided to the
//     ThirdPartyCrdReconciler.
//
// See function maybeStartWatchingThirdPartyResources for further details on the startup process.
func SetupThirdPartyCrdReconcilerWithManager(
	ctx context.Context,
	k8sClient client.Client,
	crdReconciler ThirdPartyCrdReconciler,
	logger *logr.Logger,
) error {
	pseudoClusterUid, err := util.ReadPseudoClusterUidOrFail(ctx, k8sClient, logger)
	if err != nil {
		return err
	}
	crdReconciler.CreateThirdPartyResourceReconciler(pseudoClusterUid)

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
		maybeStartWatchingThirdPartyResources(crdReconciler, logger)
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

// maybeStartWatchingThirdPartyResources is called by a variety of triggers, for example
// - when the third-party CRD is created in the cluster,
// - when this operator manager replica is elected as leader,
// - when the Dash0 API endpoint URL is provided, or
// - when a Dash0 API token is provided.
//
// It then checks if all prerequisites (which are the same conditions that are listed as triggers above) for starting a
// watch for the respective third-party resource type are fulfilled. If so, it creates and starts the reconciler for
// the third-party resource type.
func maybeStartWatchingThirdPartyResources(
	crdReconciler ThirdPartyCrdReconciler,
	logger *logr.Logger,
) {
	resourceReconciler := crdReconciler.ThirdPartyResourceReconciler()

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
		if !crdReconciler.OperatorManagerIsLeader() {
			logger.Info(
				fmt.Sprintf(
					"The %s custom resource definition is present in this cluster, but this operator manager replica "+
						"has not (or not yet) been elected as leader. The operator will not watch for %s resources. "+
						"(This might change once this operator manager replica becomes leader.)",
					crdReconciler.QualifiedKind(),
					crdReconciler.KindDisplayName(),
				))
		} else {
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
	resourceReconciler := crdReconciler.ThirdPartyResourceReconciler()
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

func upsertViaApi(
	thirdPartyResourceReconciler ThirdPartyResourceReconciler,
	dash0ApiResource *unstructured.Unstructured,
) {
	// The create/update/delete/... events we receive from K8s are sequential per resource type, that is, we only
	// receive the next event for a Perses dashboard resource once we have processed the previous one; same for
	// Prometheus rules. However, we might receive events for different resource types concurrently, that is, one event
	// each for a Perses dashboard and a Prometheus rules might be processed concurrently. All resource synchronization
	// attempts that are related to third-party resources types (Prometheus rules, Perses dashboards) end with writing
	// to the Dash0 monitoring resource status in the same namespace. These write attempts will fail if they happen
	// concurrently. Therefore, we add all synchronization attempts to one queue that is shared across resource types,
	// and only process them one at a time.
	thirdPartyResourceReconciler.Queue().Add(
		ThirdPartyResourceSyncJob{
			thirdPartyResourceReconciler: thirdPartyResourceReconciler,
			dash0ApiResource:             dash0ApiResource,
			action:                       upsert,
		},
	)
}

func deleteViaApi(
	thirdPartyResourceReconciler ThirdPartyResourceReconciler,
	dash0ApiResource *unstructured.Unstructured,
) {
	// See comment in upsertViaApi for an explanation why we use a shared queue for all resource types.
	thirdPartyResourceReconciler.Queue().Add(
		ThirdPartyResourceSyncJob{
			thirdPartyResourceReconciler: thirdPartyResourceReconciler,
			dash0ApiResource:             dash0ApiResource,
			action:                       delete,
		},
	)
}

func StartProcessingThirdPartySynchronizationQueue(
	resourceReconcileQueue *workqueue.Typed[ThirdPartyResourceSyncJob],
	setupLog *logr.Logger,
) {
	setupLog.Info("Starting the Dash0 API resource synchronization queue.")
	go func() {
		for {
			ctx := context.Background()
			logger := log.FromContext(ctx)
			item, queueShutdown := resourceReconcileQueue.Get()
			if queueShutdown {
				logger.Info("The Dash0 API resource synchronization queue has been shut down.")
				return
			}
			logger.Info(
				fmt.Sprintf(
					"starting to process Dash0 API resource synchronization for %s %s/%s.",
					item.thirdPartyResourceReconciler.KindDisplayName(),
					item.dash0ApiResource.GetNamespace(),
					item.dash0ApiResource.GetName(),
				))

			synchronizeViaApiAndUpdateStatus(
				ctx,
				item.thirdPartyResourceReconciler,
				item.dash0ApiResource,
				nil,
				item.action,
				&logger,
			)
			logger.Info(
				fmt.Sprintf(
					"finished processing Dash0 API resource synchronization for %s %s/%s.",
					item.thirdPartyResourceReconciler.KindDisplayName(),
					item.dash0ApiResource.GetNamespace(),
					item.dash0ApiResource.GetName(),
				))
			resourceReconcileQueue.Done(item)
		}
	}()
}

func StopProcessingThirdPartySynchronizationQueue(
	resourceReconcileQueue *workqueue.Typed[ThirdPartyResourceSyncJob],
	logger *logr.Logger,
) {
	logger.Info("Shutting down the Dash0 API resource synchronization queue.")
	resourceReconcileQueue.ShutDown()
}

func writeSynchronizationResultToDash0MonitoringStatus(
	ctx context.Context,
	thirdPartyResourceReconciler ThirdPartyResourceReconciler,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	thirdPartyResource *unstructured.Unstructured,
	itemsTotal int,
	successfullySynchronized []string,
	validationIssuesPerItem map[string][]string,
	synchronizationErrorsPerItem map[string]string,
	logger *logr.Logger,
) {
	qualifiedName := fmt.Sprintf("%s/%s", thirdPartyResource.GetNamespace(), thirdPartyResource.GetName())

	result := dash0common.ThirdPartySynchronizationStatusFailed
	if len(successfullySynchronized) > 0 && len(validationIssuesPerItem) == 0 && len(synchronizationErrorsPerItem) == 0 {
		result = dash0common.ThirdPartySynchronizationStatusSuccessful
	} else if len(successfullySynchronized) > 0 {
		result = dash0common.ThirdPartySynchronizationStatusPartiallySuccessful
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
			if err := thirdPartyResourceReconciler.K8sClient().Get(
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
						thirdPartyResourceReconciler.ShortName(),
						qualifiedName,
						itemsTotal,
						successfullySynchronized,
						validationIssuesPerItem,
						synchronizationErrorsPerItem,
						err,
					))
				return err
			}
			resultForThisResource := thirdPartyResourceReconciler.UpdateSynchronizationResultsInDash0MonitoringStatus(
				monitoringResource,
				qualifiedName,
				result,
				itemsTotal,
				successfullySynchronized,
				synchronizationErrorsPerItem,
				validationIssuesPerItem,
			)
			if err := thirdPartyResourceReconciler.K8sClient().Status().Update(ctx, monitoringResource); err != nil {
				logger.Info(
					fmt.Sprintf("failed attempt (might be retried) to update the Dash0 monitoring resource "+
						"%s/%s with the synchronization results for %s \"%s\": %v; update error: %v",
						monitoringResource.GetNamespace(),
						monitoringResource.GetName(),
						thirdPartyResourceReconciler.ShortName(),
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
					thirdPartyResourceReconciler.ShortName(),
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
				thirdPartyResourceReconciler.ShortName(),
				qualifiedName,
				itemsTotal,
				successfullySynchronized,
				validationIssuesPerItem,
				synchronizationErrorsPerItem,
			))
	}
}
