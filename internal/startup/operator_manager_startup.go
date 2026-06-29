// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	"github.com/go-logr/zapr"
	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	persesv1alpha2 "github.com/perses/perses-operator/api/v1alpha2"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/agent0connector"
	"github.com/dash0hq/dash0-operator/internal/agent0connector/a0cresources"
	"github.com/dash0hq/dash0-operator/internal/allowlistreadycheck"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/controller"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/intelligentedge"
	"github.com/dash0hq/dash0-operator/internal/intelligentedge/ieresources"
	"github.com/dash0hq/dash0-operator/internal/postdelete"
	"github.com/dash0hq/dash0-operator/internal/postinstall"
	"github.com/dash0hq/dash0-operator/internal/predelete"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/targetallocator"
	"github.com/dash0hq/dash0-operator/internal/targetallocator/taresources"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"
	"github.com/dash0hq/dash0-operator/internal/webhooks"
)

type environmentVariables struct {
	operatorNamespace                           string
	deploymentName                              string
	webhookServiceName                          string
	webhookServicePort                          int32
	iacPeriodicRetryEnabled                     bool
	iacPeriodicRetryInterval                    time.Duration
	autoPatchPersesDashboardConversionWebhook   bool
	oTelCollectorNamePrefix                     string
	targetAllocatorNamePrefix                   string
	operatorImage                               string
	instrumentationImage                        string
	instrumentationImagePullPolicy              corev1.PullPolicy
	collectorImage                              string
	collectorImagePullPolicy                    corev1.PullPolicy
	targetAllocatorImage                        string
	targetAllocatorImagePullPolicy              corev1.PullPolicy
	configurationReloaderImage                  string
	configurationReloaderImagePullPolicy        corev1.PullPolicy
	filelogOffsetSyncImage                      string
	filelogOffsetSyncImagePullPolicy            corev1.PullPolicy
	filelogOffsetVolumeOwnershipImage           string
	filelogOffsetVolumeOwnershipImagePullPolicy corev1.PullPolicy
	barkerImage                                 string
	barkerImagePullPolicy                       corev1.PullPolicy
	agent0ConnectorImage                        string
	agent0ConnectorImagePullPolicy              corev1.PullPolicy
	agent0ConnectorEnabled                      bool
	agent0ConnectorServerAddress                string
	agent0ConnectorInsecure                     bool
	agent0ConnectorToken                        string
	agent0ConnectorSecretRefName                string
	agent0ConnectorSecretRefKey                 string
	nodeIp                                      string
	nodeName                                    string
	podIp                                       string
	sendBatchSize                               *uint32
	sendBatchMaxSize                            *uint32
	k8sAttributesDisableReplicasetInformer      bool
	k8sAttributesWaitForMetadata                bool
	k8sAttributesWaitForMetadataTimeout         string
	instrumentationDebug                        bool
	enablePythonAutoInstrumentation             bool
	debugVerbosityDetailed                      bool
	disableCollectorResourceWatches             bool
	enablePprofExtension                        bool
	compressConfigMaps                          bool
}

type commandLineArguments struct {
	isUninstrumentAll                                                     bool
	autoOperatorConfigurationResourceAvailableCheck                       bool
	allowlistSynchronizerReadyCheck                                       bool
	allowlistVersion                                                      string
	deleteAllowlistSynchronizer                                           bool
	operatorConfigurationEndpoint                                         string
	operatorConfigurationToken                                            string
	operatorConfigurationSecretRefName                                    string
	operatorConfigurationSecretRefKey                                     string
	operatorConfigurationDataset                                          string
	operatorConfigurationApiEndpoint                                      string
	operatorConfigurationKeepaliveTime                                    string
	operatorConfigurationKeepaliveTimeout                                 string
	operatorConfigurationKeepalivePermitWithoutStream                     bool
	operatorConfigurationSelfMonitoringEnabled                            bool
	operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled bool
	operatorConfigurationCollectPodLabelsAndAnnotationsEnabled            bool
	operatorConfigurationCollectNamespaceLabelsAndAnnotationsEnabled      bool
	operatorConfigurationPrometheusCrdSupportEnabled                      bool
	operatorConfigurationProfilingEnabled                                 bool
	operatorConfigurationClusterName                                      string
	operatorConfigurationAutoMonitorNamespacesEnabled                     bool
	operatorConfigurationAutoMonitorNamespacesLabelSelector               string
	operatorConfigurationInstrumentationDelivery                          string
	telemetryCollectionEnabled                                            bool
	featureIntelligentEdgeEnabled                                         bool
	forceUseOpenTelemetryCollectorServiceUrl                              bool
	isGkeAutopilot                                                        bool
	disableOpenTelemetryCollectorHostPorts                                bool
	instrumentationDelays                                                 *util.DelayConfig
	metricsAddr                                                           string
	enableLeaderElection                                                  bool
	probeAddr                                                             string
	secureMetrics                                                         bool
	enableHTTP2                                                           bool
	logLevel                                                              string
}

const (
	operatorNamespaceEnvVarName                           = "DASH0_OPERATOR_NAMESPACE"
	deploymentNameEnvVarName                              = "DASH0_DEPLOYMENT_NAME"
	webhookServiceNameEnvVarName                          = "DASH0_WEBHOOK_SERVICE_NAME"
	webhookServicePortEnvVarName                          = "DASH0_WEBHOOK_SERVICE_PORT"
	persesDashboardAutoPatchConversionWebhookEnvVarName   = "DASH0_PERSES_DASHBOARD_AUTO_PATCH_CONVERSION_WEBHOOK"
	periodicRetryEnabledEnvVarName                        = "DASH0_IAC_PERIODIC_RETRY_ENABLED"
	periodicRetryIntervalEnvVarName                       = "DASH0_IAC_PERIODIC_RETRY_INTERVAL"
	oTelCollectorNamePrefixEnvVarName                     = "OTEL_COLLECTOR_NAME_PREFIX"
	targetAllocatorNamePrefixEnvVarName                   = "OTEL_TARGET_ALLOCATOR_NAME_PREFIX"
	operatorImageEnvVarName                               = "DASH0_OPERATOR_IMAGE"
	instrumentationImageEnvVarName                        = "DASH0_INSTRUMENTATION_IMAGE"
	instrumentationImagePullPolicyEnvVarName              = "DASH0_INSTRUMENTATION_IMAGE_PULL_POLICY"
	collectorImageEnvVarName                              = "DASH0_COLLECTOR_IMAGE"
	collectorImageImagePullPolicyEnvVarName               = "DASH0_COLLECTOR_IMAGE_PULL_POLICY"
	targetAllocatorImageEnvVarName                        = "DASH0_TARGET_ALLOCATOR_IMAGE"
	targetAllocatorImageImagePullPolicyEnvVarName         = "DASH0_TARGET_ALLOCATOR_IMAGE_PULL_POLICY"
	configurationReloaderImageEnvVarName                  = "DASH0_CONFIGURATION_RELOADER_IMAGE"
	configurationReloaderImagePullPolicyEnvVarName        = "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY"
	filelogOffsetSyncImageEnvVarName                      = "DASH0_FILELOG_OFFSET_SYNC_IMAGE"
	filelogOffsetSyncImagePullPolicyEnvVarName            = "DASH0_FILELOG_OFFSET_SYNC_IMAGE_PULL_POLICY"
	filelogOffsetVolumeOwnershipImageEnvVarName           = "DASH0_FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE"
	filelogOffsetVolumeOwnershipImagePullPolicyEnvVarName = "DASH0_FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_PULL_POLICY"
	barkerImageEnvVarName                                 = "DASH0_BARKER_IMAGE"
	barkerImagePullPolicyEnvVarName                       = "DASH0_BARKER_IMAGE_PULL_POLICY"
	agent0ConnectorImageEnvVarName                        = "DASH0_AGENT0_CONNECTOR_IMAGE"
	agent0ConnectorImagePullPolicyEnvVarName              = "DASH0_AGENT0_CONNECTOR_IMAGE_PULL_POLICY"
	agent0ConnectorEnabledEnvVarName                      = "DASH0_AGENT0_CONNECTOR_ENABLED"
	agent0ConnectorServerAddressEnvVarName                = "DASH0_AGENT0_CONNECTOR_SERVER_ADDRESS"
	agent0ConnectorInsecureEnvVarName                     = "DASH0_AGENT0_CONNECTOR_INSECURE"
	agent0ConnectorTokenEnvVarName                        = "DASH0_AGENT0_CONNECTOR_TOKEN"
	agent0ConnectorSecretRefNameEnvVarName                = "DASH0_AGENT0_CONNECTOR_SECRET_REF_NAME"
	agent0ConnectorSecretRefKeyEnvVarName                 = "DASH0_AGENT0_CONNECTOR_SECRET_REF_KEY"
	k8sNodeIpEnvVarName                                   = "K8S_NODE_IP"
	k8sNodeNameEnvVarName                                 = "K8S_NODE_NAME"
	k8sPodIpEnvVarName                                    = "K8S_POD_IP"

	developmentModeEnvVarName                        = "DASH0_DEVELOPMENT_MODE"
	pprofPortEnvVarName                              = "DASH0_PPROF_PORT"
	instrumentationDebugEnvVarName                   = "DASH0_INSTRUMENTATION_DEBUG"
	enablePythonAutoInstrumentationEnvVarName        = "DASH0_ENABLE_PYTHON_AUTO_INSTRUMENTATION"
	disableCollectorResourceWatchesEnvVarName        = "DASH0_DISABLE_COLLECTOR_RESOURCE_WATCHES"
	debugVerbosityDetailedEnvVarName                 = "OTEL_COLLECTOR_DEBUG_VERBOSITY_DETAILED"
	sendBatchSizeEnvVarName                          = "OTEL_COLLECTOR_SEND_BATCH_SIZE"
	sendBatchMaxSizeEnvVarName                       = "OTEL_COLLECTOR_SEND_BATCH_MAX_SIZE"
	k8sAttributesDisableReplicasetInformerEnvVarName = "OTEL_COLLECTOR_K8SATTRIBUTES_DISABLE_REPLICASET_INFORMER"
	k8sAttributesWaitForMetadataEnvVarName           = "OTEL_COLLECTOR_K8SATTRIBUTES_WAIT_FOR_METADATA"
	k8sAttributesWaitForMetadataTimeoutEnvVarName    = "OTEL_COLLECTOR_K8SATTRIBUTES_WAIT_FOR_METADATA_TIMEOUT"
	enablePprofExtensionEnvVarName                   = "OTEL_COLLECTOR_ENABLE_PPROF_EXTENSION"
	compressConfigMapsEnvVarName                     = "OTEL_COLLECTOR_COMPRESS_CONFIG_MAPS"

	//nolint
	mandatoryEnvVarMissingMessageTemplate = "cannot start the Dash0 operator, the mandatory environment variable \"%s\" is missing"

	envVarValueTrue = "true"

	// defaultPeriodicRetryInterval is the fallback interval for the periodic synchronization retry job, used when
	// DASH0_IAC_PERIODIC_RETRY_INTERVAL is unset or cannot be parsed as a positive duration.
	defaultPeriodicRetryInterval = 10 * time.Minute
)

