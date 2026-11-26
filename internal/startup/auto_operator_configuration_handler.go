// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/retry"
)

type SecretRef struct {
	Name string
	Key  string
}

type OperatorConfigurationValues struct {
	Endpoint string
	Token    string
	SecretRef
	ApiEndpoint                                      string
	Dataset                                          string
	KeepaliveTime                                    string
	KeepaliveTimeout                                 string
	KeepalivePermitWithoutStream                     bool
	SelfMonitoringEnabled                            bool
	KubernetesInfrastructureMetricsCollectionEnabled bool
	KubernetesObjectCollectionEnabled                bool
	CollectClusterScopedObjects                      bool
	InstrumentationDelivery                          dash0v1alpha1.InstrumentationDelivery
	CollectPodLabelsAndAnnotationsEnabled            bool
	CollectNamespaceLabelsAndAnnotationsEnabled      bool
	CollectNodeLabelsAndAnnotationsEnabled           bool
	PrometheusCrdSupportEnabled                      bool
	ProfilingEnabled                                 bool
	EbpfProfilerEnabled                              bool
	EbpfProfilerPIDNamespaceTranslationMode          string
	TelemetryCollectionEnabled                       bool
	ClusterName                                      string
	AutoMonitorNamespacesEnabled                     bool
	AutoMonitorNamespacesLabelSelector               string
}

type AutoOperatorConfigurationResourceHandler struct {
	client.Client
	readyCheckExecuter          *ReadyCheckExecuter
	hasBecomeLeaderChan         chan struct{}
	operatorConfigurationValues OperatorConfigurationValues
	extraConfigValues           atomic.Pointer[operatorConfigurationExtraConfigValues]

	createOrUpdateBackoff wait.Backoff
	retryDelays           retryDelays

	pendingAttemptMutex  sync.Mutex
	cancelPendingAttempt context.CancelCauseFunc

	// firstCreateOrUpdateHasStarted, firstCreateOrUpdateDoneChan and firstCreateOrUpdateDoneOnce track the first
	// create/update of the operator configuration resource, see WaitForFirstCreateOrUpdate.
	firstCreateOrUpdateHasStarted atomic.Bool
	firstCreateOrUpdateDoneChan   chan struct{}
	firstCreateOrUpdateDoneOnce   sync.Once
}

// errAttemptSuperseded is the cancellation cause of an attempt to create or update the operator configuration resource
// that has been replaced by a newer attempt.
var errAttemptSuperseded = errors.New("superseded by a newer attempt")

// retryDelays describes how long the handler waits before it starts another round of attempts to create or update the
// operator configuration resource, after a round has failed. The delay starts at initial and is multiplied by factor
// after each failed round, until it reaches max. There is no limit for the number of rounds.
type retryDelays struct {
	initial time.Duration
	factor  float64
	max     time.Duration
}

func (d retryDelays) next(current time.Duration) time.Duration {
	return min(time.Duration(float64(current)*d.factor), d.max)
}

var (
	defaultCreateOrUpdateBackoff = wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   1.5,
		Steps:    6,
	}
	defaultRetryDelays = retryDelays{
		initial: 30 * time.Second,
		factor:  2,
		max:     5 * time.Minute,
	}
)

// operatorConfigurationExtraConfigValues are the parts of the extra config map that end up in the automatically created
// operator configuration resource. The monitoring template, filter and transform are kept as raw JSON, since using the
// actual types here would lead to circular package dependencies.
type operatorConfigurationExtraConfigValues struct {
	monitoringTemplateRaw *json.RawMessage
	filterRaw             *json.RawMessage
	transformRaw          *json.RawMessage
	exports               []dash0common.Export
}

const (
	argoCdAyncOptionsAnnotationKey    = "argocd.argoproj.io/sync-options"
	argoCdCompareOptionsAnnotationKey = "argocd.argoproj.io/compare-options"
	managedByHelmAnnotationKey        = "dash0.com/managed-by-helm"
)

