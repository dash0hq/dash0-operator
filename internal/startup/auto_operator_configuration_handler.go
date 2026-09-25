// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
	InstrumentationDelivery                          dash0v1alpha1.InstrumentationDelivery
	CollectPodLabelsAndAnnotationsEnabled            bool
	CollectNamespaceLabelsAndAnnotationsEnabled      bool
	CollectNodeLabelsAndAnnotationsEnabled           bool
	PrometheusCrdSupportEnabled                      bool
	ProfilingEnabled                                 bool
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
	monitoringTemplateRaw       atomic.Pointer[json.RawMessage]
	exports                     atomic.Pointer[[]dash0common.Export]
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
	monitoringTemplateRaw *json.RawMessage,
	exports []dash0common.Export,
) *AutoOperatorConfigurationResourceHandler {
	r := &AutoOperatorConfigurationResourceHandler{
		Client:                      client,
		readyCheckExecuter:          readyCheckExecuter,
		hasBecomeLeaderChan:         make(chan struct{}),
		operatorConfigurationValues: operatorConfigurationValues,
	}
	r.monitoringTemplateRaw.Store(monitoringTemplateRaw)
	r.exports.Store(&exports)
	return r
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
	logger.Debug("extra config map update, checking for monitoring template and export changes")
	previousMonitoringTemplate := r.monitoringTemplateRaw.Swap(extraConfig.MonitoringTemplateRaw)
	previousExports := r.exports.Swap(&extraConfig.Exports)

	monitoringTemplateHasChanged := false
	if previousMonitoringTemplate == nil || extraConfig.MonitoringTemplateRaw == nil {
		monitoringTemplateHasChanged = previousMonitoringTemplate != extraConfig.MonitoringTemplateRaw
	} else {
		monitoringTemplateHasChanged = !reflect.DeepEqual(*previousMonitoringTemplate, *extraConfig.MonitoringTemplateRaw)
	}

	exportsHaveChanged := previousExports == nil || !reflect.DeepEqual(*previousExports, extraConfig.Exports)

	if !monitoringTemplateHasChanged && !exportsHaveChanged {
		logger.Debug(
			"ignoring extra config map update, the monitoring template and the exports have not changed",
		)
		return
	}

	logger.Info("updating the operator configuration resource after the monitoring template or the exports have been updated")
	if _, err := r.CreateOrUpdateOperatorConfigurationResource(
		ctx,
		logger,
	); err != nil {
		logger.Error(
			err,
			"Failed to update the Dash0 operator configuration resource after updating the monitoring template or the "+
				"exports via Helm.",
		)
	}
}

// CreateOrUpdateOperatorConfigurationResource waits until this replica becomes the leader, then it creates or updates
// the Dash0 operator configuration resource. The function will create/update the resource asynchronously, that is, when
// the function returns the resource might not have been created/updated yet. The function will optimistically return
// the resource that is going to be created/updated, without guarantees that the resource will be created/updated
// successfully.
func (r *AutoOperatorConfigurationResourceHandler) CreateOrUpdateOperatorConfigurationResource(
	ctx context.Context,
	logger logd.Logger,
) (*dash0v1alpha1.Dash0OperatorConfiguration, error) {
	logger.Info("running validations and checks for creating/updating the Dash0 operator configuration resource")
	exports := r.loadExports()
	if err := r.validateOperatorConfiguration(exports); err != nil {
		return nil, err
	}
	monitoringTemplate, err := r.parseMonitoringTemplate()
	if err != nil {
		return nil, err
	}

	operatorConfigurationResource := convertValuesToResource(r.operatorConfigurationValues, monitoringTemplate, exports)
	go func() {
		// If multiple replicas are active, only the leader should attempt to create or update the operator
		// configuration resource.
		logger.Info(
			"waiting for this replica to become leader before creating or updating the Dash0 operator configuration " +
				"resource",
		)
		// Block until NotifiyOperatorManagerJustBecameLeader has been called.
		select {
		case <-ctx.Done():
			logger.Error(ctx.Err(), "context cancelled while waiting for this replica to become leader")
			return
		case <-r.hasBecomeLeaderChan:
			logger.Info("this replica is leader, proceeding with creating/updating the operator configuration resource")
		}

		// There is a validation webhook for operator configuration resources. Thus, before we can create or update an
		// operator configuration resource, we need to wait for the webhook endpoint to become available.
		logger.Info(
			"waiting for the webhook service to become available before creating or updating the Dash0 " +
				"operator configuration resource",
		)
		if webhookServiceIsAvailable, err := r.readyCheckExecuter.waitForWebhookServiceEndpointToBecomeReady(
			ctx,
			setupLog,
		); err != nil {
			logger.Error(err, "failed to create or update the Dash0 operator configuration resource")
			return
		} else if !webhookServiceIsAvailable {
			logger.Error(
				fmt.Errorf("cannot create or update the Dash0 operator configuration resource because the webhook service did not become available"),
				"failed to create or update the Dash0 operator configuration resource",
			)
			return
		}
		logger.Info("the webhook service is available now")

		logger.Info("all validations and checks succeeded, creating/updating the Dash0 operator configuration resource now")
		if err := r.createOrUpdateOperatorConfigurationResourceWithRetry(
			ctx,
			operatorConfigurationResource,
			logger,
		); err != nil {
			logger.Error(err, "failed to create or update the Dash0 operator configuration resource")
			return
		}
	}()

	// optimistically return the resource that we are going to create once the validation webhook becomes available
	return operatorConfigurationResource, nil
}