var (
	setupLog logd.Logger

	leaderElectionAwareRunnable     = util.NewLeaderElectionAwareRunnable()
	startupTasksK8sClient           client.Client
	isDocker                        bool
	oTelSdkStarter                  *selfmonitoringapiaccess.OTelSdkStarter
	operatorDeploymentSelfReference *appsv1.Deployment
	envVars                         environmentVariables
	extraConfig                     util.ExtraConfig
	extraConfigMapWatcher           = util.NewExtraConfigWatcher()

	thirdPartyResourceSynchronizationQueue *workqueue.Typed[controller.ThirdPartyResourceSyncJob]
)

var (
	runtimeScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(runtimeScheme))
	utilruntime.Must(dash0v1alpha1.AddToScheme(runtimeScheme))
	utilruntime.Must(dash0v1beta1.AddToScheme(runtimeScheme))

	// required for Perses dashboard controller and Prometheus rules controller.
	utilruntime.Must(apiextensionsv1.AddToScheme(runtimeScheme))
	utilruntime.Must(persesv1alpha1.AddToScheme(runtimeScheme))
	utilruntime.Must(persesv1alpha2.AddToScheme(runtimeScheme))
	utilruntime.Must(prometheusv1.AddToScheme(runtimeScheme))
}

func Start() {
	ctx := context.Background()

	developmentModeRaw, isSet := os.LookupEnv(developmentModeEnvVarName)
	developmentMode := isSet && strings.ToLower(developmentModeRaw) == envVarValueTrue

	cliArgs := defineCommandLineArguments()
	opts := parseCommandLineOptions(cliArgs, developmentMode)
	crZapOpts := crzap.UseFlagOptions(&opts)

	// Maintenance note: setupLog is not yet initialized before the call to setUpLogging.
	delegatingZapCoreWrapper := setUpLogging(crZapOpts, developmentMode)
	// setupLog is initialized after this point and can be used

	setupLog.Debug("development/debug mode enabled")

	pprofPort := os.Getenv(pprofPortEnvVarName)
	if pprofPort != "" {
		go func() {
			setupLog.Warn(
				"starting pprof server (do not use in production unless instructed by Dash0 support to do so)",
				"port",
				pprofPort,
			)
			if err := http.ListenAndServe(fmt.Sprintf(":%s", pprofPort), nil); err != nil {
				if errors.Is(err, http.ErrServerClosed) {
					setupLog.Info("pprof server has been closed")
				} else {
					setupLog.Error(err, "error in pprof server")
				}
			}
		}()
	}

	if cliArgs.isUninstrumentAll {
		if err := deleteMonitoringResourcesInAllNamespaces(setupLog); err != nil {
			setupLog.Error(err, "deleting the Dash0 monitoring resources in all namespaces failed")
			os.Exit(1)
		}
		os.Exit(0)
	}
	if cliArgs.autoOperatorConfigurationResourceAvailableCheck {
		if err := waitForOperatorConfigurationResourceAvailability(setupLog); err != nil {
			setupLog.Error(err, "waiting for the Dash0 operator configuration resource to become available has failed")
			os.Exit(1)
		}
		os.Exit(0)
	}
	if cliArgs.allowlistSynchronizerReadyCheck {
		if cliArgs.allowlistVersion == "" {
			setupLog.Error(
				fmt.Errorf("missing required flag"),
				"--allowlist-version is required when using --allowlist-synchronizer-ready-check",
			)
			os.Exit(1)
		}
		if err := waitForAllowlistSynchronizerReadiness(setupLog, cliArgs.allowlistVersion); err != nil {
			setupLog.Error(err, "waiting for the AllowlistSynchronizer resource to become ready has failed")
			os.Exit(1)
		}
		os.Exit(0)
	}
	if cliArgs.deleteAllowlistSynchronizer {
		if err := deleteDash0AllowlistSynchronizer(ctx, setupLog); err != nil {
			setupLog.Error(err, "deleting the AllowlistSynchronizer resource failed")
			os.Exit(1)
		}
		os.Exit(0)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	var tlsOpts []func(*tls.Config)
	if !cliArgs.enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := k8swebhook.NewServer(
		k8swebhook.Options{
			TLSOpts: tlsOpts,
		},
	)

	var err error
	if err = readEnvironmentVariables(setupLog); err != nil {
		setupLog.Error(err, "cannot read environment variables")
		os.Exit(1)
	}
	if err = readExtraConfigMap(); err != nil {
		setupLog.Error(err, "cannot read extra config map file at startup")
		os.Exit(1)
	}
	if err = extraConfigMapWatcher.StartWatch(setupLog); err != nil {
		setupLog.Error(err, "cannot establish file watch for extra config map")
		os.Exit(1)
	}

	if err = initStartupTasksK8sClient(setupLog); err != nil {
		os.Exit(1)
	}
	detectDocker(
		ctx,
		startupTasksK8sClient,
		setupLog,
	)
	if operatorDeploymentSelfReference, err = findDeploymentReference(
		ctx,
		startupTasksK8sClient,
		envVars.operatorNamespace,
		envVars.deploymentName,
		setupLog,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager lookup for its own deployment failed.")
		os.Exit(1)
	}

	var operatorConfigurationValues *OperatorConfigurationValues
	if operatorConfigurationIsManagedViaHelm(cliArgs) {
		operatorConfigurationValues = &OperatorConfigurationValues{
			Endpoint: cliArgs.operatorConfigurationEndpoint,
			Token:    cliArgs.operatorConfigurationToken,
			SecretRef: SecretRef{
				Name: cliArgs.operatorConfigurationSecretRefName,
				Key:  cliArgs.operatorConfigurationSecretRefKey,
			},
			SelfMonitoringEnabled: cliArgs.operatorConfigurationSelfMonitoringEnabled,
			//nolint:lll
			KubernetesInfrastructureMetricsCollectionEnabled: cliArgs.operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled,
			InstrumentationDelivery:                          cliArgs.operatorConfigurationInstrumentationDelivery,
			CollectPodLabelsAndAnnotationsEnabled:            cliArgs.operatorConfigurationCollectPodLabelsAndAnnotationsEnabled,
			CollectNamespaceLabelsAndAnnotationsEnabled:      cliArgs.operatorConfigurationCollectNamespaceLabelsAndAnnotationsEnabled,
			PrometheusCrdSupportEnabled:                      cliArgs.operatorConfigurationPrometheusCrdSupportEnabled,
			ProfilingEnabled:                                 cliArgs.operatorConfigurationProfilingEnabled,
			TelemetryCollectionEnabled:                       cliArgs.telemetryCollectionEnabled,
			ClusterName:                                      cliArgs.operatorConfigurationClusterName,
			AutoMonitorNamespacesEnabled:                     cliArgs.operatorConfigurationAutoMonitorNamespacesEnabled,
			AutoMonitorNamespacesLabelSelector:               cliArgs.operatorConfigurationAutoMonitorNamespacesLabelSelector,
		}
		if len(cliArgs.operatorConfigurationApiEndpoint) > 0 {
			operatorConfigurationValues.ApiEndpoint = cliArgs.operatorConfigurationApiEndpoint
		}
		if len(cliArgs.operatorConfigurationDataset) > 0 {
			operatorConfigurationValues.Dataset = cliArgs.operatorConfigurationDataset
		}
		if len(cliArgs.operatorConfigurationKeepaliveTime) > 0 {
			operatorConfigurationValues.KeepaliveTime = cliArgs.operatorConfigurationKeepaliveTime
		}
		if len(cliArgs.operatorConfigurationKeepaliveTimeout) > 0 {
			operatorConfigurationValues.KeepaliveTimeout = cliArgs.operatorConfigurationKeepaliveTimeout
		}
		if cliArgs.operatorConfigurationKeepalivePermitWithoutStream {
			operatorConfigurationValues.KeepalivePermitWithoutStream = true
		}
	}

	if err = startOperatorManager(
		ctx,
		cliArgs,
		tlsOpts,
		webhookServer,
		operatorConfigurationValues,
		delegatingZapCoreWrapper,
		developmentMode,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager process failed to start.")
		os.Exit(1)
	}
}

func defineCommandLineArguments() *commandLineArguments {
	cliArgs := &commandLineArguments{}
	flag.BoolVar(
		&cliArgs.isUninstrumentAll,
		"uninstrument-all",
		false,
		"If set, the process will remove all Dash0 monitoring resources from all namespaces in the cluster, then "+
			"exit. This will trigger the Dash0 monitoring resources' finalizers in each namespace, which in turn will "+
			"revert the instrumentation of all workloads in all namespaces.",
	)
	flag.BoolVar(
		&cliArgs.autoOperatorConfigurationResourceAvailableCheck,
		"auto-operator-configuration-resource-available-check",
		false,
		"If set, the process will only wait until the Dash0 operator configuration resource has been created and "+
			"becomes available, then exit.",
	)
	flag.BoolVar(
		&cliArgs.allowlistSynchronizerReadyCheck,
		"allowlist-synchronizer-ready-check",
		false,
		"If set, the process will wait until the GKE Autopilot AllowlistSynchronizer resource is ready, then exit.",
	)
	flag.StringVar(
		&cliArgs.allowlistVersion,
		"allowlist-version",
		"",
		"The version of the Dash0 operator allowlist to wait for (e.g. v1.0.3). Used with --allowlist-synchronizer-ready-check.",
	)
	flag.BoolVar(
		&cliArgs.deleteAllowlistSynchronizer,
		"delete-allowlist-synchronizer",
		false,
		"If set, the process will remove the GKE Autopilot AllowlistSynchronizer resource from the cluster, then "+
			"exit.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationEndpoint,
		"operator-configuration-endpoint",
		"",
		"The Dash0 endpoint gRPC URL for creating an operator configuration resource.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationToken,
		"operator-configuration-token",
		"",
		"The Dash0 auth token for creating an operator configuration resource.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationSecretRefName,
		"operator-configuration-secret-ref-name",
		"",
		"The name of an existing Kubernetes secret containing the Dash0 auth token, used to creating an operator "+
			"configuration resource.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationSecretRefKey,
		"operator-configuration-secret-ref-key",
		"",
		"The key in an existing Kubernetes secret containing the Dash0 auth token, used to creating an operator "+
			"configuration resource.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationDataset,
		"operator-configuration-dataset",
		"default",
		"The Dash0 dataset into which telemetry will be reported and which will be used for API access.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationApiEndpoint,
		"operator-configuration-api-endpoint",
		"",
		"The Dash0 API endpoint for managing dashboards, check rules, synthetic checks and views via the operator.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationKeepaliveTime,
		"operator-configuration-keepalive-time",
		"",
		"The keepalive time for the gRPC connection to the Dash0 backend; will be ignored if "+
			"operator-configuration-endpoint is not set.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationKeepaliveTimeout,
		"operator-configuration-keepalive-timeout",
		"",
		"The keepalive timeout for the gRPC connection to the Dash0 backend; will be ignored if "+
			"operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationKeepalivePermitWithoutStream,
		"operator-configuration-keepalive-permit-without-stream",
		false,
		"Whether to allow keepalive pings when there are no active gRPC streams; will be ignored if "+
			"operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationSelfMonitoringEnabled,
		"operator-configuration-self-monitoring-enabled",
		true,
		"Whether to set selfMonitoring.enabled on the operator configuration resource; will be ignored if "+
			"operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled,
		"operator-configuration-kubernetes-infrastructure-metrics-collection-enabled",
		true,
		"The value for kubernetesInfrastructureMetricsCollection.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationCollectPodLabelsAndAnnotationsEnabled,
		"operator-configuration-collect-pod-labels-and-annotations-enabled",
		true,
		"The value for collectPodLabelsAndAnnotations.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationCollectNamespaceLabelsAndAnnotationsEnabled,
		"operator-configuration-collect-namespace-labels-and-annotations-enabled",
		true,
		"The value for collectNamespaceLabelsAndAnnotations.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationPrometheusCrdSupportEnabled,
		"operator-configuration-prometheus-crd-support-enabled",
		false,
		"The value for prometheusCrdSupport.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationProfilingEnabled,
		"operator-configuration-profiling-enabled",
		false,
		"The value for profiling.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.telemetryCollectionEnabled,
		"dash0-telemetry-collection-enabled",
		true,
		"The value for telemetryCollection.enabled on the operator configuration resource.",
	)
	flag.StringVar(
		&cliArgs.logLevel,
		"dash0-log-level",
		"info",
		"The log level for the operator manager (debug, info, warn, error). Ignored when development mode is active.",
	)
	flag.BoolVar(
		&cliArgs.featureIntelligentEdgeEnabled,
		"dash0-feature-intelligent-edge-enabled",
		false,
		"Enable Intelligent Edge features (sampling, RED metrics, barker proxy).",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationClusterName,
		"operator-configuration-cluster-name",
		"",
		"The clusterName to set on the operator configuration resource; will be ignored if"+
			"operator-configuration-endpoint is not set. If set, the value will be added as the resource attribute "+
			"k8s.cluster.name to all telemetry.",
	)
	flag.BoolVar(
		&cliArgs.operatorConfigurationAutoMonitorNamespacesEnabled,
		"operator-configuration-auto-monitor-namespaces-enabled",
		false,
		"The value for autoMonitorNamespaces.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationAutoMonitorNamespacesLabelSelector,
		"operator-configuration-auto-monitor-namespaces-label-selector",
		"",
		"The value for autoMonitorNamespaces.labelSelector on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.",
	)
	flag.StringVar(
		&cliArgs.operatorConfigurationInstrumentationDelivery,
		"operator-configuration-instrumentation-delivery",
		"",
		"The value for spec.instrumentWorkloads.instrumentationDelivery on the operator configuration resource. "+
			"Allowed values are \"auto\", \"image-volume\" and \"init-container\". Will be ignored if "+
			"operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&cliArgs.forceUseOpenTelemetryCollectorServiceUrl,
		"dash0-force-use-otel-collector-service-url",
		false,
		"When modifying workloads, always use the service URL of the OpenTelemetry collector DaemonSet, instead of "+
			"routing telemetry from workloads via node-local traffic to the node IP/host port of the collector pod.",
	)
	flag.BoolVar(
		&cliArgs.isGkeAutopilot,
		"dash0-gke-autopilot",
		false,
		"Whether the operator is running on GKE Autopilot.",
	)
	flag.BoolVar(
		&cliArgs.disableOpenTelemetryCollectorHostPorts,
		"dash0-disable-otel-collector-host-ports",
		false,
		"Disable the host ports of the OpenTelemetry collector pods managed by the operator. Implies "+
			"--dash0-force-use-otel-collector-service-url.",
	)
	cliArgs.instrumentationDelays = &util.DelayConfig{}
	flag.Uint64Var(
		&cliArgs.instrumentationDelays.AfterEachWorkloadMillis,
		"instrumentation-delay-after-each-workload-millis",
		0,
		"An optional delay to stagger access to the Kubernetes API server to instrument existing workloads at "+
			"operator startup or when enabling instrumentation for a new namespace via Dash0Monitoring resource. This "+
			"delay will be applied after each individual workload.",
	)
	flag.Uint64Var(
		&cliArgs.instrumentationDelays.AfterEachNamespaceMillis,
		"instrumentation-delay-after-each-namespace-millis",
		0,
		"An optional delay to stagger access to the Kubernetes API server to instrument (or update the "+
			"instrumentation of) existing workloads at operator startup. This delay will be applied each time all "+
			"workloads in a namespace have been processed, before starting with the next namespace.",
	)
	flag.StringVar(
		&cliArgs.metricsAddr,
		"metrics-bind-address",
		":8080",
		"The address the metric endpoint binds to.",
	)
	flag.StringVar(
		&cliArgs.probeAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.",
	)
	flag.BoolVar(
		&cliArgs.enableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for operator manager. "+
			"Enabling this will ensure there is only one active operator manager.",
	)
	flag.BoolVar(
		&cliArgs.secureMetrics,
		"metrics-secure",
		false,
		"If set, the metrics endpoint is served securely.",
	)
	flag.BoolVar(
		&cliArgs.enableHTTP2,
		"enable-http2",
		false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers.",
	)
	return cliArgs
}

func parseCommandLineOptions(cliArgs *commandLineArguments, developmentMode bool) crzap.Options {
	var opts crzap.Options
	if developmentMode {
		opts = crzap.Options{
			Development: true,
		}
	} else {
		opts = crzap.Options{
			Development: false,
		}
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if !developmentMode {
		var level zapcore.Level
		if err := level.UnmarshalText([]byte(strings.ToLower(cliArgs.logLevel))); err != nil {
			fmt.Fprintf(os.Stderr, "invalid --dash0-log-level value %q, defaulting to info: %v\n", cliArgs.logLevel, err)
			level = zapcore.InfoLevel
		}
		lvl := zap.NewAtomicLevelAt(level)
		opts.Level = &lvl
	}

	if cliArgs.disableOpenTelemetryCollectorHostPorts {
		// disableOpenTelemetryCollectorHostPorts implies forceUseOpenTelemetryCollectorServiceUrl
		cliArgs.forceUseOpenTelemetryCollectorServiceUrl = true
	}
	return opts
}

func setUpLogging(crZapOpts crzap.Opts, developmentMode bool) *zaputil.DelegatingZapCoreWrapper {
	o := zaputil.ConvertOptions([]crzap.Opts{crZapOpts})

	// this basically mimics New<type>Config, but with a custom sink
	sink := zapcore.AddSync(o.DestWriter)

	o.ZapOpts = append(o.ZapOpts, zap.ErrorOutput(sink))
	defaultZapCore := zapcore.NewCore(&crzap.KubeAwareEncoder{Encoder: o.Encoder, Verbose: o.Development}, sink, o.Level)

	delegatingZapCoreWrapper := zaputil.NewDelegatingZapCoreWrapper()
	if developmentMode {
		delegatingZapCoreWrapper.RootDelegatingZapCore.SetBufferingLevel(zapcore.DebugLevel)
	}

	// Send log records to stdout (defaultZapCore) and also to the OTel log SDK (delegatingZapCore). Additional plot
	// twist: The OTel logger will only be initialized later, potentially after the operator configuration has been
	// reconciled. The delegatingZapCore will buffer all messages logged at startup up to the point when the OTel logger
	// is actually initialized, then re-spool them to the OTel SDK logger.
	teeCore := zapcore.NewTee(
		defaultZapCore,
		delegatingZapCoreWrapper.RootDelegatingZapCore,
	)
	crZapRawLogger := zaputil.NewRawFromCore(o, teeCore)
	zapLogger := zapr.NewLogger(crZapRawLogger)

	// Set the created tee logger as the logger for the controller-runtime package.
	ctrl.SetLogger(zapLogger)

	setupLog = logd.NewLogger(ctrl.Log.WithName("setup"))

	return delegatingZapCoreWrapper
}

func readEnvironmentVariables(logger logd.Logger) error {
	operatorNamespace, isSet := os.LookupEnv(operatorNamespaceEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, operatorNamespaceEnvVarName)
	}

	deploymentName, isSet := os.LookupEnv(deploymentNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, deploymentNameEnvVarName)
	}

	webhookServiceName, isSet := os.LookupEnv(webhookServiceNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, webhookServiceNameEnvVarName)
	}

	webhookServicePort, err := readWebhookServicePortFromEnvironmentVariable()
	if err != nil {
		return err
	}

	autoPatchPersesDashboardConversionWebhook :=
		readOptionalBoolFromEnvironmentVariable(persesDashboardAutoPatchConversionWebhookEnvVarName, true)

	iacPeriodicRetryEnabled := readOptionalBoolFromEnvironmentVariable(periodicRetryEnabledEnvVarName, true)
	iacPeriodicRetryInterval :=
		readOptionalDurationFromEnvironmentVariable(periodicRetryIntervalEnvVarName, defaultPeriodicRetryInterval)

	oTelCollectorNamePrefix, isSet := os.LookupEnv(oTelCollectorNamePrefixEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, oTelCollectorNamePrefixEnvVarName)
	}

	targetAllocatorNamePrefix, isSet := os.LookupEnv(targetAllocatorNamePrefixEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, targetAllocatorNamePrefixEnvVarName)
	}

	operatorImage, isSet := os.LookupEnv(operatorImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, operatorImageEnvVarName)
	}

	instrumentationImage, isSet := os.LookupEnv(instrumentationImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, instrumentationImageEnvVarName)
	}
	instrumentationImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(instrumentationImagePullPolicyEnvVarName)

	collectorImage, isSet := os.LookupEnv(collectorImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, collectorImageEnvVarName)
	}
	collectorImagePullPolicy := readOptionalPullPolicyFromEnvironmentVariable(collectorImageImagePullPolicyEnvVarName)

	targetAllocatorImage, isSet := os.LookupEnv(targetAllocatorImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, targetAllocatorImageEnvVarName)
	}
	targetAllocatorImagePullPolicy := readOptionalPullPolicyFromEnvironmentVariable(targetAllocatorImageImagePullPolicyEnvVarName)

	configurationReloaderImage, isSet := os.LookupEnv(configurationReloaderImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, configurationReloaderImageEnvVarName)
	}
	configurationReloaderImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(configurationReloaderImagePullPolicyEnvVarName)

	filelogOffsetSyncImage, isSet := os.LookupEnv(filelogOffsetSyncImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, filelogOffsetSyncImageEnvVarName)
	}
	filelogOffsetSyncImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(filelogOffsetSyncImagePullPolicyEnvVarName)

	filelogOffsetVolumeOwnershipImage, isSet := os.LookupEnv(filelogOffsetVolumeOwnershipImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, filelogOffsetVolumeOwnershipImageEnvVarName)
	}
	filelogOffsetVolumeOwnershipImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(filelogOffsetVolumeOwnershipImagePullPolicyEnvVarName)

	barkerImage, _ := os.LookupEnv(barkerImageEnvVarName)
	barkerImagePullPolicy := readOptionalPullPolicyFromEnvironmentVariable(barkerImagePullPolicyEnvVarName)

	agent0ConnectorImage, _ := os.LookupEnv(agent0ConnectorImageEnvVarName)
	agent0ConnectorImagePullPolicy := readOptionalPullPolicyFromEnvironmentVariable(agent0ConnectorImagePullPolicyEnvVarName)
	agent0ConnectorEnabled := readOptionalBoolFromEnvironmentVariable(agent0ConnectorEnabledEnvVarName, false)
	agent0ConnectorServerAddress, _ := os.LookupEnv(agent0ConnectorServerAddressEnvVarName)
	agent0ConnectorInsecureRaw, isSet := os.LookupEnv(agent0ConnectorInsecureEnvVarName)
	agent0ConnectorInsecure := isSet && strings.ToLower(agent0ConnectorInsecureRaw) == envVarValueTrue
	agent0ConnectorToken, _ := os.LookupEnv(agent0ConnectorTokenEnvVarName)
	agent0ConnectorSecretRefName, _ := os.LookupEnv(agent0ConnectorSecretRefNameEnvVarName)
	agent0ConnectorSecretRefKey, _ := os.LookupEnv(agent0ConnectorSecretRefKeyEnvVarName)

	nodeIp, isSet := os.LookupEnv(k8sNodeIpEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sNodeIpEnvVarName)
	}
	nodeName, isSet := os.LookupEnv(k8sNodeNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sNodeNameEnvVarName)
	}
	podIp, isSet := os.LookupEnv(k8sPodIpEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sPodIpEnvVarName)
	}

	instrumentationDebugRaw, isSet := os.LookupEnv(instrumentationDebugEnvVarName)
	instrumentationDebug := isSet && strings.ToLower(instrumentationDebugRaw) == envVarValueTrue
	enablePythonAutoInstrumentationRaw, isSet := os.LookupEnv(enablePythonAutoInstrumentationEnvVarName)
	enablePythonAutoInstrumentation := isSet && strings.ToLower(enablePythonAutoInstrumentationRaw) == envVarValueTrue

	debugVerbosityDetailedRaw, isSet := os.LookupEnv(debugVerbosityDetailedEnvVarName)
	debugVerbosityDetailed := isSet && strings.ToLower(debugVerbosityDetailedRaw) == envVarValueTrue

	disableCollectorResourceWatchesRaw, isSet := os.LookupEnv(disableCollectorResourceWatchesEnvVarName)
	disableCollectorResourceWatches := isSet && strings.ToLower(disableCollectorResourceWatchesRaw) == envVarValueTrue

	var sendBatchSize *uint32
	sendBatchSizeRaw, isSet := os.LookupEnv(sendBatchSizeEnvVarName)
	if isSet {
		converted, err := strconv.Atoi(sendBatchSizeRaw)
		if err != nil {
			logger.ErrorAsWarn(err, fmt.Sprintf("Ignoring invalid value for %s: %s", sendBatchSizeEnvVarName, sendBatchSizeRaw))
		} else {
			sendBatchSize = ptr.To(uint32(converted))
		}
	}

	var sendBatchMaxSize *uint32
	sendBatchMaxSizeRaw, isSet := os.LookupEnv(sendBatchMaxSizeEnvVarName)
	if isSet {
		converted, err := strconv.Atoi(sendBatchMaxSizeRaw)
		if err != nil {
			logger.ErrorAsWarn(err, fmt.Sprintf("Ignoring invalid value for %s: %s", sendBatchMaxSizeEnvVarName, sendBatchMaxSizeRaw))
		} else {
			sendBatchMaxSize = ptr.To(uint32(converted))
		}
	}

	k8sAttributesDisableReplicasetInformerRaw, isSet := os.LookupEnv(k8sAttributesDisableReplicasetInformerEnvVarName)
	k8sAttributesDisableReplicasetInformer :=
		isSet && strings.ToLower(k8sAttributesDisableReplicasetInformerRaw) == envVarValueTrue

	k8sAttributesWaitForMetadataRaw, isSet := os.LookupEnv(k8sAttributesWaitForMetadataEnvVarName)
	k8sAttributesWaitForMetadata := isSet && strings.ToLower(k8sAttributesWaitForMetadataRaw) == envVarValueTrue
	k8sAttributesWaitForMetadataTimeout, _ := os.LookupEnv(k8sAttributesWaitForMetadataTimeoutEnvVarName)

	enablePprofExtensionRaw, isSet := os.LookupEnv(enablePprofExtensionEnvVarName)
	enablePprofExtension := isSet && strings.ToLower(enablePprofExtensionRaw) == envVarValueTrue

	compressConfigMapsRaw, isSet := os.LookupEnv(compressConfigMapsEnvVarName)
	compressConfigMaps := isSet && strings.ToLower(compressConfigMapsRaw) == envVarValueTrue

	envVars = environmentVariables{
		operatorNamespace:                           operatorNamespace,
		deploymentName:                              deploymentName,
		webhookServiceName:                          webhookServiceName,
		webhookServicePort:                          webhookServicePort,
		iacPeriodicRetryEnabled:                     iacPeriodicRetryEnabled,
		iacPeriodicRetryInterval:                    iacPeriodicRetryInterval,
		autoPatchPersesDashboardConversionWebhook:   autoPatchPersesDashboardConversionWebhook,
		oTelCollectorNamePrefix:                     oTelCollectorNamePrefix,
		targetAllocatorNamePrefix:                   targetAllocatorNamePrefix,
		operatorImage:                               operatorImage,
		instrumentationImage:                        instrumentationImage,
		instrumentationImagePullPolicy:              instrumentationImagePullPolicy,
		collectorImage:                              collectorImage,
		collectorImagePullPolicy:                    collectorImagePullPolicy,
		targetAllocatorImage:                        targetAllocatorImage,
		targetAllocatorImagePullPolicy:              targetAllocatorImagePullPolicy,
		configurationReloaderImage:                  configurationReloaderImage,
		configurationReloaderImagePullPolicy:        configurationReloaderImagePullPolicy,
		filelogOffsetSyncImage:                      filelogOffsetSyncImage,
		filelogOffsetSyncImagePullPolicy:            filelogOffsetSyncImagePullPolicy,
		filelogOffsetVolumeOwnershipImage:           filelogOffsetVolumeOwnershipImage,
		filelogOffsetVolumeOwnershipImagePullPolicy: filelogOffsetVolumeOwnershipImagePullPolicy,
		barkerImage:                                 barkerImage,
		barkerImagePullPolicy:                       barkerImagePullPolicy,
		agent0ConnectorImage:                        agent0ConnectorImage,
		agent0ConnectorImagePullPolicy:              agent0ConnectorImagePullPolicy,
		agent0ConnectorEnabled:                      agent0ConnectorEnabled,
		agent0ConnectorServerAddress:                agent0ConnectorServerAddress,
		agent0ConnectorInsecure:                     agent0ConnectorInsecure,
		agent0ConnectorToken:                        agent0ConnectorToken,
		agent0ConnectorSecretRefName:                agent0ConnectorSecretRefName,
		agent0ConnectorSecretRefKey:                 agent0ConnectorSecretRefKey,
		nodeIp:                                      nodeIp,
		nodeName:                                    nodeName,
		podIp:                                       podIp,
		sendBatchSize:                               sendBatchSize,
		sendBatchMaxSize:                            sendBatchMaxSize,
		k8sAttributesDisableReplicasetInformer:      k8sAttributesDisableReplicasetInformer,
		k8sAttributesWaitForMetadata:                k8sAttributesWaitForMetadata,
		k8sAttributesWaitForMetadataTimeout:         k8sAttributesWaitForMetadataTimeout,
		instrumentationDebug:                        instrumentationDebug,
		enablePythonAutoInstrumentation:             enablePythonAutoInstrumentation,
		debugVerbosityDetailed:                      debugVerbosityDetailed,
		disableCollectorResourceWatches:             disableCollectorResourceWatches,
		enablePprofExtension:                        enablePprofExtension,
		compressConfigMaps:                          compressConfigMaps,
	}

	return nil
}

