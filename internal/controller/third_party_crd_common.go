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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type ApiConfig struct {
	Endpoint string
	Dataset  string
}

type ApiClient interface {
	SetApiEndpointAndDataset(*ApiConfig, *logr.Logger)
	RemoveApiEndpointAndDataset()
}

type ThirdPartyCrdReconciler interface {
	handler.TypedEventHandler[client.Object, reconcile.Request]
	reconcile.TypedReconciler[reconcile.Request]

	Manager() ctrl.Manager
	GetAuthToken() string
	KindDisplayName() string
	Group() string
	Kind() string
	Version() string
	QualifiedKind() string
	ControllerName() string
	DoesCrdExist() *atomic.Bool
	SetCrdExists(bool)
	SkipNameValidation() bool
	CreateResourceReconciler(types.UID, string, *http.Client)
	ResourceReconciler() ThirdPartyResourceReconciler
}

type ThirdPartyResourceReconciler interface {
	handler.TypedEventHandler[client.Object, reconcile.Request]
	reconcile.TypedReconciler[reconcile.Request]

	KindDisplayName() string
	ShortName() string
	IsWatching() *atomic.Bool
	SetIsWatching(bool)
	GetAuthToken() string
	GetApiConfig() *atomic.Pointer[ApiConfig]
	ControllerName() string
	K8sClient() client.Client
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
		succesfullySynchronized []string,
		synchronizationErrorsPerItem map[string]string,
		validationIssuesPerItem map[string][]string,
	) interface{}
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
	synchronizeResource bool
	monitoringResource  *dash0v1alpha1.Dash0Monitoring
	authToken           string
	apiEndpoint         string
	dataset             string
	k8sNamespace        string
	k8sName             string
	spec                map[string]interface{}
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
	authToken := crdReconciler.GetAuthToken()
	if authToken == "" {
		logger.Info(fmt.Sprintf("No Dash0 auth token has been provided via the operator configuration resource. "+
			"The operator will not watch for %s resources.", crdReconciler.KindDisplayName()))
		return nil
	}

	kubeSystemNamespace := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "kube-system"}, kubeSystemNamespace); err != nil {
		msg := "unable to get the kube-system namespace uid"
		logger.Error(err, msg)
		return fmt.Errorf("%s: %w", msg, err)
	}

	crdReconciler.CreateResourceReconciler(
		kubeSystemNamespace.UID,
		authToken,
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
	if crdReconciler.ResourceReconciler().IsWatching().Load() {
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

	apiConfig := crdReconciler.ResourceReconciler().GetApiConfig().Load()
	if !isValidApiConfig(apiConfig) {
		if !isStartup {
			// Silently ignore this missing precondition if it happens during the startup of the operator. It will
			// be remedied automatically once the operator configuration resource is reconciled for the first time.
			logger.Info(
				fmt.Sprintf(
					"The %s custom resource definition is present in this cluster, but no Dash0 API endpoint been "+
						"provided via the operator configuration resource, or the operator configuration resource "+
						"has not been reconciled yet. The operator will not watch for %s resources. "+
						"(If there is an operator configuration resource with an API endpoint present in the "+
						"cluster, it will be reconciled in a few seconds and this message can be safely ignored.)",
					crdReconciler.QualifiedKind(),
					crdReconciler.KindDisplayName(),
				))
		}
		return
	}

	logger.Info(
		fmt.Sprintf(
			"The %s custom resource definition is present in this cluster, and a Dash0 API endpoint has been provided. "+
				"The operator will watch for %s resources.",
			crdReconciler.QualifiedKind(),
			crdReconciler.KindDisplayName(),
		),
	)
	startWatchingThirdPartyResources(crdReconciler, logger)
}

func startWatchingThirdPartyResources(
	crdReconciler ThirdPartyCrdReconciler,
	logger *logr.Logger,
) {
	logger.Info(fmt.Sprintf("Setting up a watch for %s custom resources.", crdReconciler.KindDisplayName()))

	unstructuredGvkForPersesDashboards := &unstructured.Unstructured{}
	unstructuredGvkForPersesDashboards.SetGroupVersionKind(schema.GroupVersionKind{
		Kind:    crdReconciler.Kind(),
		Group:   crdReconciler.Group(),
		Version: crdReconciler.Version(),
	})

	resourceReconciler := crdReconciler.ResourceReconciler()
	controllerBuilder := ctrl.NewControllerManagedBy(crdReconciler.Manager()).
		Named(resourceReconciler.ControllerName()).
		Watches(
			unstructuredGvkForPersesDashboards,
			// Deliberately not using a convenience mechanism like &handler.EnqueueRequestForObject{} (which would
			// feed all events into the Reconcile method) here, since using the lower-level TypedEventHandler interface
			// directly allows us to distinguish between create and delete events more easily.
			resourceReconciler,
		)
	if crdReconciler.SkipNameValidation() {
		controllerBuilder = controllerBuilder.WithOptions(controller.TypedOptions[reconcile.Request]{
			SkipNameValidation: ptr.To(true),
		})
	}
	if err := controllerBuilder.Complete(resourceReconciler); err != nil {
		logger.Error(err, "unable to create a new controller for watching Perses dashboards")
		return
	}
	resourceReconciler.SetIsWatching(true)
}

func isValidApiConfig(apiConfig *ApiConfig) bool {
	return apiConfig != nil && apiConfig.Endpoint != ""
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
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	resource *unstructured.Unstructured,
	logger *logr.Logger,
) {
	synchronizeViaApi(
		ctx,
		resourceReconciler,
		resource,
		upsert,
		"Creating/updating",
		logger,
	)
}

func deleteViaApi(
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	resource *unstructured.Unstructured,
	logger *logr.Logger,
) {
	synchronizeViaApi(
		ctx,
		resourceReconciler,
		resource,
		delete,
		"Deleting",
		logger,
	)
}

func synchronizeViaApi(
	ctx context.Context,
	resourceReconciler ThirdPartyResourceReconciler,
	resource *unstructured.Unstructured,
	action apiAction,
	actionLabel string,
	logger *logr.Logger,
) {
	preconditionChecksResult := validatePreconditions(
		ctx,
		resourceReconciler,
		resource,
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
				resource.GetNamespace(),
				resource.GetName(),
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
		// stage with errors occurring in executeAllHttpRequests, but items that have an error in MapResourceToHttpRequests
		// are never converted to requests, so the two maps are disjoint.
		maps.Copy(synchronizationErrors, httpErrors)
	}
	logger.Info(
		fmt.Sprintf("%s %s %s/%s: %d %s(s), %d successfully synchronized, validation issues: %v, synchronization errors: %v",
			actionLabel,
			resourceReconciler.KindDisplayName(),
			resource.GetNamespace(),
			resource.GetName(),
			itemsTotal,
			resourceReconciler.ShortName(),
			len(successfullySynchronized),
			validationIssues,
			synchronizationErrors,
		))
	writeSynchronizationResult(
		ctx,
		resourceReconciler,
		preconditionChecksResult.monitoringResource,
		resource,
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
	resource *unstructured.Unstructured,
	logger *logr.Logger,
) *preconditionValidationResult {
	namespace := resource.GetNamespace()
	name := resource.GetName()

	monitoringRes, err := util.FindUniqueOrMostRecentResourceInScope(
		ctx,
		resourceReconciler.K8sClient(),
		resource.GetNamespace(),
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

	authToken := resourceReconciler.GetAuthToken()
	if authToken == "" {
		logger.Info(
			fmt.Sprintf(
				"No auth token is set on the controller deployment, the %s(s) from %s/%s not be updated in Dash0.",
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

	specRaw := resource.Object["spec"]
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
		synchronizeResource: true,
		monitoringResource:  monitoringResource,
		authToken:           authToken,
		apiEndpoint:         apiConfig.Endpoint,
		dataset:             dataset,
		k8sNamespace:        namespace,
		k8sName:             name,
		spec:                spec,
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
			"trying to udpate/create/delete the %s \"%s\" at %s",
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
	resource *unstructured.Unstructured,
	itemsTotal int,
	succesfullySynchronized []string,
	validationIssuesPerItem map[string][]string,
	synchronizationErrorsPerItem map[string]string,
	logger *logr.Logger,
) {
	qualifiedName := fmt.Sprintf("%s/%s", resource.GetNamespace(), resource.GetName())

	result := dash0v1alpha1.Failed
	if len(succesfullySynchronized) > 0 && len(validationIssuesPerItem) == 0 && len(synchronizationErrorsPerItem) == 0 {
		result = dash0v1alpha1.Successful
	} else if len(succesfullySynchronized) > 0 {
		result = dash0v1alpha1.PartiallySuccessful
	}

	resultForThisResource := resourceReconciler.UpdateSynchronizationResultsInStatus(
		monitoringResource,
		qualifiedName,
		result,
		itemsTotal,
		succesfullySynchronized,
		synchronizationErrorsPerItem,
		validationIssuesPerItem,
	)

	if err := resourceReconciler.K8sClient().Status().Update(ctx, monitoringResource); err != nil {
		logger.Error(
			err,
			fmt.Sprintf("failed to update the Dash0 monitoring resource in namespace %s with the synchronization results for %s \"%s\": %v",
				resource.GetNamespace(),
				resourceReconciler.ShortName(),
				qualifiedName,
				resultForThisResource,
			))
	}
}