func (r *AutoOperatorConfigurationResourceHandler) loadExports() []dash0common.Export {
	exports := r.exports.Load()
	if exports == nil {
		return nil
	}
	return *exports
}

func (r *AutoOperatorConfigurationResourceHandler) validateOperatorConfiguration(exports []dash0common.Export) error {
	if r.operatorConfigurationValues.Endpoint == "" && len(exports) == 0 {
		return fmt.Errorf(
			"invalid operator configuration: the operator configuration resource is managed via Helm, but neither " +
				"--operator-configuration-endpoint (Helm value operator.dash0Export.endpoint) nor any export (Helm " +
				"value operator.exports) has been provided",
		)
	}
	if r.operatorConfigurationValues.Endpoint != "" && r.operatorConfigurationValues.Token == "" {
		if r.operatorConfigurationValues.SecretRef.Name == "" { //nolint:staticcheck
			return fmt.Errorf(
				"invalid operator configuration: --operator-configuration-endpoint has been provided, " +
					"indicating that an operator configuration resource should be created/updated, but neither " +
					"--operator-configuration-token nor --operator-configuration-secret-ref-name have been provided",
			)
		}
		if r.operatorConfigurationValues.SecretRef.Key == "" { //nolint:staticcheck
			return fmt.Errorf(
				"invalid operator configuration: --operator-configuration-endpoint has been provided, " +
					"indicating that an operator configuration resource should be created/updated, but neither " +
					"--operator-configuration-token nor --operator-configuration-secret-ref-key have been provided",
			)
		}
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

func (r *AutoOperatorConfigurationResourceHandler) parseMonitoringTemplate() (*dash0v1alpha1.MonitoringTemplate, error) {
	monitoringTemplateRaw := r.monitoringTemplateRaw.Load()
	if monitoringTemplateRaw == nil {
		return nil, nil
	}
	monitoringTemplate := dash0v1alpha1.MonitoringTemplate{}
	if err := json.Unmarshal(*monitoringTemplateRaw, &monitoringTemplate); err != nil {
		return nil, fmt.Errorf("invalid operator configuration: the monitoring template cannot be parsed: %v", err)
	}
	return &monitoringTemplate, nil
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
		wait.Backoff{
			Duration: 3 * time.Second,
			Factor:   1.5,
			Steps:    6,
		},
		&logger,
	)
}

func (r *AutoOperatorConfigurationResourceHandler) createOrUpdateOperatorConfigurationResourceOnce(
	ctx context.Context,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
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

func dash0ExportFromValues(operatorConfigurationValues OperatorConfigurationValues) dash0common.Export {
	authorization := dash0common.Authorization{}
	if operatorConfigurationValues.Token != "" {
		authorization.Token = &operatorConfigurationValues.Token
	} else {
		authorization.SecretRef = &dash0common.SecretRef{
			Name: operatorConfigurationValues.SecretRef.Name, //nolint:staticcheck
			Key:  operatorConfigurationValues.SecretRef.Key,  //nolint:staticcheck
		}
	}

	dash0Configuration := &dash0common.Dash0Configuration{
		Endpoint:      operatorConfigurationValues.Endpoint,
		Authorization: authorization,
	}
	if operatorConfigurationValues.ApiEndpoint != "" {
		dash0Configuration.ApiEndpoint = operatorConfigurationValues.ApiEndpoint
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
) *dash0v1alpha1.Dash0OperatorConfiguration {
	// The Dash0 export is derived from the operator.dash0Export.* Helm values, which are transported as command line
	// arguments; the exports from the Helm value operator.exports are transported via the extra config map. The Dash0
	// export comes first, since self-monitoring only uses the first export.
	var exports []dash0common.Export
	if operatorConfigurationValues.Endpoint != "" {
		exports = append(exports, dash0ExportFromValues(operatorConfigurationValues))
	}
	for _, export := range exportsFromExtraConfig {
		export := *export.DeepCopy()
		if export.Http != nil && export.Http.Encoding == "" {
			export.Http.Encoding = dash0common.Proto
		}
		exports = append(exports, export)
	}

	if !operatorConfigurationValues.TelemetryCollectionEnabled {
		operatorConfigurationValues.KubernetesInfrastructureMetricsCollectionEnabled = false
		operatorConfigurationValues.CollectPodLabelsAndAnnotationsEnabled = false
		operatorConfigurationValues.CollectNamespaceLabelsAndAnnotationsEnabled = false
		operatorConfigurationValues.CollectNodeLabelsAndAnnotationsEnabled = false
		operatorConfigurationValues.PrometheusCrdSupportEnabled = false
		operatorConfigurationValues.ProfilingEnabled = false
		operatorConfigurationValues.AutoMonitorNamespacesEnabled = false
	}

	spec := dash0v1alpha1.Dash0OperatorConfigurationSpec{
		SelfMonitoring: dash0v1alpha1.SelfMonitoring{
			Enabled: new(operatorConfigurationValues.SelfMonitoringEnabled),
		},
		Exports: exports,
		KubernetesInfrastructureMetricsCollection: dash0v1alpha1.KubernetesInfrastructureMetricsCollection{
			Enabled: new(operatorConfigurationValues.KubernetesInfrastructureMetricsCollectionEnabled),
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