// readExtraConfigMap reads the config map content, which is basically a container for structured configuration data
// that would be cumbersome to pass in as a command line argument.
func readExtraConfigMap() error {
	var err error
	extraConfig, err = util.ReadExtraConfigMap()
	if err != nil {
		return err
	}
	return nil
}

func readWebhookServicePortFromEnvironmentVariable() (int32, error) {
	raw, isSet := os.LookupEnv(webhookServicePortEnvVarName)
	if !isSet {
		return 0, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, webhookServicePortEnvVarName)
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %s=%q as integer: %w", webhookServicePortEnvVarName, raw, err)
	}
	return int32(parsed), nil
}

func readOptionalBoolFromEnvironmentVariable(envVarName string, defaultValue bool) bool {
	raw, isSet := os.LookupEnv(envVarName)
	if !isSet {
		return defaultValue
	}
	return strings.ToLower(raw) == envVarValueTrue
}

// readOptionalDurationFromEnvironmentVariable reads a Go duration string (as understood by time.ParseDuration, e.g.
// "30s", "10m", "2h") from the given environment variable. If the variable is unset, cannot be parsed, or is not a
// positive duration, the provided default value is used (and a warning is logged for unparseable/non-positive values).
func readOptionalDurationFromEnvironmentVariable(envVarName string, defaultValue time.Duration) time.Duration {
	raw, isSet := os.LookupEnv(envVarName)
	if !isSet || raw == "" {
		return defaultValue
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		setupLog.Warn(
			fmt.Sprintf(
				"Ignoring invalid value for %s (%q): %v. Using the default of %s instead.",
				envVarName, raw, err, defaultValue,
			),
		)
		return defaultValue
	}
	if parsed <= 0 {
		setupLog.Warn(
			fmt.Sprintf(
				"Ignoring non-positive value for %s (%q). Using the default of %s instead.",
				envVarName, raw, defaultValue,
			),
		)
		return defaultValue
	}
	return parsed
}