func NewAutoOperatorConfigurationResourceHandler(
	client client.Client,
	readyCheckExecuter *ReadyCheckExecuter,
	operatorConfigurationValues OperatorConfigurationValues,
	extraConfig util.ExtraConfig,
) *AutoOperatorConfigurationResourceHandler {
	r := &AutoOperatorConfigurationResourceHandler{
		Client:                      client,
		readyCheckExecuter:          readyCheckExecuter,
		hasBecomeLeaderChan:         make(chan struct{}),
		operatorConfigurationValues: operatorConfigurationValues,
		createOrUpdateBackoff:       defaultCreateOrUpdateBackoff,
		retryDelays:                 defaultRetryDelays,
		firstCreateOrUpdateDoneChan: make(chan struct{}),
	}
	r.extraConfigValues.Store(newOperatorConfigurationExtraConfigValues(extraConfig))
	return r
}

func newOperatorConfigurationExtraConfigValues(
	extraConfig util.ExtraConfig,
) *operatorConfigurationExtraConfigValues {
	return &operatorConfigurationExtraConfigValues{
		monitoringTemplateRaw: extraConfig.MonitoringTemplateRaw,
		filterRaw:             extraConfig.FilterRaw,
		transformRaw:          extraConfig.TransformRaw,
		exports:               extraConfig.Exports,
	}
}

func (r *AutoOperatorConfigurationResourceHandler) NotifyOperatorManagerJustBecameLeader(
	_ context.Context,
	_ logd.Logger,
) {
	close(r.hasBecomeLeaderChan)
}

func (r *AutoOperatorConfigurationResourceHandler) UpdateExtraConfig(
	ctx context.Context,
	extraConfig util.ExtraConfig,
	logger logd.Logger,
) {
	logger.Debug("extra config map update, checking for changes of the monitoring template, exports, filters and " +
		"transformations")
	newValues := newOperatorConfigurationExtraConfigValues(extraConfig)
	previousValues := r.extraConfigValues.Swap(newValues)
	if !hasExtraConfigValuesChanged(previousValues, newValues) {
		logger.Debug("ignoring extra config map update, the monitoring template, the exports, the filters and the " +
			"transformations have the same content as before")
		return
	}

	logger.Info("updating the operator configuration resource after the monitoring template, the exports, the " +
		"filters or the transformations have been updated")
	if _, err := r.CreateOrUpdateOperatorConfigurationResource(
		ctx,
		logger,
	); err != nil {
		logger.Error(
			err,
			"Failed to update the Dash0 operator configuration resource after updating the monitoring template, the "+
				"exports, the filters or the transformations via Helm.",
		)
	}
}

func hasExtraConfigValuesChanged(
	previousValues *operatorConfigurationExtraConfigValues,
	newValues *operatorConfigurationExtraConfigValues,
) bool {
	if previousValues == nil {
		return newValues != nil
	}
	if newValues == nil {
		return true
	}
	return hasRawValueChanged(previousValues.monitoringTemplateRaw, newValues.monitoringTemplateRaw) ||
		hasRawValueChanged(previousValues.filterRaw, newValues.filterRaw) ||
		hasRawValueChanged(previousValues.transformRaw, newValues.transformRaw) ||
		!reflect.DeepEqual(previousValues.exports, newValues.exports)
}

func hasRawValueChanged(previousValue *json.RawMessage, newValue *json.RawMessage) bool {
	if previousValue == nil && newValue == nil {
		return false
	}
	if previousValue == nil || newValue == nil {
		return true
	}
	return !reflect.DeepEqual(*previousValue, *newValue)
}

