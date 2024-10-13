// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"io"
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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/dash0hq/dash0-operator/internal/util"
)

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
	HttpClient() *http.Client
	MapResourceToHttpRequests(*preconditionValidationResult, apiAction, *logr.Logger) ([]*http.Request, error)
}

type ApiConfig struct {
	Endpoint string
	Dataset  string
}

type apiAction int

const (
	upsert apiAction = iota
	delete
)

type preconditionValidationResult struct {
	executeRequest bool
	authToken      string
	apiEndpoint    string
	dataset        string
	k8sNamespace   string
	k8sName        string
	spec           map[string]interface{}
}

var (
	retrySettings = wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.5,
		Steps:    3,
	}
)

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
	resourceReconciler ThirdPartyResourceReconciler,
	resource *unstructured.Unstructured,
	logger *logr.Logger,
) error {
	preconditionChecksResult := validatePreconditions(
		resourceReconciler,
		resource,
		logger,
	)
	if !preconditionChecksResult.executeRequest {
		return nil
	}

	httpRequests, err := resourceReconciler.MapResourceToHttpRequests(preconditionChecksResult, upsert, logger)
	if err != nil {
		return err
	}

	if len(httpRequests) == 0 {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s did not contain any %s, skipping.",
				resourceReconciler.KindDisplayName(),
				resource.GetNamespace(),
				resource.GetName(),
				resourceReconciler.ShortName(),
			))
	}

	return executeAllHttpRequests(resourceReconciler, httpRequests, "Creating/updating", logger)
}

func deleteViaApi(
	resourceReconciler ThirdPartyResourceReconciler,
	resource *unstructured.Unstructured,
	logger *logr.Logger,
) error {
	preconditionChecksResult := validatePreconditions(
		resourceReconciler,
		resource,
		logger,
	)
	if !preconditionChecksResult.executeRequest {
		return nil
	}

	httpRequests, err := resourceReconciler.MapResourceToHttpRequests(preconditionChecksResult, delete, logger)
	if err != nil {
		return err
	}

	if len(httpRequests) == 0 {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s did not contain any %s, skipping.",
				resourceReconciler.KindDisplayName(),
				resource.GetNamespace(),
				resource.GetName(),
				resourceReconciler.ShortName(),
			))
	}

	return executeAllHttpRequests(resourceReconciler, httpRequests, "Deleting", logger)
}

func validatePreconditions(
	resourceReconciler ThirdPartyResourceReconciler,
	resource *unstructured.Unstructured,
	logger *logr.Logger,
) *preconditionValidationResult {
	namespace := resource.GetNamespace()
	name := resource.GetName()

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
			executeRequest: false,
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
			executeRequest: false,
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
			executeRequest: false,
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
			executeRequest: false,
		}
	}

	return &preconditionValidationResult{
		executeRequest: true,
		authToken:      authToken,
		apiEndpoint:    apiConfig.Endpoint,
		dataset:        dataset,
		k8sNamespace:   namespace,
		k8sName:        name,
		spec:           spec,
	}
}

func executeAllHttpRequests(
	resourceReconciler ThirdPartyResourceReconciler,
	allRequests []*http.Request,
	actionLabel string,
	logger *logr.Logger,
) error {
	for _, req := range allRequests {
		if err := executeSingleHttpRequest(resourceReconciler, req, actionLabel, logger); err != nil {
			return err
		}
	}
	return nil
}

func executeSingleHttpRequest(
	resourceReconciler ThirdPartyResourceReconciler,
	req *http.Request,
	actionLabel string,
	logger *logr.Logger,
) error {
	logger.Info(
		fmt.Sprintf(
			"%s %s at %s in Dash0",
			actionLabel,
			resourceReconciler.ShortName(),
			req.URL.String(),
		))
	res, err := resourceReconciler.HttpClient().Do(req)
	if err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"unable to execute the HTTP request to create/update/delete the %s at %s",
				resourceReconciler.ShortName(),
				req.URL.String(),
			))
		return err
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return handleNon2xxStatusCode(resourceReconciler, req, res, logger)
	}

	// http status code was 2xx, discard the response body and close it
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	return nil
}

func handleNon2xxStatusCode(
	resourceReconciler ThirdPartyResourceReconciler,
	req *http.Request,
	res *http.Response,
	logger *logr.Logger,
) error {
	defer func() {
		_ = res.Body.Close()
	}()
	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		readBodyErr := fmt.Errorf("unable to read the API response payload after receiving status code %d when "+
			"trying to udpate/create/delete the %s at %s",
			res.StatusCode,
			resourceReconciler.ShortName(),
			req.URL.String(),
		)
		logger.Error(readBodyErr, "unable to read the API response payload")
		return readBodyErr
	}

	statusCodeErr := fmt.Errorf(
		"unexpected status code %d when updating/creating/deleting the %s at %s, response body is %s",
		res.StatusCode,
		resourceReconciler.ShortName(),
		req.URL.String(),
		string(responseBody),
	)
	logger.Error(statusCodeErr, "unexpected status code")
	return statusCodeErr
}
