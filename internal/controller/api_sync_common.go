// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/resources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ApiConfig struct {
	Endpoint string
	Dataset  string
}

type ApiClient interface {
	SetApiEndpointAndDataset(context.Context, *ApiConfig, *logr.Logger)
	RemoveApiEndpointAndDataset(context.Context, *logr.Logger)
}

// ApiSyncReconciler is the common interface for reconcilers that synchonize their Kubernetes resources to the Dash0
// API. This can either be resource types owned by the Dash0 operator (like Dash0SyntheticCheck), which then also
// implement the OwnedResourceReconciler interface, or third-party resource types (like PrometheusRule or
// PersesDashboard), which implement the ThirdPartyResourceReconciler interface.
type ApiSyncReconciler interface {
	KindDisplayName() string
	ShortName() string
	GetAuthToken() string
	GetApiConfig() *atomic.Pointer[ApiConfig]
	ControllerName() string
	K8sClient() client.Client
	HttpClient() *http.Client
	GetHttpRetryDelay() time.Duration

	// MapResourceToHttpRequests converts a Kubernetes resource object to a list of HTTP requests that can be sent to
	// the Dash0 API. It returns:
	// - the total number of eligible items in the Kubernetes resource,
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
		[]string,
		map[string][]string,
		map[string]string,
	)

	ExtractIdOriginAndLinkFromResponseBody(
		responseBytes []byte,
		logger *logr.Logger,
	) Dash0ApiObjectLabels
}

// OwnedResourceReconciler extends the ApiSyncReconciler interface with methods that are specific to resource types
// owned by the Dash0 operator (like Dash0SyntheticCheck, Dash0View).
type OwnedResourceReconciler interface {
	ApiSyncReconciler

	// WriteSynchronizationResultToSynchronizedResource writes the result of a synchronization attempt to the status
	// of the Kubernetes resource that has been synchronized. This is only supported for resource types owned by the
	// Dash0 operator, not for third-party resource types.
	WriteSynchronizationResultToSynchronizedResource(
		context.Context,
		client.Object,
		dash0common.Dash0ApiResourceSynchronizationStatus,
		Dash0ApiObjectLabels,
		[]string,
		string,
		*logr.Logger,
	)
}

type HttpRequestWithItemName struct {
	ItemName string
	Request  *http.Request
}

type Dash0ApiObjectWithOrigin struct {
	Origin string `json:"origin"`
}

type Dash0ApiObjectWithMetadata struct {
	Metadata Dash0ApiObjectMetadata `json:"metadata"`
}

type Dash0ApiObjectMetadata struct {
	Labels Dash0ApiObjectLabels `json:"labels"`
}

type Dash0ApiObjectLabels struct {
	Id      string `json:"dash0.com/id,omitempty"`
	Origin  string `json:"dash0.com/origin,omitempty"`
	Dataset string `json:"dash0.com/dataset"`
}

type Dash0DashboardResponse struct {
	Metadata Dash0DashboardMetadata `json:"metadata"`
}

type Dash0DashboardMetadata struct {
	Dash0Extensions Dash0ApiResponseWithOriginAsId `json:"dash0Extensions"`
}

type Dash0ApiResponseWithOriginAsId struct {
	Origin  string `json:"id"`
	Dataset string `json:"dataset"`
}

type SuccessfulSynchronizationResult struct {
	ItemName string
	Labels   Dash0ApiObjectLabels
}

type apiAction int

const (
	upsertAction apiAction = iota
	deleteAction
)