// CreateOrUpdateOperatorConfigurationResource waits until this replica becomes the leader, then it creates or updates
// the Dash0 operator configuration resource. The function will create/update the resource asynchronously, that is, when
// the function returns the resource might not have been created/updated yet. The function will optimistically return
// the resource that is going to be created/updated, without guarantees that the resource will be created/updated
// successfully. If creating/updating the resource fails with a retryable error (for example because the webhook
// service is temporarily not available), the handler keeps retrying in the background with increasing delays, until it
// succeeds, the context is cancelled, or a later call of this function supersedes the attempt.
func (r *AutoOperatorConfigurationResourceHandler) CreateOrUpdateOperatorConfigurationResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	isFirstCreateOrUpdate := r.firstCreateOrUpdateHasStarted.CompareAndSwap(false, true)

	operatorConfigurationResource, err := r.validateAndConvertToResource(logger)
	if err != nil {
		if isFirstCreateOrUpdate {
			r.markFirstCreateOrUpdateAsDone()
		}
		return nil, err
	}

	go r.createOrUpdateOperatorConfigurationResourceUntilSuccessful(
		r.supersedePendingAttempt(ctx),
		operatorConfigurationResource,
		logger,
	)

	// optimistically return the resource that we are going to create once the validation webhook becomes available
	return operatorConfigurationResource, nil
}

func (r *AutoOperatorConfigurationResourceHandler) validateAndConvertToResource(
	logger logd.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	logger.Info("running validations and checks for creating/updating the Dash0 operator configuration resource")
	exports := r.loadExports()
	if err := r.validateOperatorConfiguration(exports); err != nil {
		return nil, err
	}
	extraConfigValues := r.extraConfigValues.Load()
	monitoringTemplate, err := parseMonitoringTemplate(extraConfigValues)
	if err != nil {
		return nil, err
	}
	filter, err := parseFilter(extraConfigValues)
	if err != nil {
		return nil, err
	}
	transform, err := parseTransform(extraConfigValues)
	if err != nil {
		return nil, err
	}
	return convertValuesToResource(r.operatorConfigurationValues, monitoringTemplate, exports, filter, transform), nil
}

// WaitForFirstCreateOrUpdate blocks until the first call of CreateOrUpdateOperatorConfigurationResource has finished,
// that is, until it has created or updated the operator configuration resource, or until it has given up because of an
// error that is not retried. If that call is superseded by a later call, it waits for the later call instead. Since
// retryable errors are retried indefinitely, it returns false if the given timeout expires or the context is cancelled
// before that.
//
// Operator manager startup tasks that read the operator configuration resource can use this to avoid acting on the
// resource from before a Helm upgrade, which is only replaced once this replica has become leader.
func (r *AutoOperatorConfigurationResourceHandler) WaitForFirstCreateOrUpdate(
	ctx context.Context,
	timeout time.Duration,
) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.firstCreateOrUpdateDoneChan:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (r *AutoOperatorConfigurationResourceHandler) markFirstCreateOrUpdateAsDone() {
	r.firstCreateOrUpdateDoneOnce.Do(func() {
		close(r.firstCreateOrUpdateDoneChan)
	})
}

func (r *AutoOperatorConfigurationResourceHandler) loadExports() []dash0common.Export {
	extraConfigValues := r.extraConfigValues.Load()
	if extraConfigValues == nil {
		return nil
	}
	return extraConfigValues.exports
}

