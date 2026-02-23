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
	Token    string
}

// ValidatedApiConfigAndToken ensures a complete API sync config where all fields have been validated, correctly
// formatted and (for optional values) defaults have been applied.
type ValidatedApiConfigAndToken struct {
	ApiConfig
}

func NewValidatedApiConfigAndToken(endpoint string, dataset string, token string) *ValidatedApiConfigAndToken {
	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}
	if dataset == "" {
		dataset = util.DatasetDefault
	}
	return &ValidatedApiConfigAndToken{
		ApiConfig: ApiConfig{
			Endpoint: endpoint,
			Dataset:  dataset,
			Token:    token,
		},
	}
}

type ApiClient interface {
	SetDefaultApiConfigs(context.Context, []ApiConfig, logr.Logger)
	RemoveDefaultApiConfigs(context.Context, logr.Logger)
}

type NamespacedApiClient interface {
	SetNamespacedApiConfigs(context.Context, string, []ApiConfig, logr.Logger)
	RemoveNamespacedApiConfigs(context.Context, string, logr.Logger)
}

type ResourceToRequestsResult struct {
	ApiConfig ApiConfig
	// the total number of eligible items in the Kubernetes resource
	ItemsTotal int
	// the request objects for which the conversion was successful
	ApiRequests []WrappedApiRequest
	// only set for resource types with a one-to-many relationship between Kubernetes resources and Dash0 entities,
	// contains a list of all Dash0 origins produced by this Kubernetes resource for this ApiConfig
	OriginsInResource []string
	// synchronization errors that occurred during the conversion - if there is a synchronization error for a resource,
	// ApiRequests must not contain a request for the associated resource
	SynchronizationErrors map[string]string
	// validation issues that occurred while preparing the request
	ValidationIssues map[string][]string
}

func NewResourceToRequestsResult(
	apiConfig ApiConfig,
	itemsTotal int,
	apiRequests []WrappedApiRequest,
	origins []string,
	validationIssues map[string][]string,
	synchronizationErrors map[string]string,
) *ResourceToRequestsResult {
	return &ResourceToRequestsResult{
		ApiConfig:             apiConfig,
		ItemsTotal:            itemsTotal,
		ApiRequests:           apiRequests,
		OriginsInResource:     origins,
		ValidationIssues:      validationIssues,
		SynchronizationErrors: synchronizationErrors,
	}
}

func NewResourceToRequestsResultSingleItemSuccess(
	apiConfig ApiConfig,
	request *http.Request,
	itemName string,
	origin string,
) *ResourceToRequestsResult {
	return NewResourceToRequestsResult(
		apiConfig,
		1,
		[]WrappedApiRequest{
			{
				Request:  request,
				ItemName: itemName,
				Origin:   origin,
			},
		},
		nil,
		nil,
		nil,
	)
}

func NewResourceToRequestsResultSingleItemValidationIssue(
	apiConfig ApiConfig,
	itemName string,
	issue string,
) *ResourceToRequestsResult {
	return NewResourceToRequestsResult(
		apiConfig,
		1,
		nil,
		nil,
		map[string][]string{
			itemName: {issue},
		},
		nil,
	)
}

func NewResourceToRequestsResultSingleItemError(
	apiConfig ApiConfig,
	itemName string,
	errorMessage string,
) *ResourceToRequestsResult {
	return NewResourceToRequestsResult(apiConfig, 1, nil, nil, nil, map[string]string{itemName: errorMessage})
}

func NewResourceToRequestsResultPreconditionError(apiConfig ApiConfig, errorMessage string) *ResourceToRequestsResult {
	return NewResourceToRequestsResult(
		apiConfig,
		// There might actually be eligible items in the Kubernetes resource, but at this point we do not
		// know yet, so return 0.
		0,
		nil,
		nil,
		nil,
		map[string]string{"*": errorMessage},
	)
}