// allOwnedIacResourceSynchronizationControllers gathers the reconcilers of the operator-owned resource types into a
// slice of OwnedIacResourceSynchronizationController for the periodic synchronization retry runnable. The sampling rule
// reconciler is optional (it is only created when the corresponding feature is enabled), so it is only included when
// non-nil.
func allOwnedIacResourceSynchronizationControllers(
	notificationChannelReconciler *controller.NotificationChannelReconciler,
	signalToMetricsReconciler *controller.SignalToMetricsReconciler,
	spamFilterReconciler *controller.SpamFilterReconciler,
	syntheticCheckReconciler *controller.SyntheticCheckReconciler,
	viewReconciler *controller.ViewReconciler,
	samplingRuleReconciler *controller.SamplingRuleReconciler,
) []controller.OwnedIacResourceSynchronizationController {
	controllers := []controller.OwnedIacResourceSynchronizationController{
		notificationChannelReconciler,
		signalToMetricsReconciler,
		spamFilterReconciler,
		syntheticCheckReconciler,
		viewReconciler,
	}
	if samplingRuleReconciler != nil {
		controllers = append(controllers, samplingRuleReconciler)
	}
	return controllers
}

// setupSynchronizationRetryRunnable adds the periodic synchronization retry runnable to the manager, unless it has been
// disabled via operator.iac.periodicRetry.enabled=false in the Helm values.
func setupSynchronizationRetryRunnable(
	mgr ctrl.Manager,
	k8sClient client.Client,
	persesDashboardCrdReconciler *controller.PersesDashboardCrdReconciler,
	prometheusRuleCrdReconciler *controller.PrometheusRuleCrdReconciler,
	ownedIacResourceSynchronizationControllers []controller.OwnedIacResourceSynchronizationController,
) error {
	if !envVars.iacPeriodicRetryEnabled {
		setupLog.Info("The periodic synchronization retry job is disabled (operator.iac.periodicRetry.enabled=false).")
		return nil
	}
	setupLog.Info(
		fmt.Sprintf(
			"The periodic synchronization retry job is enabled and will run every %s.",
			envVars.iacPeriodicRetryInterval,
		),
	)
	if err := mgr.Add(controller.NewSynchronizationRetryRunnable(
		k8sClient,
		persesDashboardCrdReconciler,
		prometheusRuleCrdReconciler,
		ownedIacResourceSynchronizationControllers,
		envVars.iacPeriodicRetryInterval,
		setupLog,
	)); err != nil {
		return fmt.Errorf("unable to add the synchronization retry runnable to the manager: %w", err)
	}
	return nil
}