func (r *AutoOperatorConfigurationResourceHandler) validateOperatorConfiguration(exports []dash0common.Export) error {
	values := r.operatorConfigurationValues
	if values.Endpoint == "" && values.ApiEndpoint == "" && len(exports) == 0 {
		return fmt.Errorf(
			"invalid operator configuration: the operator configuration resource is managed via Helm, but neither " +
				"--operator-configuration-endpoint (Helm value operator.dash0Export.endpoint), " +
				"--operator-configuration-api-endpoint (Helm value operator.dash0Export.apiEndpoint) nor any export " +
				"(Helm value operator.exports) has been provided",
		)
	}
	if (values.Endpoint != "" || values.ApiEndpoint != "") && values.Token == "" {
		if values.SecretRef.Name == "" { //nolint:staticcheck
			return fmt.Errorf(
				"invalid operator configuration: --operator-configuration-endpoint or " +
					"--operator-configuration-api-endpoint has been provided, indicating that an operator " +
					"configuration resource should be created/updated, but neither --operator-configuration-token nor " +
					"--operator-configuration-secret-ref-name have been provided",
			)
		}
		if values.SecretRef.Key == "" { //nolint:staticcheck
			return fmt.Errorf(
				"invalid operator configuration: --operator-configuration-endpoint or " +
					"--operator-configuration-api-endpoint has been provided, indicating that an operator " +
					"configuration resource should be created/updated, but neither --operator-configuration-token nor " +
					"--operator-configuration-secret-ref-key have been provided",
			)
		}
	}
	if (values.TelemetryCollectionEnabled || values.SelfMonitoringEnabled) && values.Endpoint == "" && len(exports) == 0 {
		return fmt.Errorf(
			"invalid operator configuration: telemetry collection or self-monitoring is enabled, but there is no " +
				"export to send the telemetry to; either provide --operator-configuration-endpoint (Helm value " +
				"operator.dash0Export.endpoint) or an export (Helm value operator.exports), or, to only manage Dash0 " +
				"resources via the Dash0 API, disable telemetry collection and self-monitoring (Helm values " +
				"operator.telemetryCollectionEnabled=false and operator.selfMonitoringEnabled=false)",
		)
	}
	if r.operatorConfigurationValues.TelemetryCollectionEnabled &&
		r.operatorConfigurationValues.EbpfProfilerEnabled &&
		!r.operatorConfigurationValues.ProfilingEnabled {
		return fmt.Errorf(
			"invalid operator configuration: --operator-configuration-ebpf-profiler-enabled (Helm value " +
				"operator.ebpfProfilerEnabled) cannot be combined with --operator-configuration-profiling-enabled=false " +
				"(Helm value operator.profilingEnabled); set it to true or leave it unset",
		)
	}
	for i, export := range exports {
		if err := validateExportFromHelm(i, export); err != nil {
			return err
		}
	}
	return nil
}

func validateExportFromHelm(index int, export dash0common.Export) error {
	if export.Dash0 == nil && export.Grpc == nil && export.Http == nil {
		return fmt.Errorf(
			"invalid operator configuration: operator.exports[%d] has none of dash0, grpc or http set, at least one "+
				"of them is required",
			index,
		)
	}
	if export.Dash0 != nil && export.Dash0.Endpoint == "" {
		return fmt.Errorf("invalid operator configuration: operator.exports[%d].dash0 has no endpoint", index)
	}
	if export.Grpc != nil && export.Grpc.Endpoint == "" {
		return fmt.Errorf("invalid operator configuration: operator.exports[%d].grpc has no endpoint", index)
	}
	if export.Http != nil && export.Http.Endpoint == "" {
		return fmt.Errorf("invalid operator configuration: operator.exports[%d].http has no endpoint", index)
	}
	return nil
}

func parseMonitoringTemplate(
	extraConfigValues *operatorConfigurationExtraConfigValues,
) (*dash0v1alpha1.MonitoringTemplate, error) {
	if extraConfigValues == nil || extraConfigValues.monitoringTemplateRaw == nil {
		return nil, nil
	}
	monitoringTemplate := dash0v1alpha1.MonitoringTemplate{}
	if err := json.Unmarshal(*extraConfigValues.monitoringTemplateRaw, &monitoringTemplate); err != nil {
		return nil, fmt.Errorf("invalid operator configuration: the monitoring template cannot be parsed: %v", err)
	}
	return &monitoringTemplate, nil
}

func parseFilter(extraConfigValues *operatorConfigurationExtraConfigValues) (*dash0common.Filter, error) {
	if extraConfigValues == nil || extraConfigValues.filterRaw == nil {
		return nil, nil
	}
	filter := dash0common.Filter{}
	if err := json.Unmarshal(*extraConfigValues.filterRaw, &filter); err != nil {
		return nil, fmt.Errorf("invalid operator configuration: the filter cannot be parsed: %v", err)
	}
	return &filter, nil
}

func parseTransform(extraConfigValues *operatorConfigurationExtraConfigValues) (*dash0common.Transform, error) {
	if extraConfigValues == nil || extraConfigValues.transformRaw == nil {
		return nil, nil
	}
	transform := dash0common.Transform{}
	if err := json.Unmarshal(*extraConfigValues.transformRaw, &transform); err != nil {
		return nil, fmt.Errorf("invalid operator configuration: the transformation cannot be parsed: %v", err)
	}
	return &transform, nil
}