func (m *ResourceToRequestsResult) IsNoOp() bool {
	return len(m.ApiRequests) == 0 &&
		len(m.ValidationIssues) == 0 &&
		len(m.SynchronizationErrors) == 0
}

func (m *ResourceToRequestsResult) HasNoErrorsAndNoIssues() bool {
	return len(m.ValidationIssues) == 0 && len(m.SynchronizationErrors) == 0
}

// ApiSyncReconciler is the common interface for reconcilers that synchonize their Kubernetes resources to the Dash0
// API. This can either be resource types owned by the Dash0 operator (like Dash0SyntheticCheck), which then also
// implement the OwnedResourceReconciler interface, or third-party resource types (like PrometheusRule or
// PersesDashboard), which implement the ThirdPartyResourceReconciler interface.
type ApiSyncReconciler interface {
	KindDisplayName() string
	ShortName() string
	GetDefaultApiConfigs() []ApiConfig
	GetNamespacedApiConfigs(string) ([]ApiConfig, bool)
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
	MapResourceToHttpRequests(*preconditionValidationResult, ApiConfig, apiAction, logr.Logger) *ResourceToRequestsResult

	ExtractIdFromResponseBody(
		responseBytes []byte,
		logger logr.Logger,
	) (string, error)
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
		synchronizationResults,
		logr.Logger,
	)
}

// WrappedApiRequest bundles an http.Request for the Dash0 API with additional metadata.
type WrappedApiRequest struct {
	Request   *http.Request
	ItemName  string
	Origin    string
	ApiConfig ApiConfig
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
	Id          string `json:"dash0.com/id,omitempty"`
	Origin      string `json:"dash0.com/origin,omitempty"`
	ApiEndpoint string `json:"dash0.com/apiEndpoint"`
	Dataset     string `json:"dash0.com/dataset"`
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

	// resource is the Kubernetes resource that is being reconciled, as a map
	resource map[string]any

	// monitoringResource is the Dash0 monitoring resource that was found in the same namespace as the resource
	monitoringResource *dash0v1beta1.Dash0Monitoring
	// validatedApiConfigs are the validated and standardized API configs containing the endpoint, dataset and token
	validatedApiConfigs []ValidatedApiConfigAndToken

	// k8sNamespace is Kubernetes namespace in which the Dash0 API resource has been found
	k8sNamespace string

	// k8sNamespace is Kubernetes name of the Dash0 API resource
	k8sName string
}

type synchronizationResults struct {
	itemsTotal          int
	validationIssues    map[string][]string
	resultsPerApiConfig []synchronizationResultPerApiConfig
}

func (s *synchronizationResults) allSynchronizationErrors() []map[string]string {
	if s == nil {
		return nil
	}
	var allSyncErrors []map[string]string
	for _, resultsPerApiConfig := range s.resultsPerApiConfig {
		allSyncErrors = append(allSyncErrors, resultsPerApiConfig.resourceToRequestsResult.SynchronizationErrors)
	}
	return allSyncErrors
}

func (s *synchronizationResults) resourceSyncStatus() dash0common.Dash0ApiResourceSynchronizationStatus {
	var hasError, hasSuccess bool

	for _, resultPerApiConfig := range s.resultsPerApiConfig {
		if len(resultPerApiConfig.successfullySynchronized) > 0 {
			hasSuccess = true
		}
		if len(resultPerApiConfig.resourceToRequestsResult.SynchronizationErrors) > 0 {
			hasError = true
		}
	}

	if hasSuccess && (hasError || len(s.validationIssues) > 0) {
		return dash0common.Dash0ApiResourceSynchronizationStatusPartiallySuccessful
	} else if hasSuccess {
		return dash0common.Dash0ApiResourceSynchronizationStatusSuccessful
	} else {
		return dash0common.Dash0ApiResourceSynchronizationStatusFailed
	}
}