func readOptionalPullPolicyFromEnvironmentVariable(envVarName string) corev1.PullPolicy {
	pullPolicyRaw := os.Getenv(envVarName)
	if pullPolicyRaw != "" {
		if pullPolicyRaw == string(corev1.PullAlways) ||
			pullPolicyRaw == string(corev1.PullIfNotPresent) ||
			pullPolicyRaw == string(corev1.PullNever) {
			return corev1.PullPolicy(pullPolicyRaw)
		} else {
			setupLog.Warn(
				fmt.Sprintf(
					"Ignoring unknown pull policy setting (%s): %s.", envVarName, pullPolicyRaw,
				),
			)
		}
	}
	return ""
}

func initStartupTasksK8sClient(logger logd.Logger) error {
	cfg := ctrl.GetConfigOrDie()
	var err error
	if startupTasksK8sClient, err = client.New(
		cfg, client.Options{
			Scheme: runtimeScheme,
		},
	); err != nil {
		logger.Error(err, "failed to create Kubernetes API client for startup tasks")
		return err
	}
	return nil
}

func detectDocker(
	ctx context.Context,
	k8sClient client.Client,
	logger logd.Logger,
) {
	nodeList := &corev1.NodeList{}
	err := k8sClient.List(ctx, nodeList, &client.ListOptions{Limit: 1})
	if err != nil {
		logger.Error(err, "cannot list nodes for container runtime detection")
		// assume it's not Docker
		return
	}
	for _, node := range nodeList.Items {
		if strings.Contains(node.Status.NodeInfo.ContainerRuntimeVersion, "docker://") {
			isDocker = true
		}
	}
}

func startOperatorManager(
	ctx context.Context,
	cliArgs *commandLineArguments,
	tlsOpts []func(*tls.Config),
	webhookServer k8swebhook.Server,
	operatorConfigurationValues *OperatorConfigurationValues,
	delegatingZapCoreWrapper *zaputil.DelegatingZapCoreWrapper,
	developmentMode bool,
) error {
	options := ctrl.Options{
		Scheme: runtimeScheme,
		Metrics: metricsserver.Options{
			BindAddress:   cliArgs.metricsAddr,
			SecureServing: cliArgs.secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: cliArgs.probeAddr,
		LeaderElection:         cliArgs.enableLeaderElection,
		LeaderElectionID:       "5ae7ac41.dash0.com",

		// We are deliberately not setting LeaderElectionReleaseOnCancel to true, since we cannot guarantee that the
		// operator manager will terminate immediately, we need to shut down a couple of internal components before
		// terminating (self monitoring OTel SDK shutdown etc.).
		LeaderElectionReleaseOnCancel: false,
	}

	if !cliArgs.telemetryCollectionEnabled {
		// If telemetry collection is disabled, we do not have the RBAC permissions to read secrects in arbitrary
		// namespaces. As a consequence, we need to instruct controller-runtime to only list/watch secrets in the operator
		// namespace. Otherwise, any k8sClient.Get call to fetch a secret (in particular, the Dash0 auth token secret for
		// self-monitoring) will make controller-runtime execute a list and watch for secrets in the entire cluster, just
		// to build its internal cache. This fails with
		// 'User ... cannot list resource "secrets" in API group "" at the cluster scope.'
		// if we do not have cluster-wide permissions for secrets.
		options.Cache = cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Secret{}: {
					Namespaces: map[string]cache.Config{
						envVars.operatorNamespace: {},
					},
				},
			},
		}
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		return fmt.Errorf("unable to create the manager: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create the clientset client")
	}

	kubernetesVersionInfo, kubernetesVersionDetected := cluster.DetectKubernetesVersion(clientset, setupLog)
	resolvedInstrumentationDelivery := cluster.ResolveInstrumentationDelivery(
		cliArgs.operatorConfigurationInstrumentationDelivery,
		kubernetesVersionInfo,
		kubernetesVersionDetected,
		operatorConfigurationIsManagedViaHelm(cliArgs),
		setupLog,
	)

	operatorConfigurationTokenRedacted := ""
	if cliArgs.operatorConfigurationToken != "" {
		operatorConfigurationTokenRedacted = "<redacted>"
	}
	setupLog.Info(
		"operator manager configuration:",

		"operator image",
		envVars.operatorImage,

		"init container image",
		envVars.instrumentationImage,
		"init container image pull policy override",
		envVars.instrumentationImagePullPolicy,

		"collector image",
		envVars.collectorImage,
		"collector image pull policy override",
		envVars.collectorImagePullPolicy,

		"target-allocator image",
		envVars.targetAllocatorImage,
		"target-allocator image pull policy override",
		envVars.targetAllocatorImagePullPolicy,

		"configuration reloader image",
		envVars.configurationReloaderImage,
		"configuration reloader image pull policy override",
		envVars.configurationReloaderImagePullPolicy,

		"barker image",
		envVars.barkerImage,
		"barker image pull policy override",
		envVars.barkerImagePullPolicy,

		"operator namespace",
		envVars.operatorNamespace,
		"operator manager deployment name",
		envVars.deploymentName,
		"otel collector name prefix",
		envVars.oTelCollectorNamePrefix,

		"operator configuration endpoint",
		cliArgs.operatorConfigurationEndpoint,
		"operator configuration api endpoint",
		cliArgs.operatorConfigurationApiEndpoint,
		"operator configuration dataset",
		cliArgs.operatorConfigurationDataset,
		"operator configuration token",
		operatorConfigurationTokenRedacted,
		"operator configuration secret ref name",
		cliArgs.operatorConfigurationSecretRefName,
		"operator configuration secret ref key",
		cliArgs.operatorConfigurationSecretRefKey,
		"operator configuration self-monitoring enabled",
		cliArgs.operatorConfigurationSelfMonitoringEnabled,
		"operator configuration kubernetes infrastructure metrics collection enabled",
		cliArgs.operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled,
		"operator configuration collect pod labels and annotations enabled",
		cliArgs.operatorConfigurationCollectPodLabelsAndAnnotationsEnabled,
		"operator configuration collect namespace labels and annotations enabled",
		cliArgs.operatorConfigurationCollectNamespaceLabelsAndAnnotationsEnabled,
		"operator configuration prometheus crd support enabled",
		cliArgs.operatorConfigurationPrometheusCrdSupportEnabled,
		"operator configuration profiling enabled",
		cliArgs.operatorConfigurationProfilingEnabled,
		"operator configuration cluster name",
		cliArgs.operatorConfigurationClusterName,
		"auto-monitor namespaces enabled",
		cliArgs.operatorConfigurationAutoMonitorNamespacesEnabled,
		"auto-monitor namespaces label selector",
		cliArgs.operatorConfigurationAutoMonitorNamespacesLabelSelector,

		"telemetry collection enabled",
		cliArgs.telemetryCollectionEnabled,
		"feature intelligent edge enabled",
		cliArgs.featureIntelligentEdgeEnabled,
		"force-use OpenTelemetry collector service URL",
		cliArgs.forceUseOpenTelemetryCollectorServiceUrl,
		"disable OpenTelemetry collector host ports",
		cliArgs.disableOpenTelemetryCollectorHostPorts,
		"is GKE Autopilot",
		cliArgs.isGkeAutopilot,

		"metrics bind address",
		cliArgs.metricsAddr,
		"health probe bind address",
		cliArgs.probeAddr,
		"leader election enabled",
		cliArgs.enableLeaderElection,
		"metrics secure",
		cliArgs.secureMetrics,
		"http2 enabled",
		cliArgs.enableHTTP2,

		"extra config",
		extraConfig,

		"requested instrumentation delivery",
		cliArgs.operatorConfigurationInstrumentationDelivery,
		"resolved instrumentation delivery",
		resolvedInstrumentationDelivery,
		"instrumentation delays",
		cliArgs.instrumentationDelays,
		"development mode",
		developmentMode,
		"verbosity detailed",
		envVars.debugVerbosityDetailed,
		"enable pprof extension",
		envVars.enablePprofExtension,
		"compress config maps",
		envVars.compressConfigMaps,
		"instrumentation debug",
		envVars.instrumentationDebug,
		"Python auto-instrumentation enabled",
		envVars.enablePythonAutoInstrumentation,
		"watch collector resources",
		!envVars.disableCollectorResourceWatches,
		"Kubernetes version",
		kubernetesVersionInfo.VersionString,
		"Kubernetes version detected",
		kubernetesVersionDetected,
	)

	err = startDash0Controllers(
		ctx,
		mgr,
		clientset,
		cliArgs,
		operatorConfigurationValues,
		kubernetesVersionInfo,
		kubernetesVersionDetected,
		resolvedInstrumentationDelivery,
		delegatingZapCoreWrapper,
		developmentMode,
	)
	if err != nil {
		return err
	}

	defer func() {
		if thirdPartyResourceSynchronizationQueue != nil {
			controller.StopProcessingThirdPartySynchronizationQueue(thirdPartyResourceSynchronizationQueue, setupLog)
		}
	}()

	if err = mgr.Add(leaderElectionAwareRunnable); err != nil {
		return fmt.Errorf("unable to add the leader election aware runnable: %w", err)
	}

	setupLog.Info("starting manager (waiting for leader election)")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("unable to set up the signal handler: %w", err)
	}
	// ^mgr.Start(...) blocks. It only returns when the manager is terminating.

	extraConfigMapWatcher.CloseWatch()

	if oTelSdkStarter != nil {
		oTelSdkStarter.ShutDown(ctx, setupLog)
	}

	return nil
}