// supersedePendingAttempt cancels the attempt that has been started by a previous call of
// CreateOrUpdateOperatorConfigurationResource (it might still be retrying, with values that are outdated by now) and
// returns the context for the new attempt.
func (r *AutoOperatorConfigurationResourceHandler) supersedePendingAttempt(ctx context.Context) context.Context {
	r.pendingAttemptMutex.Lock()
	defer r.pendingAttemptMutex.Unlock()
	if r.cancelPendingAttempt != nil {
		r.cancelPendingAttempt(errAttemptSuperseded)
	}
	attemptCtx, cancel := context.WithCancelCause(ctx)
	r.cancelPendingAttempt = cancel
	return attemptCtx
}

func (r *AutoOperatorConfigurationResourceHandler) createOrUpdateOperatorConfigurationResourceUntilSuccessful(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) {
	defer func() {
		// A superseded attempt leaves signalling the first create/update as done to the attempt that superseded it.
		if !errors.Is(context.Cause(ctx), errAttemptSuperseded) {
			r.markFirstCreateOrUpdateAsDone()
		}
	}()

	// If multiple replicas are active, only the leader should attempt to create or update the operator
	// configuration resource.
	logger.Info(
		"waiting for this replica to become leader before creating or updating the Dash0 operator configuration " +
			"resource",
	)
	// Block until NotifiyOperatorManagerJustBecameLeader has been called.
	select {
	case <-ctx.Done():
		logger.Info(
			"no longer waiting for this replica to become leader, the attempt to create or update the Dash0 operator " +
				"configuration resource has been cancelled or superseded by a newer attempt",
		)
		return
	case <-r.hasBecomeLeaderChan:
		logger.Info("this replica is leader, proceeding with creating/updating the operator configuration resource")
	}

	delay := r.retryDelays.initial
	for {
		err := r.waitForWebhookServiceAndCreateOrUpdateOperatorConfigurationResource(
			ctx,
			operatorConfigurationResource,
			logger,
		)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			logAttemptHasBeenCancelled(logger)
			return
		}
		if !retry.IsRetryable(err) {
			logger.Error(
				err,
				"failed to create or update the Dash0 operator configuration resource, this will not be retried",
			)
			return
		}
		logger.Error(
			err,
			fmt.Sprintf(
				"failed to create or update the Dash0 operator configuration resource, trying again in %s",
				delay,
			),
		)
		select {
		case <-ctx.Done():
			logAttemptHasBeenCancelled(logger)
			return
		case <-time.After(delay):
		}
		delay = r.retryDelays.next(delay)
	}
}

func logAttemptHasBeenCancelled(logger logd.Logger) {
	logger.Info(
		"the attempt to create or update the Dash0 operator configuration resource has been cancelled or " +
			"superseded by a newer attempt",
	)
}

func (r *AutoOperatorConfigurationResourceHandler) waitForWebhookServiceAndCreateOrUpdateOperatorConfigurationResource(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	// There is a validation webhook for operator configuration resources. Thus, before we can create or update an
	// operator configuration resource, we need to wait for the webhook endpoint to become available.
	logger.Info(
		"waiting for the webhook service to become available before creating or updating the Dash0 " +
			"operator configuration resource",
	)
	if err := r.readyCheckExecuter.waitForWebhookServiceEndpointToBecomeReady(ctx, logger); err != nil {
		return err
	}
	logger.Info("the webhook service is available now")

	logger.Info("all validations and checks succeeded, creating/updating the Dash0 operator configuration resource now")
	return r.createOrUpdateOperatorConfigurationResourceWithRetry(ctx, operatorConfigurationResource, logger)
}

func (r *AutoOperatorConfigurationResourceHandler) createOrUpdateOperatorConfigurationResourceWithRetry(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	return retry.Retry(
		"create/update operator configuration resource",
		func() error {
			return r.createOrUpdateOperatorConfigurationResourceOnce(ctx, operatorConfigurationResource, logger)
		},
		r.createOrUpdateBackoff,
		&logger,
	)
}