type preconditionValidationResult struct {
	// synchronizeResource = false means no sync action whatsoever will happen for this resource, usually because a
	// precondition for synchronizing resources is not met (no API token etc.)
	synchronizeResource bool

	// syncDisabledViaLabel = false means that the resource has the label dash0.com/enable=false set; this will turn
	// any create/update event for the object into a delete request (for most purposes it would be enough to ignore
	// resources with that label, but when a resource has been synchronized earlier, and then the dash0.com/enable=false
	// is added after the fact, we need to delete the object in Dash0 to get back into a consistent state)
	syncDisabledViaLabel bool

	// resource is the full resource that is being reconciled, as a map
	resource map[string]interface{}

	// dash0ApiResourceSpec is spec part of the resource that is being reconciled, as a map
	dash0ApiResourceSpec map[string]interface{}

	// monitoringResource is the Dash0 monitoring resource that was found in the same namespace as the resource
	monitoringResource *dash0v1beta1.Dash0Monitoring

	// authToken is the Dash0 auth token that will be used to sync the Dash0 API resource
	authToken string

	// apiEndpoint is the Dash0 API endpoint that will be used to sync the Dash0 API resource
	apiEndpoint string

	// dataset is the Dash0 dataset into which the Dash0 API resource will be synchronized
	dataset string

	// k8sNamespace is Kubernetes namespace in which the Dash0 API resource has been found
	k8sNamespace string

	// k8sNamespace is Kubernetes name of the Dash0 API resource
	k8sName string
}

func synchronizeViaApiAndUpdateStatus(
	ctx context.Context,
	apiSyncReconciler ApiSyncReconciler,
	dash0ApiResource *unstructured.Unstructured,
	ownedResource client.Object,
	action apiAction,
	logger *logr.Logger,
) {
	preconditionChecksResult := validatePreconditions(
		ctx,
		apiSyncReconciler,
		dash0ApiResource,
		logger,
	)
	if !preconditionChecksResult.synchronizeResource {
		return
	}

	resourceHasBeenDeleted := action == deleteAction

	if preconditionChecksResult.syncDisabledViaLabel {
		// The resource has the label dash0.com/enable=false set; thus we override the API action unconditionally with
		// delete, that is, we ask MapResourceToHttpRequests to create HTTP DELETE requests. For most purposes it would
		// be enough to ignore resources with that label entirely and issue no HTTP requests, but when a resource has
		// been synchronized earlier, and then the dash0.com/enable=false is added after the fact, we need to delete the
		// object in Dash0 to get back into a consistent state.
		action = deleteAction
	}

	var existingOriginsFromApi []string
	if action != deleteAction {
		var err error
		existingOriginsFromApi, err = fetchExistingOrigins(apiSyncReconciler, preconditionChecksResult, logger)
		if err != nil {
			// the error has already been logged in fetchExistingOrigins, but we need to record the failure in the status of
			// the monitoring resource
			writeSynchronizationResult(
				ctx,
				apiSyncReconciler,
				preconditionChecksResult.monitoringResource,
				dash0ApiResource,
				ownedResource,
				0,
				nil,
				map[string][]string{},
				map[string]string{"*": err.Error()},
				false,
				logger,
			)
			return
		}
	}

	itemsTotal, httpRequests, originsInResource, validationIssues, synchronizationErrors :=
		apiSyncReconciler.MapResourceToHttpRequests(preconditionChecksResult, action, logger)
	if action != deleteAction {
		itemsTotal, httpRequests = addDeleteRequestsForObjectsThatHaveBeenDeletedInTheKubernetesResource(
			apiSyncReconciler,
			preconditionChecksResult,
			existingOriginsFromApi,
			originsInResource,
			httpRequests,
			itemsTotal,
			synchronizationErrors,
			logger,
		)
	}
	if len(httpRequests) == 0 && len(validationIssues) == 0 && len(synchronizationErrors) == 0 {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s did not contain any %s, skipping.",
				apiSyncReconciler.KindDisplayName(),
				dash0ApiResource.GetNamespace(),
				dash0ApiResource.GetName(),
				apiSyncReconciler.ShortName(),
			))
	}

	var successfullySynchronized []SuccessfulSynchronizationResult
	var httpErrors map[string]string
	if len(httpRequests) > 0 {
		successfullySynchronized, httpErrors =
			executeAllHttpRequests(apiSyncReconciler, httpRequests, logger)
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
			fmt.Sprintf("%s %s/%s: %d %s(s), %d successfully synchronized",
				apiSyncReconciler.KindDisplayName(),
				dash0ApiResource.GetNamespace(),
				dash0ApiResource.GetName(),
				itemsTotal,
				apiSyncReconciler.ShortName(),
				len(successfullySynchronized),
			))
	} else {
		logger.Error(
			fmt.Errorf("validation issues and/or synchronization issues occurred"),
			fmt.Sprintf("%s %s/%s: %d %s(s), %d successfully synchronized, validation issues: %v, synchronization errors: %v",
				apiSyncReconciler.KindDisplayName(),
				dash0ApiResource.GetNamespace(),
				dash0ApiResource.GetName(),
				itemsTotal,
				apiSyncReconciler.ShortName(),
				len(successfullySynchronized),
				validationIssues,
				synchronizationErrors,
			))
	}
	writeSynchronizationResult(
		ctx,
		apiSyncReconciler,
		preconditionChecksResult.monitoringResource,
		dash0ApiResource,
		ownedResource,
		itemsTotal,
		successfullySynchronized,
		validationIssues,
		synchronizationErrors,
		resourceHasBeenDeleted,
		logger,
	)
}