func startDash0Controllers(
	ctx context.Context,
	mgr manager.Manager,
	clientset *kubernetes.Clientset,
	cliArgs *commandLineArguments,
	operatorConfigurationValues *OperatorConfigurationValues,
	kubernetesVersionInfo cluster.KubernetesVersionInfo,
	kubernetesVersionDetected bool,
	instrumentationDelivery cluster.ResolvedInstrumentationDelivery,
	delegatingZapCoreWrapper *zaputil.DelegatingZapCoreWrapper,
	developmentMode bool,
) error {
	images := util.Images{
		OperatorImage:                               envVars.operatorImage,
		InitContainerImage:                          envVars.instrumentationImage,
		InitContainerImagePullPolicy:                envVars.instrumentationImagePullPolicy,
		CollectorImage:                              envVars.collectorImage,
		CollectorImagePullPolicy:                    envVars.collectorImagePullPolicy,
		TargetAllocatorImage:                        envVars.targetAllocatorImage,
		TargetAllocatorPullPolicy:                   envVars.targetAllocatorImagePullPolicy,
		ConfigurationReloaderImage:                  envVars.configurationReloaderImage,
		ConfigurationReloaderImagePullPolicy:        envVars.configurationReloaderImagePullPolicy,
		FilelogOffsetSyncImage:                      envVars.filelogOffsetSyncImage,
		FilelogOffsetSyncImagePullPolicy:            envVars.filelogOffsetSyncImagePullPolicy,
		FilelogOffsetVolumeOwnershipImage:           envVars.filelogOffsetVolumeOwnershipImage,
		FilelogOffsetVolumeOwnershipImagePullPolicy: envVars.filelogOffsetVolumeOwnershipImagePullPolicy,
		BarkerImage:                                 envVars.barkerImage,
		BarkerImagePullPolicy:                       envVars.barkerImagePullPolicy,
		Agent0ConnectorImage:                        envVars.agent0ConnectorImage,
		Agent0ConnectorImagePullPolicy:              envVars.agent0ConnectorImagePullPolicy,
	}

	httpClient := util.WithUserAgent(
		dash0apiclient.NewTransport(
			dash0apiclient.WithTransportMaxRetries(2),
			dash0apiclient.WithTransportRetryWaitMin(1*time.Second),
			dash0apiclient.WithTransportRetryWaitMax(3*time.Second),
		).HTTPClient(),
		images.GetOperatorVersion(),
	)

	isIPv6Cluster := strings.Count(envVars.podIp, ":") >= 2
	possibleCollectorUrls := collectors.RenderCollectorBaseUrls(
		envVars.oTelCollectorNamePrefix,
		envVars.operatorNamespace,
	)
	oTelCollectorBaseUrl := collectors.SelectCollectorBaseUrl(
		possibleCollectorUrls,
		cliArgs.forceUseOpenTelemetryCollectorServiceUrl,
		isIPv6Cluster,
	)

	readyCheckExecuter, err := registerHealthAndReadyChecks(ctx, mgr)
	if err != nil {
		return err
	}

	operatorConfigurationResource := createOrUpdateAutoOperatorConfigurationResource(
		ctx,
		readyCheckExecuter,
		operatorConfigurationValues,
		setupLog,
	)

	k8sClient := mgr.GetClient()

	if err = mgr.Add(NewLogConfigurationResourcesRunnable(k8sClient)); err != nil {
		return fmt.Errorf("unable to add log-operator-configuration task: %w", err)
	}

	clusterInstrumentationConfig := util.NewClusterInstrumentationConfig(
		images,
		possibleCollectorUrls,
		oTelCollectorBaseUrl,
		extraConfig,
		instrumentationDelivery,
		cliArgs.instrumentationDelays,
		envVars.instrumentationDebug,
		envVars.enablePythonAutoInstrumentation,
	)
	clusterInstrumentationConfig.SetKubernetesVersion(kubernetesVersionInfo, kubernetesVersionDetected)
	clusterUid, err := cluster.ReadPseudoClusterUidOrFail(ctx, startupTasksK8sClient, setupLog)
	if err != nil {
		return err
	}

	var instrumenter *instrumentation.Instrumenter
	var collectorManager *collectors.CollectorManager
	var targetallocatorManager *targetallocator.TargetAllocatorManager

	if cliArgs.telemetryCollectionEnabled {
		startupInstrumenter := instrumentation.NewInstrumenter(
			// The k8s client will be added later, in internal/startup/instrument_at_startup.go#Start.
			nil,
			clientset,
			mgr.GetEventRecorder("dash0-startup-tasks"),
			clusterInstrumentationConfig,
		)
		// For consistency, we update the extra config map in the startupInstrumenter handler as well if it changes. Since
		// this instrumenter only runs once at startup, this has no effect whatsoever.
		extraConfigMapWatcher.AddClient(startupInstrumenter)
		// register the instrument-at-startup task to run once this operator manager becomes leader
		if err = mgr.Add(NewInstrumentAtStartupRunnable(mgr, startupInstrumenter)); err != nil {
			return fmt.Errorf("unable to add instrument-at-startup task: %w", err)
		}

		instrumenter = instrumentation.NewInstrumenter(
			k8sClient,
			clientset,
			mgr.GetEventRecorder("dash0-monitoring-controller"),
			clusterInstrumentationConfig,
		)
		// For consistency, we update the extra config map in the instrumenter handler as well. However, even if
		// instrumentation related settings have been changed (e.g. operator.initContainerResources), we will not
		// re-instrument existing workloads to update the settings. It would make no sense, since the init container
		// only runs once at pod startup of the workload, so changing resource settings for the init container afterwards
		// is useless.
		extraConfigMapWatcher.AddClient(instrumenter)

		collectorConfig := util.CollectorConfig{
			Images:                                 images,
			OperatorNamespace:                      envVars.operatorNamespace,
			OTelCollectorNamePrefix:                envVars.oTelCollectorNamePrefix,
			TargetAllocatorNamePrefix:              envVars.targetAllocatorNamePrefix,
			Agent0ConnectorEnabled:                 envVars.agent0ConnectorEnabled,
			SendBatchSize:                          envVars.sendBatchSize,
			SendBatchMaxSize:                       envVars.sendBatchMaxSize,
			K8sAttributesDisableReplicasetInformer: envVars.k8sAttributesDisableReplicasetInformer,
			K8sAttributesWaitForMetadata:           envVars.k8sAttributesWaitForMetadata,
			K8sAttributesWaitForMetadataTimeout:    envVars.k8sAttributesWaitForMetadataTimeout,
			NodeIp:                                 envVars.nodeIp,
			NodeName:                               envVars.nodeName,
			PseudoClusterUid:                       clusterUid,
			IsIPv6Cluster:                          isIPv6Cluster,
			IsDocker:                               isDocker,
			DisableHostPorts:                       cliArgs.disableOpenTelemetryCollectorHostPorts,
			IsGkeAutopilot:                         cliArgs.isGkeAutopilot,
			DevelopmentMode:                        developmentMode,
			DebugVerbosityDetailed:                 envVars.debugVerbosityDetailed,
			EnableProfExtension:                    envVars.enablePprofExtension,
			CompressConfigMap:                      envVars.compressConfigMaps,
		}
		oTelColResourceManager := otelcolresources.NewOTelColResourceManager(
			k8sClient,
			mgr.GetScheme(),
			operatorDeploymentSelfReference,
			collectorConfig,
		)
		collectorManager = collectors.NewCollectorManager(
			k8sClient,
			clientset,
			extraConfig,
			developmentMode,
			cliArgs.featureIntelligentEdgeEnabled,
			oTelColResourceManager,
		)
		// We update the extra config map in the collectorManager when the extra config map changes, and also trigger a
		// reconciliation of the collectors. This makes sure changed resource settings, filelog offset volumes or toleration
		// taints are applied more or less immediately. (Noticing the changed config map can take a minute or a bit more.)
		extraConfigMapWatcher.AddClient(collectorManager)

		if err := setupCollectorReconciler(mgr, k8sClient, collectorManager, envVars); err != nil {
			return err
		}

		targetAllocatorConfig := util.TargetAllocatorConfig{
			Images:                    images,
			OperatorNamespace:         envVars.operatorNamespace,
			TargetAllocatorNamePrefix: envVars.targetAllocatorNamePrefix,
			CollectorComponent:        otelcolresources.CollectorDaemonSetServiceComponent(),
			IsGkeAutopilot:            cliArgs.isGkeAutopilot,
		}
		targetallocatorResourceManager := taresources.NewTargetAllocatorResourceManager(
			k8sClient,
			mgr.GetScheme(),
			operatorDeploymentSelfReference,
			targetAllocatorConfig,
		)
		targetallocatorManager = targetallocator.NewTargetAllocatorManager(
			k8sClient, clientset, extraConfig, developmentMode, targetallocatorResourceManager,
		)
		// We update the extra config map in the targetallocatorManager when the extra config map changes, and also trigger a
		// reconciliation of the target-allocator.
		extraConfigMapWatcher.AddClient(targetallocatorManager)
		targetAllocatorReconciler := targetallocator.NewTargetAllocatorReconciler(
			k8sClient,
			targetallocatorManager,
			envVars.operatorNamespace,
			envVars.targetAllocatorNamePrefix,
		)
		if err := targetAllocatorReconciler.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to set up the target-allocator reconciler: %w", err)
		}
	}

	agent0ConnectorManager, err := setupAgent0ConnectorManager(
		mgr,
		k8sClient,
		envVars,
		images,
		operatorDeploymentSelfReference,
		clusterUid,
		developmentMode,
	)
	if err != nil {
		return err
	}

	var ieManager *intelligentedge.IntelligentEdgeManager
	if !cliArgs.featureIntelligentEdgeEnabled {
		setupLog.Info("Intelligent Edge features are disabled.")
	} else {
		setupLog.Info("Intelligent Edge features are enabled.")
		ieResourceManager := ieresources.NewIntelligentEdgeResourceManager(
			k8sClient,
			mgr.GetScheme(),
			operatorDeploymentSelfReference,
			envVars.operatorNamespace,
			envVars.oTelCollectorNamePrefix,
			envVars.barkerImage,
			envVars.barkerImagePullPolicy,
			images.GetOperatorVersion(),
		)
		ieManager = intelligentedge.NewIntelligentEdgeManager(
			k8sClient,
			ieResourceManager,
			extraConfig,
		)
		// Update the extra config in the intelligent edge manager when the extra config map changes, and also trigger a
		// reconciliation of the intelligent edge resources.
		extraConfigMapWatcher.AddClient(ieManager)
		ieReconciler := intelligentedge.NewIntelligentEdgeReconciler(
			k8sClient,
			ieManager,
			collectorManager,
		)
		if err := ieReconciler.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to set up the intelligent edge reconciler: %w", err)
		}
		setupLog.Info("The intelligent edge reconciler has been started.")
	} // closes else (IE enabled)

	syntheticCheckReconciler := controller.NewSyntheticCheckReconciler(
		k8sClient,
		clusterUid,
		leaderElectionAwareRunnable,
		httpClient,
	)
	if err := syntheticCheckReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the synthetic check reconciler: %w", err)
	}
	leaderElectionAwareRunnable.AddLeaderElectionClient(syntheticCheckReconciler)

	viewReconciler := controller.NewViewReconciler(
		k8sClient,
		clusterUid,
		leaderElectionAwareRunnable,
		httpClient,
	)
	if err := viewReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the view reconciler: %w", err)
	}
	leaderElectionAwareRunnable.AddLeaderElectionClient(viewReconciler)

	notificationChannelReconciler := controller.NewNotificationChannelReconciler(
		k8sClient,
		clusterUid,
		leaderElectionAwareRunnable,
		httpClient,
	)
	if err := notificationChannelReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the notification channel reconciler: %w", err)
	}
	leaderElectionAwareRunnable.AddLeaderElectionClient(notificationChannelReconciler)

	spamFilterReconciler := controller.NewSpamFilterReconciler(
		k8sClient,
		clusterUid,
		leaderElectionAwareRunnable,
		httpClient,
	)
	if err := spamFilterReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the spam filter reconciler: %w", err)
	}
	leaderElectionAwareRunnable.AddLeaderElectionClient(spamFilterReconciler)

	var samplingRuleReconciler *controller.SamplingRuleReconciler
	if cliArgs.featureIntelligentEdgeEnabled {
		samplingRuleReconciler = controller.NewSamplingRuleReconciler(
			k8sClient,
			clusterUid,
			leaderElectionAwareRunnable,
			httpClient,
		)
		if err := samplingRuleReconciler.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to set up the sampling rule reconciler: %w", err)
		}
		leaderElectionAwareRunnable.AddLeaderElectionClient(samplingRuleReconciler)
	}

	signalToMetricsReconciler := controller.NewSignalToMetricsReconciler(
		k8sClient,
		clusterUid,
		leaderElectionAwareRunnable,
		httpClient,
	)
	if err := signalToMetricsReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the signal-to-metrics reconciler: %w", err)
	}
	leaderElectionAwareRunnable.AddLeaderElectionClient(signalToMetricsReconciler)

	thirdPartyResourceSynchronizationQueue =
		workqueue.NewTypedWithConfig(
			workqueue.TypedQueueConfig[controller.ThirdPartyResourceSyncJob]{
				Name: "dash0-third-party-resource-synchronization-queue",
			},
		)

	persesDashboardCrdReconciler := controller.NewPersesDashboardCrdReconciler(
		k8sClient,
		thirdPartyResourceSynchronizationQueue,
		leaderElectionAwareRunnable,
		httpClient,
		controller.PersesDashboardConversionWebhookSettings{
			AutoPatchConversionWebhook: envVars.autoPatchPersesDashboardConversionWebhook,
			OperatorNamespace:          envVars.operatorNamespace,
			WebhookServiceName:         envVars.webhookServiceName,
			WebhookServicePort:         envVars.webhookServicePort,
		},
	)
	if err := persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, startupTasksK8sClient, setupLog); err != nil {
		return fmt.Errorf("unable to set up the Perses dashboard reconciler: %w", err)
	}

	prometheusRuleCrdReconciler := controller.NewPrometheusRuleCrdReconciler(
		k8sClient,
		thirdPartyResourceSynchronizationQueue,
		leaderElectionAwareRunnable,
		httpClient,
	)
	if err := prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, startupTasksK8sClient, setupLog); err != nil {
		return fmt.Errorf("unable to set up the Prometheus rule reconciler: %w", err)
	}

	controller.StartProcessingThirdPartySynchronizationQueue(thirdPartyResourceSynchronizationQueue, setupLog)

	// Periodically retry synchronizing iac resources (Prometheus rules, Perses dashboards, notification channels,
	// sampling rules, etc.) for which the previous synchronization attempt failed due to a transient error (server error
	// or rate limiting). This can be disabled or have its interval adjusted via operator.iac.periodicRetry in the Helm
	// values.
	if err = setupSynchronizationRetryRunnable(
		mgr,
		k8sClient,
		persesDashboardCrdReconciler,
		prometheusRuleCrdReconciler,
		allOwnedIacResourceSynchronizationControllers(
			notificationChannelReconciler,
			signalToMetricsReconciler,
			spamFilterReconciler,
			syntheticCheckReconciler,
			viewReconciler,
			samplingRuleReconciler,
		),
	); err != nil {
		return err
	}

	setupLog.Info("Creating the self-monitoring OTel SDK starter.")
	oTelSdkStarter = selfmonitoringapiaccess.NewOTelSdkStarter(delegatingZapCoreWrapper, common.NewDefaultExporterFactory())

	setupLog.Info("Creating the operator configuration resource reconciler.")
	apiClients := []controller.ApiClient{
		syntheticCheckReconciler,
		viewReconciler,
		notificationChannelReconciler,
		spamFilterReconciler,
		signalToMetricsReconciler,
		persesDashboardCrdReconciler,
		prometheusRuleCrdReconciler,
	}
	if samplingRuleReconciler != nil {
		apiClients = append(apiClients, samplingRuleReconciler)
	}
	operatorConfigurationReconciler := controller.NewOperatorConfigurationReconciler(
		k8sClient,
		clientset,
		apiClients,
		collectorManager,
		targetallocatorManager,
		agent0ConnectorManager,
		ieManager,
		clusterInstrumentationConfig,
		clusterUid,
		operatorDeploymentSelfReference.Namespace,
		operatorDeploymentSelfReference.UID,
		operatorDeploymentSelfReference.Name,
		oTelSdkStarter,
		images,
		envVars.operatorNamespace,
		developmentMode,
	)
	setupLog.Info("Starting the operator configuration resource reconciler.")
	if err := operatorConfigurationReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the operator configuration resource reconciler: %w", err)
	}
	setupLog.Info("The operator configuration resource reconciler has been started.")

	setupLog.Info("Creating the monitoring resource reconciler.")
	monitoringReconciler := controller.NewMonitoringReconciler(
		k8sClient,
		clientset,
		[]controller.NamespacedApiClient{
			syntheticCheckReconciler,
			viewReconciler,
			notificationChannelReconciler,
			spamFilterReconciler,
			signalToMetricsReconciler,
			persesDashboardCrdReconciler,
			prometheusRuleCrdReconciler,
		},
		instrumenter,
		collectorManager,
		targetallocatorManager,
		nil,
		envVars.operatorNamespace,
	)
	setupLog.Info("Starting the monitoring resource reconciler.")
	if err := monitoringReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the monitoring reconciler: %w", err)
	}
	setupLog.Info("The monitoring resource reconciler has been started.")

	var autoNamespaceMonitoringReconciler *controller.AutoNamespaceMonitoringReconciler
	if cliArgs.telemetryCollectionEnabled {
		setupLog.Info("Creating the auto-namespace-monitoring reconciler.")
		autoNamespaceMonitoringReconciler = controller.NewAutoNamespaceMonitoringReconciler(
			k8sClient,
			envVars.operatorNamespace,
		)
		setupLog.Info("Starting the auto-namespace-monitoring reconciler.")
		if err := autoNamespaceMonitoringReconciler.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to set up the auto-namespace-monitoring reconciler: %w", err)
		}
		setupLog.Info("The auto-namespace-monitoring reconciler has been started.")

		instrumentationWebhookHandler := webhooks.NewInstrumentationWebhookHandler(
			k8sClient,
			mgr.GetEventRecorder("dash0-instrumentation-webhook"),
			clusterInstrumentationConfig,
		)
		if err := instrumentationWebhookHandler.SetupWebhookWithManager(mgr); err != nil {
			return fmt.Errorf("unable to create the instrumentation webhook: %w", err)
		}
		// For consistency, we update the extra config map in the instrumentation webhook handler as well.
		// In case instrumentation-related settings have been changed (e.g., operator.initContainerResources), _new_
		// workloads will be instrumented with the updated settings. Existing and already instrumented workloads will not
		// be reinstrumented, and even if they are changed, the instrumentation webhook will not apply the new settings
		// due to the HasBeenInstrumentedSuccessfullyByThisVersion check.
		extraConfigMapWatcher.AddClient(instrumentationWebhookHandler)
	}

	if err := setupResourceWebhooks(
		mgr,
		k8sClient,
		envVars.operatorNamespace,
		cliArgs.telemetryCollectionEnabled,
		cliArgs.featureIntelligentEdgeEnabled,
	); err != nil {
		return err
	}

	selfMonitoringClients := []selfmonitoringapiaccess.SelfMonitoringMetricsClient{
		operatorConfigurationReconciler,
		monitoringReconciler,
		syntheticCheckReconciler,
		viewReconciler,
		spamFilterReconciler,
		signalToMetricsReconciler,
		persesDashboardCrdReconciler,
		prometheusRuleCrdReconciler,
	}
	if autoNamespaceMonitoringReconciler != nil {
		selfMonitoringClients = append(selfMonitoringClients, autoNamespaceMonitoringReconciler)
	}
	if samplingRuleReconciler != nil {
		selfMonitoringClients = append(selfMonitoringClients, samplingRuleReconciler)
	}
	oTelSdkStarter.WaitForOTelConfig(selfMonitoringClients)

	triggerSecretRefExchangeAndStartSelfMonitoringIfPossible(
		ctx,
		startupTasksK8sClient,
		envVars.operatorNamespace,
		oTelSdkStarter,
		operatorConfigurationResource,
		clusterUid,
		images.GetOperatorVersion(),
		developmentMode,
	)

	return nil
}