type synchronizationResultPerApiConfig struct {
	apiConfig                ApiConfig
	successfullySynchronized []SuccessfulSynchronizationResult
	resourceToRequestsResult *ResourceToRequestsResult
}

func synchronizeViaApiAndUpdateStatus(
	ctx context.Context,
	apiSyncReconciler ApiSyncReconciler,
	dash0ApiResource *unstructured.Unstructured,
	ownedResource client.Object,
	action apiAction,
	logger logr.Logger,
) {
	preconditionChecksResult := validatePreconditionsAndPreprocess(
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

	var synchronizationResultsPerApiConfig []synchronizationResultPerApiConfig

	for _, validatedApiConfig := range preconditionChecksResult.validatedApiConfigs {
		var existingOriginsFromApi []string
		if action != deleteAction {
			var err error
			existingOriginsFromApi, err = fetchExistingOrigins(
				apiSyncReconciler,
				preconditionChecksResult,
				validatedApiConfig.ApiConfig,
				logger,
			)
			if err != nil {
				// The error has already been logged in fetchExistingOrigins. Record the failure for this config
				// and continue with the remaining configs.
				requestResult := NewResourceToRequestsResultPreconditionError(validatedApiConfig.ApiConfig, err.Error())
				synchronizationResultsPerApiConfig = append(
					synchronizationResultsPerApiConfig, synchronizationResultPerApiConfig{
						apiConfig:                validatedApiConfig.ApiConfig,
						successfullySynchronized: nil,
						resourceToRequestsResult: requestResult,
					},
				)
				continue
			}
		}

		resourceToRequestsResult := apiSyncReconciler.MapResourceToHttpRequests(
			preconditionChecksResult,
			validatedApiConfig.ApiConfig,
			action,
			logger,
		)

		if action != deleteAction {
			addDeleteRequestsForObjectsThatHaveBeenDeletedInTheKubernetesResource(
				apiSyncReconciler,
				validatedApiConfig.ApiConfig,
				existingOriginsFromApi,
				resourceToRequestsResult,
				logger,
			)
		}
		if resourceToRequestsResult.IsNoOp() {
			logger.Info(
				fmt.Sprintf(
					"%s %s/%s did not contain any %s, skipping.",
					apiSyncReconciler.KindDisplayName(),
					dash0ApiResource.GetNamespace(),
					dash0ApiResource.GetName(),
					apiSyncReconciler.ShortName(),
				),
			)
		}

		var successfullySynchronized []SuccessfulSynchronizationResult
		var httpErrors map[string]string
		if len(resourceToRequestsResult.ApiRequests) > 0 {
			successfullySynchronized, httpErrors =
				executeAllHttpRequests(apiSyncReconciler, resourceToRequestsResult.ApiRequests, logger)
		}
		if len(httpErrors) > 0 {
			if resourceToRequestsResult.SynchronizationErrors == nil {
				resourceToRequestsResult.SynchronizationErrors = make(map[string]string)
			}
			// In theory, the following map merge could overwrite synchronization errors from the MapResourceToHttpRequests
			// stage with errors occurring in executeAllHttpRequests, but items that have an error in
			// MapResourceToHttpRequests are never converted to requests, so the two maps are disjoint.
			maps.Copy(resourceToRequestsResult.SynchronizationErrors, httpErrors)
		}

		if resourceToRequestsResult.HasNoErrorsAndNoIssues() {
			logger.Info(
				fmt.Sprintf(
					"%s %s/%s: %d %s(s), %d successfully synchronized to %s (%s)",
					apiSyncReconciler.KindDisplayName(),
					dash0ApiResource.GetNamespace(),
					dash0ApiResource.GetName(),
					resourceToRequestsResult.ItemsTotal,
					apiSyncReconciler.ShortName(),
					len(successfullySynchronized),
					validatedApiConfig.Endpoint,
					validatedApiConfig.Dataset,
				),
			)
		} else {
			logger.Error(
				fmt.Errorf("validation issues and/or synchronization issues occurred"),
				fmt.Sprintf(
					"%s %s/%s: %d %s(s), %d successfully synchronized to %s (%s), validation issues: %v, synchronization errors: %v",
					apiSyncReconciler.KindDisplayName(),
					dash0ApiResource.GetNamespace(),
					dash0ApiResource.GetName(),
					resourceToRequestsResult.ItemsTotal,
					apiSyncReconciler.ShortName(),
					len(successfullySynchronized),
					validatedApiConfig.Endpoint,
					validatedApiConfig.Dataset,
					resourceToRequestsResult.ValidationIssues,
					resourceToRequestsResult.SynchronizationErrors,
				),
			)
		}

		result := synchronizationResultPerApiConfig{
			apiConfig:                validatedApiConfig.ApiConfig,
			successfullySynchronized: successfullySynchronized,
			resourceToRequestsResult: resourceToRequestsResult,
		}

		synchronizationResultsPerApiConfig = append(synchronizationResultsPerApiConfig, result)
	}

	var syncResults synchronizationResults
	if len(synchronizationResultsPerApiConfig) > 0 {
		// note: properties like itemsTotal and validationIssues do not depend on a specific apiConfig and should be the
		// same in all entries, so we use the values from the first
		syncResults = synchronizationResults{
			itemsTotal:          synchronizationResultsPerApiConfig[0].resourceToRequestsResult.ItemsTotal,
			validationIssues:    synchronizationResultsPerApiConfig[0].resourceToRequestsResult.ValidationIssues,
			resultsPerApiConfig: synchronizationResultsPerApiConfig,
		}
	}

	writeSynchronizationResult(
		ctx,
		apiSyncReconciler,
		preconditionChecksResult.monitoringResource,
		dash0ApiResource,
		ownedResource,
		syncResults,
		resourceHasBeenDeleted,
		logger,
	)
}

// validatePreconditionsAndPreprocess checks whether a resource can be synchronized to the Dash0 API and applies common
// preprocessing steps to the resource payload.
//
// Validation checks:
// - Is there a monitoring resource in the namespace?
// - Is synchronization enabled for the resource type in the namespace?
// - Is synchronization disabled for this resource specifically via the dash0.com/enable label?
// - Are a Dash0 API endpoint and a Dash0 auth token available (either in the operator configuration or the monitoring resource)?
//
// Preprocessing steps:
// - Remove irrelevant metadata like metadata.managedFields and the kubectl.kubernetes.io/last-applied-configuration
// annotation.
// - Remove well-known dash0.com labels that were part of the YAML download in the Dash0 UI for a while, but should not
// be sent to the API:
//   - dash0.com/dataset
//   - dash0.com/id
//   - dash0.com/source
//   - dash0.com/version
func validatePreconditionsAndPreprocess(
	ctx context.Context,
	apiSyncReconciler ApiSyncReconciler,
	dash0ApiResource *unstructured.Unstructured,
	logger logr.Logger,
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
		logger.Error(
			err,
			fmt.Sprintf(
				"An error occurred when looking up the Dash0 monitoring resource in namespace %s while trying to synchronize the %s resource %s.",
				namespace,
				apiSyncReconciler.KindDisplayName(),
				name,
			),
		)
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
			),
		)
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
				),
			)
			return &preconditionValidationResult{
				synchronizeResource: false,
			}
		}
	}

	var defaultValidatedApiConfigs []ValidatedApiConfigAndToken
	var namespacedValidatedApiConfigs []ValidatedApiConfigAndToken

	namespacedApiConfigs, namespacedApiConfigExists := apiSyncReconciler.GetNamespacedApiConfigs(namespace)

	// This check is only relevant when the operator starts and we might have already reconciled the operator config
	// but not the monitoring resource in this namespace. In that case we would incorrectly do an initial sync to the
	// default backend even though the monitoring resource defines a custom API config.
	if monitoringResource.HasDash0ExportConfigured() && !namespacedApiConfigExists {
		logger.Info(
			fmt.Sprintf(
				"The monitoring resource of namespace %s has a Dash0 export, but no API config "+
					"is available (yet). This might happen if the monitoring resource has not been reconciled so far and will "+
					"be resolved once the resource is reconciled. Sync for the %s from %s will be disabled until then.",
				namespace, apiSyncReconciler.ShortName(), name,
			),
		)
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	// We first check whether valid apiConfigs exist for the namespace
	if namespacedApiConfigExists {
		validNamespacedConfigs := filterValidApiConfigs(
			namespacedApiConfigs,
			logger,
			fmt.Sprintf("monitoring resource in namespace %s", namespace),
		)
		if len(validNamespacedConfigs) > 0 {
			logger.Info(
				fmt.Sprintf(
					"Found %d valid Dash0 API config(s) in the monitoring resource "+
						"in namespace %s. Sync for the %s from %s will use the endpoint, dataset and token defined in the monitoring resource.",
					len(validNamespacedConfigs), namespace, apiSyncReconciler.ShortName(), name,
				),
			)
			for _, apiConfig := range validNamespacedConfigs {
				validatedApiConfig := NewValidatedApiConfigAndToken(
					apiConfig.Endpoint,
					apiConfig.Dataset,
					apiConfig.Token,
				)
				namespacedValidatedApiConfigs = append(namespacedValidatedApiConfigs, *validatedApiConfig)
			}
		}
	}

	// We also expect default API config(s), even when the namespaced ones are valid.
	// This is done mostly for consistency with the validation logic for the collectors.
	defaultApiConfigs := apiSyncReconciler.GetDefaultApiConfigs()
	validDefaultConfigs := filterValidApiConfigs(defaultApiConfigs, logger, "default operator configuration")
	if len(validDefaultConfigs) > 0 {
		for _, apiConfig := range validDefaultConfigs {
			validatedApiConfig := NewValidatedApiConfigAndToken(
				apiConfig.Endpoint,
				apiConfig.Dataset,
				apiConfig.Token,
			)
			defaultValidatedApiConfigs = append(defaultValidatedApiConfigs, *validatedApiConfig)
		}
	}
	if len(namespacedValidatedApiConfigs) == 0 && len(defaultValidatedApiConfigs) == 0 {
		logger.Info(
			fmt.Sprintf(
				"No valid Dash0 API config(s) available (neither in the monitoring resource "+
					"nor in the operator configuration resource). The %s(s) from %s/%s will not be updated in Dash0.",
				apiSyncReconciler.ShortName(),
				namespace,
				name,
			),
		)
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
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
			),
		)
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	syncDisabledViaLabel := isSyncDisabledViaLabel(dash0ApiResourceObject)

	cleanUpMetadata(dash0ApiResourceObject)

	if dash0ApiResourceObject["spec"] == nil {
		logger.Info(
			fmt.Sprintf(
				"%s %s/%s has no spec, the %s(s) from will not be updated in Dash0.",
				apiSyncReconciler.KindDisplayName(),
				namespace,
				name,
				apiSyncReconciler.ShortName(),
			),
		)
		return &preconditionValidationResult{
			synchronizeResource: false,
		}
	}

	var validatedApiConfigs []ValidatedApiConfigAndToken
	if len(namespacedValidatedApiConfigs) > 0 {
		validatedApiConfigs = namespacedValidatedApiConfigs
	} else {
		validatedApiConfigs = defaultValidatedApiConfigs
	}

	return &preconditionValidationResult{
		synchronizeResource:  true,
		syncDisabledViaLabel: syncDisabledViaLabel,
		resource:             dash0ApiResourceObject,
		monitoringResource:   monitoringResource,
		validatedApiConfigs:  validatedApiConfigs,
		k8sNamespace:         namespace,
		k8sName:              name,
	}
}