func validatePreconditions(
	ctx context.Context,
	apiSyncReconciler ApiSyncReconciler,
	dash0ApiResource *unstructured.Unstructured,
	logger *logr.Logger,
) *preconditionValidationResult {
	namespace := dash0ApiResource.GetNamespace()
	name := dash0ApiResource.GetName()

	monitoringRes, err := resources.FindUniqueOrMostRecentResourceInScope(
		ctx,
		apiSyncReconciler.K8sClient(),
		dash0ApiResource.GetNamespace(),
		&dash0v1beta1.Dash0Monitoring{},
		logger,
	)
	if err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"An error occurred when looking up the Dash0 monitoring resource in namespace %s while trying to synchronize the %s resource %s.",
				namespace,
				apiSyncReconciler.KindDisplayName(),
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
				apiSyncReconciler.KindDisplayName(),
				name,
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}
	monitoringResource := monitoringRes.(*dash0v1beta1.Dash0Monitoring)

	if thirdPartyResourceReconciler, ok := apiSyncReconciler.(ThirdPartyResourceReconciler); ok {
		// Only third-party resource reconcilers (like PrometheusRule, PersesDashboard) support disabling
		// synchronization.
		// Our own CRD types have no need for a configuration option in the Dash0 monitoring resource to disable their
		// synchronization for an entire namespace: There is no risk of accidentally having Dash0 synthetic checks or
		// Dash0 view resources in a namespace for other purposes, which a user does not want to synchronize with the
		// Dash0 API. For third-party resource types, this can happen, maybe users have PrometheusRule resources in
		// a namespace for the Prometheus operator or Perses dashboards for the Perses operator, but do not want to
		// synchronize them with Dash0.
		if !thirdPartyResourceReconciler.IsSynchronizationEnabled(monitoringResource) {
			logger.Info(
				fmt.Sprintf(
					"Synchronization for %ss is disabled via the settings of the Dash0 monitoring resource in namespace %s, will not synchronize the %s resource %s.",
					thirdPartyResourceReconciler.KindDisplayName(),
					namespace,
					thirdPartyResourceReconciler.ShortName(),
					name,
				))
			return &preconditionValidationResult{
				synchronizeResource: false,
			}
		}
	}

	apiConfig := apiSyncReconciler.GetApiConfig().Load()
	authToken := apiSyncReconciler.GetAuthToken()
	if !isValidApiConfig(apiConfig) {
		logger.Info(
			fmt.Sprintf(
				"No Dash0 API endpoint has been provided via the operator configuration resource, "+
					"the %s(s) from %s/%s will not be updated in Dash0.",
				apiSyncReconciler.ShortName(),
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
				apiSyncReconciler.ShortName(),
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

	dash0ApiResourceObject := dash0ApiResource.Object
	if dash0ApiResourceObject == nil {
		logger.Info(
			fmt.Sprintf(
				"The \"Object\" property in the event for %s in %s/%s is absent or empty, the %s(s) will not be updated in Dash0.",
				apiSyncReconciler.KindDisplayName(),
				namespace,
				name,
				apiSyncReconciler.ShortName(),
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	syncDisabledViaLabel := isSyncDisabledViaLabel(dash0ApiResourceObject)

	cleanUpMetadata(dash0ApiResourceObject)

	specRaw := dash0ApiResourceObject["spec"]
	if specRaw == nil {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s has no spec, the %s(s) from will not be updated in Dash0.",
				apiSyncReconciler.KindDisplayName(),
				namespace,
				name,
				apiSyncReconciler.ShortName(),
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
				apiSyncReconciler.KindDisplayName(),
				namespace,
				name,
				apiSyncReconciler.ShortName(),
			))
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	apiEndpoint := apiConfig.Endpoint
	if !strings.HasSuffix(apiEndpoint, "/") {
		apiEndpoint += "/"
	}

	return &preconditionValidationResult{
		synchronizeResource:  true,
		syncDisabledViaLabel: syncDisabledViaLabel,
		resource:             dash0ApiResourceObject,
		// TODO remove dash0ApiResourceSpec from struct once all controllers have been migrated to work with the full
		// resource, and no controller uses dash0ApiResourceSpec.
		// (See perses_dashboard_controller.go#MapResourceToHttpRequests.)
		dash0ApiResourceSpec: spec,
		monitoringResource:   monitoringResource,
		authToken:            authToken,
		apiEndpoint:          apiEndpoint,
		dataset:              dataset,
		k8sNamespace:         namespace,
		k8sName:              name,
	}
}

func isSyncDisabledViaLabel(dash0ApiResourceObject map[string]interface{}) bool {
	if metadataRaw := dash0ApiResourceObject["metadata"]; metadataRaw != nil {
		if metadata, ok := metadataRaw.(map[string]interface{}); ok {
			if labelsRaw := metadata["labels"]; labelsRaw != nil {
				if labels, ok := labelsRaw.(map[string]interface{}); ok {
					if dash0Enable := labels["dash0.com/enable"]; dash0Enable == "false" {
						return true
					}
				}
			}
		}
	}
	return false
}

// cleanUpMetadata removes fields from the resource that are somewhat large and not relevant for synchronizing a
// resource with the Dash0 API, to reduce the payload size of the request sent to the API (e.g. metadata.managedFields,
// metadata.annotations.kubectl.kubernetes.io/last-applied-configuration).
func cleanUpMetadata(resource map[string]interface{}) {
	metadataRaw := resource["metadata"]
	if metadataRaw != nil {
		metadata, ok := metadataRaw.(map[string]interface{})
		delete(metadata, "managedFields")
		if ok {
			annotationsRaw := metadata["annotations"]
			if annotationsRaw != nil {
				annotations, ok := annotationsRaw.(map[string]interface{})
				if ok {
					delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
				}
			}
		}
	}
}

func fetchExistingOrigins(
	apiSyncReconciler ApiSyncReconciler,
	preconditionChecksResult *preconditionValidationResult,
	logger *logr.Logger,
) ([]string, error) {
	thirdPartyResourceReconciler, ok := apiSyncReconciler.(ThirdPartyResourceReconciler)
	if !ok {
		// this resource reconciler synchronizes a resource type owned by the Dash0 operator, fetching existing origins
		// is neither supported nor necessary since there is a one-to-one relationship between K8s resource and Dash0
		// API object
		return nil, nil
	}
	if fetchExistingOriginsRequest, err :=
		thirdPartyResourceReconciler.FetchExistingResourceOriginsRequest(preconditionChecksResult); err != nil {
		logger.Error(err, "cannot create request to fetch existing resource origins")
		return nil, err
	} else if fetchExistingOriginsRequest != nil {
		actionLabel := fmt.Sprintf("fetch existing origins: %s %s",
			fetchExistingOriginsRequest.Method,
			fetchExistingOriginsRequest.URL.String())
		if responseBytes, err := executeSingleHttpRequestWithRetryAndReadBody(
			apiSyncReconciler,
			fetchExistingOriginsRequest,
			actionLabel,
			logger,
		); err != nil {
			logger.Error(err, "cannot fetch existing origins")
			return nil, err
		} else {
			objectsWithOrigin := make([]Dash0ApiObjectWithOrigin, 0)
			if err = json.Unmarshal(responseBytes, &objectsWithOrigin); err != nil {
				logger.Error(
					err,
					"cannot parse response after querying existing origins",
					"response",
					string(responseBytes),
				)
				return nil, err
			}
			existingOriginsWithMatchingPrefix := make([]string, 0, len(objectsWithOrigin))
			for _, objWithOrigin := range objectsWithOrigin {
				if objWithOrigin.Origin != "" {
					existingOriginsWithMatchingPrefix =
						append(existingOriginsWithMatchingPrefix, objWithOrigin.Origin)
				}
			}
			return existingOriginsWithMatchingPrefix, nil
		}
	} else {
		// this third party resource reconciler does not support fetching existing origins, this is expected for
		// resource types that have a one-to-one relationship between K8s resource and Dash0 API object
		return nil, nil
	}
}

func addDeleteRequestsForObjectsThatHaveBeenDeletedInTheKubernetesResource(
	apiSyncReconciler ApiSyncReconciler,
	preconditionChecksResult *preconditionValidationResult,
	existingOriginsFromApi []string,
	originsInResource []string,
	httpRequests []HttpRequestWithItemName,
	itemsTotal int,
	synchronizationErrors map[string]string,
	logger *logr.Logger,
) (int, []HttpRequestWithItemName) {
	thirdPartyResourceReconciler, ok := apiSyncReconciler.(ThirdPartyResourceReconciler)
	if !ok {
		// This resource reconciler synchronizes a resource type owned by the Dash0 operator, deleting existing objects
		// on upsert is not supported nor necessary since there is a one-to-one relationship between K8s resources and
		// Dash0 API objects.
		return itemsTotal, httpRequests
	}
	deleteHttpRequests, deleteSynchronizationErrors := thirdPartyResourceReconciler.CreateDeleteRequests(
		preconditionChecksResult,
		existingOriginsFromApi,
		originsInResource,
		logger,
	)
	itemsTotal += len(deleteHttpRequests)
	maps.Copy(synchronizationErrors, deleteSynchronizationErrors)
	httpRequests = slices.Concat(httpRequests, deleteHttpRequests)
	return itemsTotal, httpRequests
}

// structToMap converts any struct to an unstructured.Unstructured object.
func structToMap(obj interface{}) (*unstructured.Unstructured, error) {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var object map[string]interface{}
	if err = json.Unmarshal(jsonBytes, &object); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{
		Object: object,
	}, nil
}

func addAuthorizationHeader(req *http.Request, preconditionChecksResult *preconditionValidationResult) {
	req.Header.Set(util.AuthorizationHeaderName, util.RenderAuthorizationHeader(preconditionChecksResult.authToken))
}

// executeAllHttpRequests executes all HTTP requests in the given list and returns the names of the items that were
// successfully synchronized, as well as a map of name to error message for items that were rejected by the Dash0 API.
func executeAllHttpRequests(
	apiSyncReconciler ApiSyncReconciler,
	allRequests []HttpRequestWithItemName,
	logger *logr.Logger,
) ([]SuccessfulSynchronizationResult, map[string]string) {
	successfullySynchronized := make([]SuccessfulSynchronizationResult, 0)
	httpErrors := make(map[string]string)
	for _, req := range allRequests {
		actionLabel := fmt.Sprintf("synchronize the %s \"%s\": %s %s",
			apiSyncReconciler.ShortName(),
			req.ItemName,
			req.Request.Method,
			req.Request.URL.String())
		if responseBytes, err := executeSingleHttpRequestWithRetryAndReadBody(apiSyncReconciler, req.Request, actionLabel, logger); err != nil {
			httpErrors[req.ItemName] = err.Error()
		} else {
			syncResponse := SuccessfulSynchronizationResult{
				ItemName: req.ItemName,
				Labels:   apiSyncReconciler.ExtractIdOriginAndLinkFromResponseBody(responseBytes, logger),
			}
			successfullySynchronized = append(successfullySynchronized, syncResponse)
		}
	}
	if len(successfullySynchronized) == 0 {
		successfullySynchronized = nil
	}
	return successfullySynchronized, httpErrors
}

func executeSingleHttpRequestWithRetryAndReadBody(
	apiSyncReconciler ApiSyncReconciler,
	req *http.Request,
	actionLabel string,
	logger *logr.Logger,
) ([]byte, error) {
	logger.Info(fmt.Sprintf("executing HTTP request to %s", actionLabel))
	responseBody := &[]byte{}
	if err := util.RetryWithCustomBackoff(
		fmt.Sprintf("http request to %s", req.URL.String()),
		func() error {
			return executeSingleHttpRequestAndReadBody(
				apiSyncReconciler,
				req,
				responseBody,
				actionLabel,
				logger,
			)
		},
		wait.Backoff{
			Steps:    3,
			Duration: apiSyncReconciler.GetHttpRetryDelay(),
			Factor:   1.5,
		},
		true,
		true,
		logger,
	); err != nil {
		return nil, err
	} else if responseBody != nil && len(*responseBody) > 0 {
		return *responseBody, nil
	} else {
		return nil, fmt.Errorf("unexpected nil/empty response body")
	}
}

func executeSingleHttpRequestAndReadBody(
	apiSyncReconciler ApiSyncReconciler,
	req *http.Request,
	responseBody *[]byte,
	actionLabel string,
	logger *logr.Logger,
) error {
	res, err := apiSyncReconciler.HttpClient().Do(req)
	if err != nil {
		logger.Error(err,
			fmt.Sprintf("unable to execute the HTTP request to %s", actionLabel))
		return util.NewRetryableErrorWithFlag(err, true)
	}

	isUnexpectedStatusCode := res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices
	if req.Method == http.MethodDelete {
		isUnexpectedStatusCode = res.StatusCode != http.StatusNotFound && isUnexpectedStatusCode
	}
	if isUnexpectedStatusCode {
		// HTTP status is not 2xx, treat this as an error.
		// convertNon2xxStatusCodeToError will also consume and close the response body.
		err = convertNon2xxStatusCodeToError(res, actionLabel)
		retryableStatusCodeError := util.NewRetryableError(err)
		if res.StatusCode >= http.StatusBadRequest && res.StatusCode < http.StatusInternalServerError {
			// HTTP 4xx status codes are not retryable
			retryableStatusCodeError.SetRetryable(false)
			logger.Error(err, "unexpected status code")
			return retryableStatusCodeError
		} else {
			// everything else, in particular HTTP 5xx status codes can be retried
			retryableStatusCodeError.SetRetryable(true)
			logger.Error(err, "unexpected status code, request might be retried")
			return retryableStatusCodeError
		}
	}

	// HTTP status code was 2xx, read the response body and return it
	defer func() {
		_ = res.Body.Close()
	}()

	if responseBytes, err := io.ReadAll(res.Body); err != nil {
		logger.Error(err,
			fmt.Sprintf(
				"unable to execute the HTTP request for %s at %s",
				apiSyncReconciler.ShortName(),
				req.URL.String(),
			))
		return util.NewRetryableErrorWithFlag(err, true)
	} else {
		*responseBody = responseBytes
	}
	return nil
}

func convertNon2xxStatusCodeToError(
	res *http.Response,
	actionLabel string,
) error {
	defer func() {
		_ = res.Body.Close()
	}()
	errorResponseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		readBodyErr := fmt.Errorf("unable to read the API response payload after receiving status code %d when "+
			"trying to %s",
			res.StatusCode,
			actionLabel,
		)
		return readBodyErr
	}

	statusCodeErr := fmt.Errorf(
		"unexpected status code %d when trying to %s, response body is %s",
		res.StatusCode,
		actionLabel,
		string(errorResponseBody),
	)
	return statusCodeErr
}

// writeSynchronizationResult writes the result of a synchronization attempt to the status of a Kubernetes
// resources. For third-party resource types, where we have no business in writing to the resource's status, the
// synchronization result is written to a map in the status of the Dash0 monitoring resource in the same namespace.
// For resource types owned by the Dash0 operator, the synchronization result is written directly to the status of
// the resource that has been synchronized.
func writeSynchronizationResult(
	ctx context.Context,
	apiSyncReconciler ApiSyncReconciler,
	monitoringResource *dash0v1beta1.Dash0Monitoring,
	dash0ApiResource *unstructured.Unstructured,
	ownedResource client.Object,
	itemsTotal int,
	successfullySynchronized []SuccessfulSynchronizationResult,
	validationIssuesPerItem map[string][]string,
	synchronizationErrorsPerItem map[string]string,
	resourceHasBeenDeleted bool,
	logger *logr.Logger,

) {
	ownedResourceReconciler, ok := apiSyncReconciler.(OwnedResourceReconciler)
	if ok {
		if resourceHasBeenDeleted {
			// the resource has been deleted, so we cannot update its status
			return
		}
		convertAndWriteSynchronizationResultForOwnedResource(
			ctx,
			ownedResourceReconciler,
			ownedResource,
			successfullySynchronized,
			validationIssuesPerItem,
			synchronizationErrorsPerItem,
			logger,
		)
		return
	}
	thirdPartyResourceReconciler, ok := apiSyncReconciler.(ThirdPartyResourceReconciler)
	if ok {
		writeSynchronizationResultToDash0MonitoringStatus(
			ctx,
			thirdPartyResourceReconciler,
			monitoringResource,
			dash0ApiResource,
			itemsTotal,
			successfullySynchronized,
			validationIssuesPerItem,
			synchronizationErrorsPerItem,
			logger,
		)
		return
	}
	logger.Error(
		fmt.Errorf("cannot write synchronization results"),
		"api sync synchronizer neither implements OwnedResourceReconciler ThirdPartyResourceReconciler, cannot write synchronization results to status",
	)
}

// convertAndWriteSynchronizationResultForOwnedResource converts the generalized synchronization result (which could be
// the result of synchronizing a third-party resource type with a 1-to-many relationship between K8s resource and Dash0
// API objects) to the specific case of a resource type owned by the Dash0 operator, which always has a 1-to-1
// relationship between K8s resource and Dash0 API object, and writes the result to the status of the synchronized
// resource.
func convertAndWriteSynchronizationResultForOwnedResource(
	ctx context.Context,
	ownedResourceReconciler OwnedResourceReconciler,
	ownedResource client.Object,
	successfullySynchronized []SuccessfulSynchronizationResult,
	validationIssuesPerItem map[string][]string,
	synchronizationErrorsPerItem map[string]string,
	logger *logr.Logger,
) {
	status := dash0common.Dash0ApiResourceSynchronizationStatusFailed
	if len(successfullySynchronized) > 0 && len(validationIssuesPerItem) == 0 && len(synchronizationErrorsPerItem) == 0 {
		// successfullySynchronized can always only be 0 or 1
		status = dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
	}

	apiObjectLabels := Dash0ApiObjectLabels{}
	if len(successfullySynchronized) > 0 {
		apiObjectLabels = successfullySynchronized[0].Labels
	}

	synchronizationError := ""
	if len(synchronizationErrorsPerItem) > 0 {
		// synchronizationErrorsPerItem can only have at most one entry
		synchronizationError = slices.Collect(maps.Values(synchronizationErrorsPerItem))[0]
	} else {
		// clear out errors from previous synchronization attempts
		synchronizationError = ""
	}

	var validationIssues []string
	if len(validationIssuesPerItem) > 0 {
		// there can only be at most one list of validation issues for a Perses dashboard resource
		validationIssues = slices.Collect(maps.Values(validationIssuesPerItem))[0]
	} else {
		// clear out validation issues from previous synchronization attempts
		validationIssues = make([]string, 0)
	}

	ownedResourceReconciler.WriteSynchronizationResultToSynchronizedResource(
		ctx,
		ownedResource,
		status,
		apiObjectLabels,
		validationIssues,
		synchronizationError,
		logger,
	)
}