func registerHealthAndReadyChecks(ctx context.Context, mgr manager.Manager) (*ReadyCheckExecuter, error) {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("unable to set up the operator manager health check: %w", err)
	}
	readyCheckExecuter := NewReadyCheckExecuter(
		startupTasksK8sClient,
		envVars.operatorNamespace,
		envVars.webhookServiceName,
	)
	readyCheckExecuter.start(ctx, setupLog)
	if err := mgr.AddReadyzCheck("readyz", readyCheckExecuter.isReady); err != nil {
		return readyCheckExecuter, fmt.Errorf("unable to set up the operator manager ready check: %w", err)
	}
	return readyCheckExecuter, nil
}

func findDeploymentReference(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	deploymentName string,
	logger logd.Logger,
) (*appsv1.Deployment, error) {
	deploymentReference := &appsv1.Deployment{}
	fullyQualifiedName := fmt.Sprintf("%s/%s", operatorNamespace, deploymentName)
	if err := k8sClient.Get(
		ctx, client.ObjectKey{
			Namespace: operatorNamespace,
			Name:      deploymentName,
		}, deploymentReference,
	); err != nil {
		logger.Error(err, fmt.Sprintf("failed to get reference to deployment %s", deploymentName))
		return nil, err
	}
	if deploymentReference.UID == "" {
		msg := fmt.Sprintf("reference for deployment %s (%s) has no UID", deploymentName, fullyQualifiedName)
		err := fmt.Errorf("%s", msg)
		logger.Error(err, msg)
		return nil, err
	}
	logger.Debug(fmt.Sprintf("found deployment reference with UID: %s", deploymentReference.UID))
	return deploymentReference, nil
}