func (r *AutoOperatorConfigurationResourceHandler) createOrUpdateOperatorConfigurationResourceOnce(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	if err := ctx.Err(); err != nil {
		return retry.NewRetryableError(err, false)
	}
	allOperatorConfigurationResources := &dash0v1alpha1.Dash0OperatorConfigurationList{}
	if err := r.List(ctx, allOperatorConfigurationResources); err != nil {
		return fmt.Errorf("failed to list all Dash0 operator configuration resources: %w", err)
	}

	if len(allOperatorConfigurationResources.Items) >= 1 {
		// The validation webhook for the operator configuration resource guarantees that there is only ever one
		// resource per cluster. Thus, we can arbitrarily update the first item in the list.
		existingOperatorConfigurationResource := allOperatorConfigurationResources.Items[0]
		// If this is a manually created operator configuration resource, we refuse to overwrite it.
		if existingOperatorConfigurationResource.Name != util.OperatorConfigurationAutoResourceName {
			//nolint:staticcheck
			return retry.NewRetryableError(
				fmt.Errorf(
					"The configuration provided via Helm instructs the operator manager to create/update an operator "+
						"configuration resource at startup, that is, operator.dash0Export.enabled is true and "+
						"operator.dash0Export.endpoint has been provided, or operator.exports is not empty. But there "+
						"is already an operator configuration resource in the cluster with the name %s that has not "+
						"been created by the operator manager. Replacing a manually created operator configuration "+
						"resource with values provided via Helm is not supported. Please either delete the existing "+
						"operator configuration resource or change the Helm values to not create an operator "+
						"configuration resource at startup, e.g. set operator.dash0Export.enabled to false, remove all "+
						"operator.dash0Export.* values and remove operator.exports.",
					existingOperatorConfigurationResource.Name,
				),
				// do not retry
				false,
			)
		}

		existingOperatorConfigurationResource.Spec = operatorConfigurationResource.Spec
		if err := r.Update(ctx, &existingOperatorConfigurationResource); err != nil {
			return fmt.Errorf("failed to update the Dash0 operator configuration resource: %w", err)
		}
		logger.Info("the Dash0 operator configuration resource has been updated")
		return nil
	}

	if err := r.Create(ctx, operatorConfigurationResource); err != nil {
		return fmt.Errorf("failed to create the Dash0 operator configuration resource: %w", err)
	}

	logger.Info("a Dash0 operator configuration resource has been created")
	return nil
}

func authorizationFromValues(operatorConfigurationValues OperatorConfigurationValues) dash0common.Authorization {
	authorization := dash0common.Authorization{}
	if operatorConfigurationValues.Token != "" {
		authorization.Token = &operatorConfigurationValues.Token
	} else {
		authorization.SecretRef = &dash0common.SecretRef{
			Name: operatorConfigurationValues.SecretRef.Name, //nolint:staticcheck
			Key:  operatorConfigurationValues.SecretRef.Key,  //nolint:staticcheck
		}
	}
	return authorization
}

func dash0ExportFromValues(operatorConfigurationValues OperatorConfigurationValues) dash0common.Export {
	dash0Configuration := &dash0common.Dash0Configuration{
		Endpoint:      operatorConfigurationValues.Endpoint,
		Authorization: authorizationFromValues(operatorConfigurationValues),
	}
	if operatorConfigurationValues.Dataset != "" {
		dash0Configuration.Dataset = operatorConfigurationValues.Dataset
	}
	if operatorConfigurationValues.KeepaliveTime != "" ||
		operatorConfigurationValues.KeepaliveTimeout != "" ||
		operatorConfigurationValues.KeepalivePermitWithoutStream {
		keepalive := &dash0common.KeepaliveClientConfig{}
		if operatorConfigurationValues.KeepaliveTime != "" {
			keepalive.Time = &operatorConfigurationValues.KeepaliveTime
		}
		if operatorConfigurationValues.KeepaliveTimeout != "" {
			keepalive.Timeout = &operatorConfigurationValues.KeepaliveTimeout
		}
		if operatorConfigurationValues.KeepalivePermitWithoutStream {
			keepalive.PermitWithoutStream = new(true)
		}
		dash0Configuration.Keepalive = keepalive
	}

	return dash0common.Export{Dash0: dash0Configuration}
}