func isSyncDisabledViaLabel(dash0ApiResourceObject map[string]any) bool {
	if metadataRaw := dash0ApiResourceObject["metadata"]; metadataRaw != nil {
		if metadata, ok := metadataRaw.(map[string]any); ok {
			if labelsRaw := metadata["labels"]; labelsRaw != nil {
				if labels, ok := labelsRaw.(map[string]any); ok {
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
func cleanUpMetadata(resource map[string]any) {
	metadataRaw := resource["metadata"]
	if metadataRaw != nil {
		metadata, ok := metadataRaw.(map[string]any)
		if ok {
			delete(metadata, "managedFields")
			annotationsRaw := metadata["annotations"]
			if annotationsRaw != nil {
				annotations, ok := annotationsRaw.(map[string]any)
				if ok {
					delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
				}
			}
			labelsRaw := metadata["labels"]
			if labelsRaw != nil {
				labels, ok := labelsRaw.(map[string]any)
				if ok {
					delete(labels, "dash0.com/dataset")
					delete(labels, "dash0.com/id")
					delete(labels, "dash0.com/source")
					delete(labels, "dash0.com/version")
				}
			}
		}
	}
}

func fetchExistingOrigins(
	apiSyncReconciler ApiSyncReconciler,
	preconditionChecksResult *preconditionValidationResult,
	apiConfig ApiConfig,
	logger logr.Logger,
) ([]string, error) {
	thirdPartyResourceReconciler, ok := apiSyncReconciler.(ThirdPartyResourceReconciler)
	if !ok {
		// this resource reconciler synchronizes a resource type owned by the Dash0 operator, fetching existing origins
		// is neither supported nor necessary since there is a one-to-one relationship between K8s resource and Dash0
		// API object
		return nil, nil
	}
	if fetchExistingOriginsRequest, err :=
		thirdPartyResourceReconciler.FetchExistingResourceOriginsRequest(preconditionChecksResult, apiConfig); err != nil {
		logger.Error(err, "cannot create request to fetch existing resource origins")
		return nil, err
	} else if fetchExistingOriginsRequest != nil {
		actionLabel := fmt.Sprintf(
			"fetch existing origins: %s %s",
			fetchExistingOriginsRequest.Method,
			fetchExistingOriginsRequest.URL.String(),
		)
		if responseBytes, err := executeSingleHttpRequestWithRetryAndReadBody(
			apiSyncReconciler,
			fetchExistingOriginsRequest,
			actionLabel,
			true,
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
	apiConfig ApiConfig,
	existingOriginsFromApi []string,
	resourceToRequestsResult *ResourceToRequestsResult,
	logger logr.Logger,
) {
	thirdPartyResourceReconciler, ok := apiSyncReconciler.(ThirdPartyResourceReconciler)
	if !ok {
		// This resource reconciler synchronizes a resource type owned by the Dash0 operator, deleting existing objects
		// on upsert is not supported nor necessary since there is a one-to-one relationship between K8s resources and
		// Dash0 API objects.
		return
	}
	deleteHttpRequests, deleteSynchronizationErrors := thirdPartyResourceReconciler.CreateDeleteRequests(
		apiConfig,
		existingOriginsFromApi,
		resourceToRequestsResult.OriginsInResource,
		logger,
	)
	resourceToRequestsResult.ItemsTotal += len(deleteHttpRequests)
	maps.Copy(resourceToRequestsResult.SynchronizationErrors, deleteSynchronizationErrors)
	resourceToRequestsResult.ApiRequests = slices.Concat(resourceToRequestsResult.ApiRequests, deleteHttpRequests)
}

// structToMap converts any struct to an unstructured.Unstructured object.
func structToMap(obj any) (*unstructured.Unstructured, error) {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err = json.Unmarshal(jsonBytes, &object); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{
		Object: object,
	}, nil
}

func addAuthorizationHeader(req *http.Request, token string) {
	req.Header.Set(util.AuthorizationHeaderName, util.RenderAuthorizationHeader(token))
}

// executeAllHttpRequests executes all HTTP requests in the given list and returns the names of the items that were
// successfully synchronized, as well as a map of name to error message for items that were rejected by the Dash0 API.
func executeAllHttpRequests(
	apiSyncReconciler ApiSyncReconciler,
	allRequests []WrappedApiRequest,
	logger logr.Logger,
) ([]SuccessfulSynchronizationResult, map[string]string) {
	successfullySynchronized := make([]SuccessfulSynchronizationResult, 0)
	httpErrors := make(map[string]string)
	for _, apiRequest := range allRequests {
		actionLabel := fmt.Sprintf(
			"synchronize the %s \"%s\": %s %s",
			apiSyncReconciler.ShortName(),
			apiRequest.ItemName,
			apiRequest.Request.Method,
			apiRequest.Request.URL.String(),
		)
		isDelete := apiRequest.Request.Method == http.MethodDelete
		if responseBytes, err :=
			executeSingleHttpRequestWithRetryAndReadBody(
				apiSyncReconciler,
				apiRequest.Request,
				actionLabel,
				!isDelete,
				logger,
			); err != nil {
			httpErrors[apiRequest.ItemName] = err.Error()
		} else {
			// The Dash0ApiObjectLabels will be used to provide additional information in the resources status when
			// we write the synchronization results, like the object's Dash0 id, origin and dataset.
			labels := Dash0ApiObjectLabels{
				Origin:      apiRequest.Origin,
				ApiEndpoint: apiRequest.ApiConfig.Endpoint,
				Dataset:     apiRequest.ApiConfig.Dataset,
			}

			if isDelete {
				// We do not receive a response body from HTTP DELETE requests. The id is not available, so it will be omitted.
				labels.Id = ""
			} else {
				id, _ := apiSyncReconciler.ExtractIdFromResponseBody(responseBytes, logger)
				labels.Id = id
			}
			syncResponse := SuccessfulSynchronizationResult{
				ItemName: apiRequest.ItemName,
				Labels:   labels,
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
	expectBody bool,
	logger logr.Logger,
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
	} else if expectBody {
		return nil, fmt.Errorf("unexpected nil/empty response body")
	} else {
		return make([]byte, 0), nil
	}
}

func executeSingleHttpRequestAndReadBody(
	apiSyncReconciler ApiSyncReconciler,
	req *http.Request,
	responseBody *[]byte,
	actionLabel string,
	logger logr.Logger,
) error {
	res, err := apiSyncReconciler.HttpClient().Do(req)
	if err != nil {
		logger.Error(
			err,
			fmt.Sprintf("unable to execute the HTTP request to %s", actionLabel),
		)
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
		logger.Error(
			err,
			fmt.Sprintf(
				"unable to execute the HTTP request for %s at %s",
				apiSyncReconciler.ShortName(),
				req.URL.String(),
			),
		)
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
		readBodyErr := fmt.Errorf(
			"unable to read the API response payload after receiving status code %d when "+
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
	syncResults synchronizationResults,
	resourceHasBeenDeleted bool,
	logger logr.Logger,

) {
	ownedResourceReconciler, ok := apiSyncReconciler.(OwnedResourceReconciler)
	if ok {
		if resourceHasBeenDeleted {
			// the resource has been deleted, so we cannot update its status
			return
		}
		ownedResourceReconciler.WriteSynchronizationResultToSynchronizedResource(
			ctx,
			ownedResource,
			syncResults,
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
			syncResults,
			logger,
		)
		return
	}
	logger.Error(
		fmt.Errorf("cannot write synchronization results"),
		"api sync synchronizer neither implements OwnedResourceReconciler ThirdPartyResourceReconciler, cannot write synchronization results to status",
	)
}