func operatorConfigurationIsManagedViaHelm(cliArgs *commandLineArguments) bool {
	// cliArgs.operatorConfigurationEndpoint is provided via Helm if and only if operator.dash0Export.enabled is true.
	return len(cliArgs.operatorConfigurationEndpoint) > 0
}

func createOrUpdateAutoOperatorConfigurationResource(
	ctx context.Context,
	readyCheckExecuter *ReadyCheckExecuter,
	operatorConfigurationValues *OperatorConfigurationValues,
	logger logd.Logger,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	if operatorConfigurationValues == nil {
		return nil
	}
	autoOperatorConfigurationResourceHandler := NewAutoOperatorConfigurationResourceHandler(
		startupTasksK8sClient,
		readyCheckExecuter,
		*operatorConfigurationValues,
		extraConfig.MonitoringTemplateRaw,
	)
	leaderElectionAwareRunnable.AddLeaderElectionClient(autoOperatorConfigurationResourceHandler)
	if operatorConfigurationResource, err :=
		autoOperatorConfigurationResourceHandler.CreateOrUpdateOperatorConfigurationResource(
			ctx,
			logger,
		); err != nil {
		logger.Error(err, "Failed to create the requested Dash0 operator configuration resource.")
		return nil
	} else {
		extraConfigMapWatcher.AddClient(autoOperatorConfigurationResourceHandler)
		return operatorConfigurationResource
	}
}

// setupCollectorReconciler sets up the reconciler that watches the OpenTelemetry collector resources managed by the
// operator, unless the troubleshooting setting operator.disableCollectorResourceWatches is enabled.
func setupCollectorReconciler(
	mgr ctrl.Manager,
	k8sClient client.Client,
	collectorManager *collectors.CollectorManager,
	envVars environmentVariables,
) error {
	if envVars.disableCollectorResourceWatches {
		setupLog.Warn(
			"The setting operator.disableCollectorResourceWatches is true, collector resources will not be " +
				"watched. This setting is intended for troubleshooting the OpenTelemetry collector setup.",
		)
		return nil
	}
	collectorReconciler := collectors.NewCollectorReconciler(
		k8sClient,
		collectorManager,
		envVars.operatorNamespace,
		envVars.oTelCollectorNamePrefix,
	)
	if err := collectorReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the collector reconciler: %w", err)
	}
	return nil
}

func setupAgent0ConnectorManager(
	mgr ctrl.Manager,
	k8sClient client.Client,
	envVars environmentVariables,
	images util.Images,
	operatorDeploymentSelfReference *appsv1.Deployment,
	pseudoClusterUid types.UID,
	developmentMode bool,
) (*agent0connector.Agent0ConnectorManager, error) {
	if !envVars.agent0ConnectorEnabled {
		// We might not have the permissions to manage the agent0 connector resources (in particular when telemetry
		// collection is also disabled), e.g. no permission to manage cluster roles, config maps etc.
		return nil, nil
	}
	agent0ConnectorConfig := util.Agent0ConnectorConfig{
		Images:            images,
		OperatorNamespace: envVars.operatorNamespace,
		NamePrefix:        envVars.oTelCollectorNamePrefix,
		PseudoClusterUid:  pseudoClusterUid,
		ServerAddress:     envVars.agent0ConnectorServerAddress,
		Insecure:          envVars.agent0ConnectorInsecure,
		Authorization:     agent0ConnectorAuthorization(envVars),
		DevelopmentMode:   developmentMode,
	}
	agent0ConnectorResourceManager := a0cresources.NewAgent0ConnectorResourceManager(
		k8sClient,
		mgr.GetScheme(),
		operatorDeploymentSelfReference,
		agent0ConnectorConfig,
	)
	agent0ConnectorManager := agent0connector.NewAgent0ConnectorManager(
		k8sClient,
		envVars.agent0ConnectorEnabled,
		developmentMode,
		agent0ConnectorResourceManager,
	)
	agent0ConnectorReconciler := agent0connector.NewAgent0ConnectorReconciler(
		k8sClient,
		agent0ConnectorManager,
		envVars.operatorNamespace,
		envVars.oTelCollectorNamePrefix,
	)
	if err := agent0ConnectorReconciler.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("unable to set up the agent0-connector reconciler: %w", err)
	}
	return agent0ConnectorManager, nil
}

// agent0ConnectorAuthorization builds the Dash0 authorization configuration for the agent0-connector workload from the
// operator's environment variables. A literal token (operator.agent0Connector.token) takes precedence over a secret
// reference (operator.agent0Connector.secretRef); if neither is configured, an empty authorization is returned (the
// Helm chart enforces that one is provided when Agent0 connector is enabled).
func agent0ConnectorAuthorization(envVars environmentVariables) dash0common.Authorization {
	if envVars.agent0ConnectorToken != "" {
		return dash0common.Authorization{Token: &envVars.agent0ConnectorToken}
	}
	if envVars.agent0ConnectorSecretRefName != "" && envVars.agent0ConnectorSecretRefKey != "" {
		return dash0common.Authorization{
			SecretRef: &dash0common.SecretRef{
				Name: envVars.agent0ConnectorSecretRefName,
				Key:  envVars.agent0ConnectorSecretRefKey,
			},
		}
	}
	return dash0common.Authorization{}
}

// triggerSecretRefExchangeAndStartSelfMonitoringIfPossible starts the OTel SDK directly, instead of waiting for the
// operator configuration resource to be picked up by a reconcile cycle. That is, if the command line parameters used to
// start the operator manager process had instructions to create an auto operator configuration resource,
// createOrUpdateAutoOperatorConfigurationResource has already triggered the asynchronous creation of the resource.
// Instead of waiting for the resource to actually be created, and then waiting for a reconcile request being routed to
// the OperatorConfigurationController, just for the purpose of initializing the OTel SDK, we can initialize the OTel
// SDK right away with the values from the command line parameters.
// The function also triggers exchanging the secret ref for a token if necessary (i.e. if the operator configuration
// resource command line parameters specify a secret ref instead of a token).
func triggerSecretRefExchangeAndStartSelfMonitoringIfPossible(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	oTelSdkStarter *selfmonitoringapiaccess.OTelSdkStarter,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	pseudoClusterUid types.UID,
	operatorVersion string,
	developmentMode bool,
) {
	if operatorConfigurationResource == nil {
		return
	}

	if operatorConfigurationResource.Spec.SelfMonitoring.Enabled == nil ||
		!*operatorConfigurationResource.Spec.SelfMonitoring.Enabled {
		return
	}

	selfMonitoringConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
			ctx,
			k8sClient,
			operatorNamespace,
			operatorConfigurationResource,
			setupLog,
		)
	if err != nil {
		setupLog.Error(
			err,
			"cannot generate self-monitoring configuration from operator configuration resource startup values",
		)
		return
	}

	oTelSdkStarter.SetOTelSdkParameters(
		ctx,
		selfMonitoringConfiguration.Export,
		selfMonitoringConfiguration.Token,
		pseudoClusterUid,
		operatorConfigurationResource.Spec.ClusterName,
		operatorDeploymentSelfReference.Namespace,
		operatorDeploymentSelfReference.UID,
		operatorDeploymentSelfReference.Name,
		operatorVersion,
		developmentMode,
		setupLog,
	)
}

func deleteMonitoringResourcesInAllNamespaces(logger logd.Logger) error {
	handler, err := predelete.NewOperatorPreDeleteHandler()
	if err != nil {
		logger.Error(err, "Failed to create the pre-delete handler.")
		return err
	}
	if err = handler.DeleteAllMonitoringResources(); err != nil {
		logger.Error(err, "Failed to delete all monitoring resources.")
		return err
	}
	return nil
}

func waitForOperatorConfigurationResourceAvailability(logger logd.Logger) error {
	handler, err := postinstall.NewOperatorPostInstallHandler()
	if err != nil {
		logger.Error(err, "Failed to create the post-install handler.")
		return err
	}
	if err = handler.WaitForOperatorConfigurationResourceToBecomeAvailable(); err != nil {
		logger.Error(err, "Failed to wait for the operator monitoring resource.")
		return err
	}
	return nil
}

func waitForAllowlistSynchronizerReadiness(logger logd.Logger, version string) error {
	handler, err := allowlistreadycheck.NewOperatorPreInstallHandler(version)
	if err != nil {
		logger.Error(err, "Failed to create the pre-install handler.")
		return err
	}
	if err = handler.WaitForAllowlistSynchronizerToBecomeReady(); err != nil {
		logger.Error(err, "Failed to wait for the AllowlistSynchronizer to become ready.")
		return err
	}
	return nil
}

func deleteDash0AllowlistSynchronizer(ctx context.Context, logger logd.Logger) error {
	handler, err := postdelete.NewOperatorPostDeleteHandler()
	if err != nil {
		logger.Error(err, "Failed to create the post-delete handler.")
		return err
	}
	if err = handler.DeleteGkeAutopilotAllowlistSynchronizer(ctx, logger); err != nil {
		return err
	}
	return nil
}

func setupResourceWebhooks(mgr ctrl.Manager, k8sClient client.Client, operatorNamespace string, telemetryCollectionEnabled bool, intelligentEdgeEnabled bool) error {
	if err := webhooks.NewOperatorConfigurationMutatingWebhookHandler(k8sClient).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the operator configuration mutating webhook: %w", err)
	}
	if err := webhooks.NewOperatorConfigurationValidationWebhookHandler(k8sClient, telemetryCollectionEnabled).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the operator configuration validation webhook: %w", err)
	}
	if intelligentEdgeEnabled {
		if err := webhooks.NewIntelligentEdgeValidationWebhookHandler(k8sClient).SetupWebhookWithManager(mgr); err != nil {
			return fmt.Errorf("unable to create the intelligent edge validation webhook: %w", err)
		}
	}
	if err := webhooks.NewMonitoringMutatingWebhookHandler(k8sClient, operatorNamespace).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring mutating webhook: %w", err)
	}
	if err := webhooks.NewMonitoringValidationWebhookHandler(k8sClient, operatorNamespace).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring validation webhook: %w", err)
	}
	if err := webhooks.SetupDash0MonitoringConversionWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring conversion webhook: %w", err)
	}
	if err := webhooks.SetupPersesDashboardConversionWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the Perses dashboard conversion webhook: %w", err)
	}
	return nil
}