func convertValuesToResource(
	operatorConfigurationValues OperatorConfigurationValues,
	monitoringTemplate *dash0v1alpha1.MonitoringTemplate,
	exportsFromExtraConfig []dash0common.Export,
	filter *dash0common.Filter,
	transform *dash0common.Transform,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	// The Dash0 export is derived from the operator.dash0Export.* Helm values, which are transported as command line
	// arguments; the exports from the Helm value operator.exports are transported via the extra config map. The Dash0
	// export comes first, since self-monitoring only uses the first export. The Dash0 API connections are derived from
	// the same sources in the same order; an apiEndpoint in an entry of operator.exports is converted into a dash0Api
	// entry, so the resource never uses the deprecated apiEndpoint field.
	var exports []dash0common.Export
	var dash0Api []dash0common.Dash0ApiConfiguration
	if operatorConfigurationValues.Endpoint != "" {
		exports = append(exports, dash0ExportFromValues(operatorConfigurationValues))
	}
	if operatorConfigurationValues.ApiEndpoint != "" {
		dash0Api = append(dash0Api, dash0common.Dash0ApiConfiguration{
			Endpoint:      operatorConfigurationValues.ApiEndpoint,
			Dataset:       operatorConfigurationValues.Dataset,
			Authorization: authorizationFromValues(operatorConfigurationValues),
		})
	}
	for _, export := range exportsFromExtraConfig {
		export := *export.DeepCopy()
		//nolint:staticcheck
		if export.Dash0 != nil && export.Dash0.ApiEndpoint != "" {
			dash0Api = append(dash0Api, dash0common.Dash0ApiConfiguration{
				//nolint:staticcheck
				Endpoint:      export.Dash0.ApiEndpoint,
				Dataset:       export.Dash0.Dataset,
				Authorization: export.Dash0.Authorization,
			})
			//nolint:staticcheck
			export.Dash0.ApiEndpoint = ""
		}
		if export.Http != nil && export.Http.Encoding == "" {
			export.Http.Encoding = dash0common.Proto
		}
		exports = append(exports, export)
	}

	if !operatorConfigurationValues.TelemetryCollectionEnabled {
		// Setting filters or transformations together with telemetryCollection.enabled=false is a validation error, so
		// they are dropped here, analogous to the collection settings below.
		filter = nil
		transform = nil
		if monitoringTemplate != nil {
			// Workloads are never instrumented while telemetry collection is disabled, and the operator configuration
			// validation webhook rejects a monitoring template with a mode other than none.
			monitoringTemplate = monitoringTemplate.DeepCopy()
			monitoringTemplate.Spec.InstrumentWorkloads.Mode = dash0common.InstrumentWorkloadsModeNone
		}
		operatorConfigurationValues.KubernetesInfrastructureMetricsCollectionEnabled = false
		operatorConfigurationValues.KubernetesObjectCollectionEnabled = false
		operatorConfigurationValues.CollectPodLabelsAndAnnotationsEnabled = false
		operatorConfigurationValues.CollectNamespaceLabelsAndAnnotationsEnabled = false
		operatorConfigurationValues.CollectNodeLabelsAndAnnotationsEnabled = false
		operatorConfigurationValues.PrometheusCrdSupportEnabled = false
		operatorConfigurationValues.ProfilingEnabled = false
		operatorConfigurationValues.EbpfProfilerEnabled = false
		operatorConfigurationValues.AutoMonitorNamespacesEnabled = false
	}

	spec := dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: new(operatorConfigurationValues.SelfMonitoringEnabled),
		},
		Exports:  exports,
		Dash0Api: dash0Api,
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: new(operatorConfigurationValues.KubernetesInfrastructureMetricsCollectionEnabled),
		},
		KubernetesObjectCollection: dash0v1alpha1.KubernetesObjectCollection{
			Enabled:                     new(operatorConfigurationValues.KubernetesObjectCollectionEnabled),
			CollectClusterScopedObjects: new(operatorConfigurationValues.CollectClusterScopedObjects),
		},
		CollectPodLabelsAndAnnotations: dash0v1alpha1.CollectPodLabelsAndAnnotations{
			Enabled: new(operatorConfigurationValues.CollectPodLabelsAndAnnotationsEnabled),
		},
		CollectNamespaceLabelsAndAnnotations: dash0v1alpha1.CollectNamespaceLabelsAndAnnotations{
			Enabled: new(operatorConfigurationValues.CollectNamespaceLabelsAndAnnotationsEnabled),
		},
		CollectNodeLabelsAndAnnotations: dash0v1alpha1.CollectNodeLabelsAndAnnotations{
			Enabled: new(operatorConfigurationValues.CollectNodeLabelsAndAnnotationsEnabled),
		},
		PrometheusCrdSupport: dash0v1alpha1.PrometheusCrdSupport{
			Enabled: new(operatorConfigurationValues.PrometheusCrdSupportEnabled),
		},
		ClusterName:        operatorConfigurationValues.ClusterName,
		Filter:             filter,
		Transform:          transform,
		MonitoringTemplate: monitoringTemplate,
		Profiling: &dash0v1alpha1.Profiling{
			Enabled: new(operatorConfigurationValues.ProfilingEnabled),
		},
		TelemetryCollection: dash0v1alpha1.TelemetryCollection{
			Enabled: new(operatorConfigurationValues.TelemetryCollectionEnabled),
		},
		AutoMonitorNamespaces: dash0v1alpha1.AutoMonitorNamespaces{
			Enabled:       new(operatorConfigurationValues.AutoMonitorNamespacesEnabled),
			LabelSelector: operatorConfigurationValues.AutoMonitorNamespacesLabelSelector,
		},
	}
	if operatorConfigurationValues.EbpfProfilerEnabled {
		spec.Profiling.EbpfProfiler = &dash0v1alpha1.EbpfProfiler{
			Enabled:                     new(true),
			PIDNamespaceTranslationMode: operatorConfigurationValues.EbpfProfilerPIDNamespaceTranslationMode,
		}
	}
	if operatorConfigurationValues.InstrumentationDelivery != "" {
		spec.InstrumentWorkloads = dash0v1alpha1.InstrumentWorkloads{
			InstrumentationDelivery: operatorConfigurationValues.InstrumentationDelivery,
		}
	}

	operatorConfigurationResource := dash0v1alpha1.Dash0OperatorConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: util.OperatorConfigurationAutoResourceName,
			Annotations: map[string]string{
				// For clusters managed by ArgoCD, we need to prevent ArgoCD to sync or prune resources that are not
				// created directly via the Helm chart and that have no owner reference. These are all cluster-scoped
				// resources not created via Helm, like cluster roles & cluster role bindings, but also the operator
				// configuration resource we create here. See also:
				// * https://github.com/argoproj/argo-cd/issues/4764#issuecomment-722661940 -- this is where they say
				//   that only top level resources are pruned (that is basically the same as resources without an owner
				//   reference).
				// * The docs for preventing this on a resource level are here:
				//   https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#no-prune-resources
				//   https://argo-cd.readthedocs.io/en/stable/user-guide/compare-options/#ignoring-resources-that-are-extraneous
				argoCdAyncOptionsAnnotationKey:    "Prune=false",
				argoCdCompareOptionsAnnotationKey: "IgnoreExtraneous",
				managedByHelmAnnotationKey: "DO NOT EDIT THIS RESOURCE. This operator configuration resource is " +
					"managed by the operator Helm chart (Helm values operator.dash0Export.* and operator.exports), " +
					"manual modifications to this resource (i.e. via kubectl or k9s) will be overwritten when the " +
					"operator manager is restarted or the operator is updated to a new version. See " +
					"https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/docs/configuration.md#" +
					"notes-on-creating-the-operator-configuration-resource-via-helm.",
			},
		},
		Spec: spec,
	}

	return &operatorConfigurationResource
}
