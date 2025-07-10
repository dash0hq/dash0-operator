// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	persesv1alpha1 "github.com/perses/perses-operator/api/v1alpha1"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/collectors"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/controller"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/predelete"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"
	"github.com/dash0hq/dash0-operator/internal/webhooks"
)

type environmentVariables struct {
	operatorNamespace                    string
	deploymentName                       string
	webhookServiceName                   string
	oTelCollectorNamePrefix              string
	operatorImage                        string
	initContainerImage                   string
	initContainerImagePullPolicy         corev1.PullPolicy
	collectorImage                       string
	collectorImagePullPolicy             corev1.PullPolicy
	configurationReloaderImage           string
	configurationReloaderImagePullPolicy corev1.PullPolicy
	filelogOffsetSyncImage               string
	filelogOffsetSyncImagePullPolicy     corev1.PullPolicy
	nodeIp                               string
	nodeName                             string
	podIp                                string
	sendBatchMaxSize                     *uint32
	instrumentationDebug                 bool
	debugVerbosityDetailed               bool
}

type commandLineArguments struct {
	isUninstrumentAll                                                     bool
	operatorConfigurationEndpoint                                         string
	operatorConfigurationToken                                            string
	operatorConfigurationSecretRefName                                    string
	operatorConfigurationSecretRefKey                                     string
	operatorConfigurationDataset                                          string
	operatorConfigurationApiEndpoint                                      string
	operatorConfigurationSelfMonitoringEnabled                            bool
	operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled bool
	operatorConfigurationCollectPodLabelsAndAnnotationsEnabled            bool
	operatorConfigurationClusterName                                      string
	forceUseOpenTelemetryCollectorServiceUrl                              bool
	disableOpenTelemetryCollectorHostPorts                                bool
	instrumentationDelays                                                 *instrumentation.DelayConfig
	metricsAddr                                                           string
	enableLeaderElection                                                  bool
	probeAddr                                                             string
	secureMetrics                                                         bool
	enableHTTP2                                                           bool
}

const (
	operatorNamespaceEnvVarName                    = "DASH0_OPERATOR_NAMESPACE"
	deploymentNameEnvVarName                       = "DASH0_DEPLOYMENT_NAME"
	webhookServiceNameEnvVarName                   = "DASH0_WEBHOOK_SERVICE_NAME"
	oTelCollectorNamePrefixEnvVarName              = "OTEL_COLLECTOR_NAME_PREFIX"
	operatorImageEnvVarName                        = "DASH0_OPERATOR_IMAGE"
	initContainerImageEnvVarName                   = "DASH0_INIT_CONTAINER_IMAGE"
	initContainerImagePullPolicyEnvVarName         = "DASH0_INIT_CONTAINER_IMAGE_PULL_POLICY"
	collectorImageEnvVarName                       = "DASH0_COLLECTOR_IMAGE"
	collectorImageImagePullPolicyEnvVarName        = "DASH0_COLLECTOR_IMAGE_PULL_POLICY"
	configurationReloaderImageEnvVarName           = "DASH0_CONFIGURATION_RELOADER_IMAGE"
	configurationReloaderImagePullPolicyEnvVarName = "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY"
	filelogOffsetSyncImageEnvVarName               = "DASH0_FILELOG_OFFSET_SYNC_IMAGE"
	filelogOffsetSyncImagePullPolicyEnvVarName     = "DASH0_FILELOG_OFFSET_SYNC_IMAGE_PULL_POLICY"
	k8sNodeIpEnvVarName                            = "K8S_NODE_IP"
	k8sNodeNameEnvVarName                          = "K8S_NODE_NAME"
	k8sPodIpEnvVarName                             = "K8S_POD_IP"

	oTelCollectorServiceBaseUrlPattern   = "http://%s-opentelemetry-collector-service.%s.svc.cluster.local:4318"
	oTelCollectorNodeLocalBaseUrlPattern = "http://$(%s):%d"

	developmentModeEnvVarName        = "DASH0_DEVELOPMENT_MODE"
	instrumentationDebugEnvVarName   = "DASH0_INSTRUMENTATION_DEBUG"
	debugVerbosityDetailedEnvVarName = "OTEL_COLLECTOR_DEBUG_VERBOSITY_DETAILED"
	sendBatchMaxSizeEnvVarName       = "OTEL_COLLECTOR_SEND_BATCH_MAX_SIZE"

	extraConfigFile = "/etc/config/extra.yaml"

	//nolint
	mandatoryEnvVarMissingMessageTemplate = "cannot start the Dash0 operator, the mandatory environment variable \"%s\" is missing"

	envVarValueTrue = "true"
)

var (
	setupLog logr.Logger

	leaderElectionAwareRunnable     = util.NewLeaderElectionAwareRunnable()
	startupTasksK8sClient           client.Client
	isDocker                        bool
	oTelSdkStarter                  *selfmonitoringapiaccess.OTelSdkStarter
	operatorDeploymentSelfReference *appsv1.Deployment
	envVars                         environmentVariables
	extraConfig                     util.ExtraConfig

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
	delegatingZapCoreWrapper := setUpLogging(crZapOpts)
	// setupLog is initialized after this point and can be used

	if cliArgs.isUninstrumentAll {
		if err := deleteMonitoringResourcesInAllNamespaces(&setupLog); err != nil {
			setupLog.Error(err, "deleting the Dash0 monitoring resources in all namespaces failed")
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

	tlsOpts := []func(*tls.Config){}
	if !cliArgs.enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := k8swebhook.NewServer(k8swebhook.Options{
		TLSOpts: tlsOpts,
	})

	var err error
	if err = readEnvironmentVariables(&setupLog); err != nil {
		os.Exit(1)
	}
	if err = readExtraConfigMap(); err != nil {
		os.Exit(1)
	}

	if err = initStartupTasksK8sClient(&setupLog); err != nil {
		os.Exit(1)
	}
	detectDocker(
		ctx,
		startupTasksK8sClient,
		&setupLog,
	)
	pseudoClusterUID := util.ReadPseudoClusterUID(ctx, startupTasksK8sClient, &setupLog)
	if operatorDeploymentSelfReference, err = findDeploymentReference(
		ctx,
		startupTasksK8sClient,
		envVars.operatorNamespace,
		envVars.deploymentName,
		&setupLog,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager lookup for its own deployment failed.")
		os.Exit(1)
	}

	var operatorConfigurationValues *OperatorConfigurationValues
	if len(cliArgs.operatorConfigurationEndpoint) > 0 {
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
			CollectPodLabelsAndAnnotationsEnabled:            cliArgs.operatorConfigurationCollectPodLabelsAndAnnotationsEnabled,
			ClusterName:                                      cliArgs.operatorConfigurationClusterName,
		}
		if len(cliArgs.operatorConfigurationApiEndpoint) > 0 {
			operatorConfigurationValues.ApiEndpoint = cliArgs.operatorConfigurationApiEndpoint
		}
		if len(cliArgs.operatorConfigurationDataset) > 0 {
			operatorConfigurationValues.Dataset = cliArgs.operatorConfigurationDataset
		}
	}

	if err = startOperatorManager(
		ctx,
		cliArgs,
		tlsOpts,
		webhookServer,
		operatorConfigurationValues,
		delegatingZapCoreWrapper,
		pseudoClusterUID,
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
		"If set, the process will remove all Dash0 monitoring resources from all namespaces in the cluste, then "+
			"exit. This will trigger the Dash0 monitoring resources' finalizers in each namespace, which in turn will "+
			"revert the instrumentation of all workloads in all namespaces.",
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
		"The Dash0 API endpoint for managing dashboards and check rules via the operator.",
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
			"will be ignored if operator-configuration-endpoint is not set.")
	flag.BoolVar(
		&cliArgs.operatorConfigurationCollectPodLabelsAndAnnotationsEnabled,
		"operator-configuration-collect-pod-labels-and-annotations-enabled",
		true,
		"The value for collectPodLabelsAndAnnotations.enabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.")
	flag.StringVar(
		&cliArgs.operatorConfigurationClusterName,
		"operator-configuration-cluster-name",
		"",
		"The clusterName to set on the operator configuration resource; will be ignored if"+
			"operator-configuration-endpoint is not set. If set, the value will be added as the resource attribute "+
			"k8s.cluster.name to all telemetry.")
	flag.BoolVar(
		&cliArgs.forceUseOpenTelemetryCollectorServiceUrl,
		"force-use-otel-collector-service-url",
		false,
		"When modifying workloads, always use the service URL of the OpenTelemetry collector DaemonSet, instead of "+
			"routing telemetry from workloads via node-local traffic to the node IP/host port of the collector pod.")
	flag.BoolVar(
		&cliArgs.disableOpenTelemetryCollectorHostPorts,
		"disable-otel-collector-host-ports",
		false,
		"Disable the host ports of the OpenTelemetry collector pods managed by the operator. Implies "+
			"--force-use-otel-collector-service-url.")
	cliArgs.instrumentationDelays = &instrumentation.DelayConfig{}
	flag.Uint64Var(
		&cliArgs.instrumentationDelays.AfterEachWorkloadMillis,
		"instrumentation-delay-after-each-workload-millis",
		0,
		"An optional delay to stagger access to the Kubernetes API server to instrument existing workloads at "+
			"operator startup or when enabling instrumentation for a new namespace via Dash0Monitoring resource. This "+
			"delay will be applied after each individual workload.")
	flag.Uint64Var(
		&cliArgs.instrumentationDelays.AfterEachNamespaceMillis,
		"instrumentation-delay-after-each-namespace-millis",
		0,
		"An optional delay to stagger access to the Kubernetes API server to instrument (or update the "+
			"instrumentation of) existing workloads at operator startup. This delay will be applied each time all "+
			"workloads in a namespace have been processed, before starting with the next namespace.")
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

	if cliArgs.disableOpenTelemetryCollectorHostPorts {
		// disableOpenTelemetryCollectorHostPorts implies forceUseOpenTelemetryCollectorServiceUrl
		cliArgs.forceUseOpenTelemetryCollectorServiceUrl = true
	}
	return opts
}

func setUpLogging(crZapOpts crzap.Opts) *zaputil.DelegatingZapCoreWrapper {
	o := zaputil.ConvertOptions([]crzap.Opts{crZapOpts})

	// this basically mimics New<type>Config, but with a custom sink
	sink := zapcore.AddSync(o.DestWriter)

	o.ZapOpts = append(o.ZapOpts, zap.ErrorOutput(sink))
	defaultZapCore := zapcore.NewCore(&crzap.KubeAwareEncoder{Encoder: o.Encoder, Verbose: o.Development}, sink, o.Level)

	delegatingZapCoreWrapper := zaputil.NewDelegatingZapCoreWrapper()

	// Multiplex log records to stdout (defaultZapCore) and also to the OTel log SDK (delegatingZapCore). Additional
	// plot twist: The OTel logger will only be initialized later, potentially after the operator configuration has been
	// reconciled. The delegatingZapCore will buffer all messages logged at startup up to th point when the OTel logger
	// is actually initialized, then re-spool them to the OTel SDK logger.
	teeCore := zapcore.NewTee(
		defaultZapCore,
		delegatingZapCoreWrapper.RootDelegatingZapCore,
	)
	crZapRawLogger := zaputil.NewRawFromCore(o, teeCore)
	zapLogger := zapr.NewLogger(crZapRawLogger)

	// Set the created multiplexing logger as the logger for the controller-runtime package.
	ctrl.SetLogger(zapLogger)

	setupLog = ctrl.Log.WithName("setup")

	return delegatingZapCoreWrapper
}

func readEnvironmentVariables(logger *logr.Logger) error {
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

	oTelCollectorNamePrefix, isSet := os.LookupEnv(oTelCollectorNamePrefixEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, oTelCollectorNamePrefixEnvVarName)
	}

	operatorImage, isSet := os.LookupEnv(operatorImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, operatorImageEnvVarName)
	}

	initContainerImage, isSet := os.LookupEnv(initContainerImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, initContainerImageEnvVarName)
	}
	initContainerImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(initContainerImagePullPolicyEnvVarName)

	collectorImage, isSet := os.LookupEnv(collectorImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, collectorImageEnvVarName)
	}
	collectorImagePullPolicy := readOptionalPullPolicyFromEnvironmentVariable(collectorImageImagePullPolicyEnvVarName)

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

	debugVerbosityDetailedRaw, isSet := os.LookupEnv(debugVerbosityDetailedEnvVarName)
	debugVerbosityDetailed := isSet && strings.ToLower(debugVerbosityDetailedRaw) == envVarValueTrue

	var sendBatchMaxSize *uint32
	sendBatchMaxSizeRaw, isSet := os.LookupEnv(sendBatchMaxSizeEnvVarName)
	if isSet {
		converted, err := strconv.Atoi(sendBatchMaxSizeRaw)
		if err != nil {
			logger.Error(err, "Ignoring invalid value for %s: %s", sendBatchMaxSizeEnvVarName, sendBatchMaxSizeRaw)
		} else {
			sendBatchMaxSize = ptr.To(uint32(converted))
		}
	}

	envVars = environmentVariables{
		operatorNamespace:                    operatorNamespace,
		deploymentName:                       deploymentName,
		webhookServiceName:                   webhookServiceName,
		oTelCollectorNamePrefix:              oTelCollectorNamePrefix,
		operatorImage:                        operatorImage,
		initContainerImage:                   initContainerImage,
		initContainerImagePullPolicy:         initContainerImagePullPolicy,
		collectorImage:                       collectorImage,
		collectorImagePullPolicy:             collectorImagePullPolicy,
		configurationReloaderImage:           configurationReloaderImage,
		configurationReloaderImagePullPolicy: configurationReloaderImagePullPolicy,
		filelogOffsetSyncImage:               filelogOffsetSyncImage,
		filelogOffsetSyncImagePullPolicy:     filelogOffsetSyncImagePullPolicy,
		nodeIp:                               nodeIp,
		nodeName:                             nodeName,
		podIp:                                podIp,
		sendBatchMaxSize:                     sendBatchMaxSize,
		instrumentationDebug:                 instrumentationDebug,
		debugVerbosityDetailed:               debugVerbosityDetailed,
	}

	return nil
}

// readExtraConfigMap reads the config map content, which is basically a container for structured configuration data
// would be cumbersome to pass in as a command line argument.
func readExtraConfigMap() error {
	var err error
	extraConfig, err = util.ReadExtraConfiguration(extraConfigFile)
	if err != nil {
		return fmt.Errorf("cannot read configuration file %s: %w", extraConfigFile, err)
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
			setupLog.Info(
				fmt.Sprintf(
					"Ignoring unknown pull policy setting (%s): %s.", envVarName, pullPolicyRaw))
		}
	}
	return ""
}

func initStartupTasksK8sClient(logger *logr.Logger) error {
	cfg := ctrl.GetConfigOrDie()
	var err error
	if startupTasksK8sClient, err = client.New(cfg, client.Options{
		Scheme: runtimeScheme,
	}); err != nil {
		logger.Error(err, "failed to create Kubernetes API client for startup tasks")
		return err
	}
	return nil
}

func detectDocker(
	ctx context.Context,
	k8sClient client.Client,
	logger *logr.Logger,
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
	pseudoClusterUID string,
	developmentMode bool,
) error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
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
	})
	if err != nil {
		return fmt.Errorf("unable to create the manager: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create the clientset client")
	}

	setupLog.Info(
		"operator manager configuration:",

		"operator image",
		envVars.operatorImage,

		"init container image",
		envVars.initContainerImage,
		"init container image pull policy override",
		envVars.initContainerImagePullPolicy,

		"collector image",
		envVars.collectorImage,
		"collector image pull policy override",
		envVars.collectorImagePullPolicy,

		"configuration reloader image",
		envVars.configurationReloaderImage,
		"configuration reloader image pull policy override",
		envVars.configurationReloaderImagePullPolicy,

		"operator namespace",
		envVars.operatorNamespace,
		"operator manager deployment name",
		envVars.deploymentName,
		"otel collector name prefix",
		envVars.oTelCollectorNamePrefix,
		"force-use OpenTelemetry collector service URL",
		cliArgs.forceUseOpenTelemetryCollectorServiceUrl,
		"disable OpenTelemetry collector host ports",
		cliArgs.disableOpenTelemetryCollectorHostPorts,

		"extra config",
		extraConfig,

		"instrumentation delays",
		cliArgs.instrumentationDelays,
		"development mode",
		developmentMode,
		"verbosity detailed",
		envVars.debugVerbosityDetailed,
		"instrumentation debug",
		envVars.instrumentationDebug,
	)

	err = startDash0Controllers(
		ctx,
		mgr,
		clientset,
		cliArgs,
		operatorConfigurationValues,
		delegatingZapCoreWrapper,
		pseudoClusterUID,
		developmentMode,
	)
	if err != nil {
		return err
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up the health check: %w", err)
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up the ready check: %w", err)
	}

	defer func() {
		if thirdPartyResourceSynchronizationQueue != nil {
			controller.StopProcessingThirdPartySynchronizationQueue(thirdPartyResourceSynchronizationQueue, &setupLog)
		}
	}()

	setupLog.Info("starting manager (waiting for leader election)")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("unable to set up the signal handler: %w", err)
	}
	// ^mgr.Start(...) blocks. It only returns when the manager is terminating.

	if oTelSdkStarter != nil {
		oTelSdkStarter.ShutDown(ctx, &setupLog)
	}

	return nil
}

func startDash0Controllers(
	ctx context.Context,
	mgr manager.Manager,
	clientset *kubernetes.Clientset,
	cliArgs *commandLineArguments,
	operatorConfigurationValues *OperatorConfigurationValues,
	delegatingZapCoreWrapper *zaputil.DelegatingZapCoreWrapper,
	pseudoClusterUID string,
	developmentMode bool,
) error {
	images := util.Images{
		OperatorImage:                        envVars.operatorImage,
		InitContainerImage:                   envVars.initContainerImage,
		InitContainerImagePullPolicy:         envVars.initContainerImagePullPolicy,
		CollectorImage:                       envVars.collectorImage,
		CollectorImagePullPolicy:             envVars.collectorImagePullPolicy,
		ConfigurationReloaderImage:           envVars.configurationReloaderImage,
		ConfigurationReloaderImagePullPolicy: envVars.configurationReloaderImagePullPolicy,
		FilelogOffsetSyncImage:               envVars.filelogOffsetSyncImage,
		FilelogOffsetSyncImagePullPolicy:     envVars.filelogOffsetSyncImagePullPolicy,
	}

	isIPv6Cluster := strings.Count(envVars.podIp, ":") >= 2
	oTelCollectorBaseUrl := determineCollectorBaseUrl(cliArgs.forceUseOpenTelemetryCollectorServiceUrl, isIPv6Cluster)

	startupInstrumenter := instrumentation.NewInstrumenter(
		// The k8s client will be added later, in internal/startup/instrument_at_startup.go#Start.
		nil,
		clientset,
		mgr.GetEventRecorderFor("dash0-startup-tasks"),
		images,
		extraConfig,
		oTelCollectorBaseUrl,
		cliArgs.instrumentationDelays,
		envVars.instrumentationDebug,
	)
	var err error
	if err = mgr.Add(leaderElectionAwareRunnable); err != nil {
		return fmt.Errorf("unable to add the leader election aware runnable: %w", err)
	}
	// register the instrument-at-startup task to run once this operator manager becomes leader
	if err = mgr.Add(NewInstrumentAtStartupRunnable(mgr, startupInstrumenter)); err != nil {
		return fmt.Errorf("unable to add instrument-at-startup task: %w", err)
	}
	operatorConfigurationResource := executeStartupTasks(
		ctx,
		operatorConfigurationValues,
		&setupLog,
	)

	k8sClient := mgr.GetClient()
	instrumenter := instrumentation.NewInstrumenter(
		k8sClient,
		clientset,
		mgr.GetEventRecorderFor("dash0-monitoring-controller"),
		images,
		extraConfig,
		oTelCollectorBaseUrl,
		cliArgs.instrumentationDelays,
		envVars.instrumentationDebug,
	)

	oTelColResourceManager := otelcolresources.NewOTelColResourceManager(
		k8sClient,
		mgr.GetScheme(),
		operatorDeploymentSelfReference,
		envVars.oTelCollectorNamePrefix,
		extraConfig,
		envVars.sendBatchMaxSize,
		envVars.nodeIp,
		envVars.nodeName,
		pseudoClusterUID,
		isIPv6Cluster,
		isDocker,
		cliArgs.disableOpenTelemetryCollectorHostPorts,
		developmentMode,
		envVars.debugVerbosityDetailed,
	)
	collectorManager := &collectors.CollectorManager{
		Client:                 k8sClient,
		Clientset:              clientset,
		OTelColResourceManager: oTelColResourceManager,
	}
	collectorReconciler := &collectors.CollectorReconciler{
		Client:                  k8sClient,
		CollectorManager:        collectorManager,
		Images:                  images,
		OperatorNamespace:       envVars.operatorNamespace,
		OTelCollectorNamePrefix: envVars.oTelCollectorNamePrefix,
	}
	if err := collectorReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the collector reconciler: %w", err)
	}

	thirdPartyResourceSynchronizationQueue =
		workqueue.NewTypedWithConfig(
			workqueue.TypedQueueConfig[controller.ThirdPartyResourceSyncJob]{
				Name: "dash0-third-party-resource-reconcile-queue",
			})
	persesDashboardCrdReconciler := controller.NewPersesDashboardCrdReconciler(
		k8sClient,
		thirdPartyResourceSynchronizationQueue,
		leaderElectionAwareRunnable,
	)
	if err := persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, startupTasksK8sClient, &setupLog); err != nil {
		return fmt.Errorf("unable to set up the Perses dashboard reconciler: %w", err)
	}
	prometheusRuleCrdReconciler := controller.NewPrometheusRuleCrdReconciler(
		k8sClient,
		thirdPartyResourceSynchronizationQueue,
		leaderElectionAwareRunnable,
	)
	if err := prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, startupTasksK8sClient, &setupLog); err != nil {
		return fmt.Errorf("unable to set up the Prometheus rule reconciler: %w", err)
	}
	controller.StartProcessingThirdPartySynchronizationQueue(thirdPartyResourceSynchronizationQueue, &setupLog)

	setupLog.Info("Creating the self-monitoring OTel SDK starter.")
	oTelSdkStarter = selfmonitoringapiaccess.NewOTelSdkStarter(delegatingZapCoreWrapper)

	setupLog.Info("Creating the operator configuration resource reconciler.")
	operatorConfigurationReconciler := &controller.OperatorConfigurationReconciler{
		Client:    k8sClient,
		Clientset: clientset,
		ApiClients: []controller.ApiClient{
			persesDashboardCrdReconciler,
			prometheusRuleCrdReconciler,
		},
		Scheme:                      mgr.GetScheme(),
		Recorder:                    mgr.GetEventRecorderFor("dash0-operator-configuration-controller"),
		CollectorManager:            collectorManager,
		PseudoClusterUID:            pseudoClusterUID,
		OperatorDeploymentNamespace: operatorDeploymentSelfReference.Namespace,
		OperatorDeploymentUID:       operatorDeploymentSelfReference.UID,
		OperatorDeploymentName:      operatorDeploymentSelfReference.Name,
		OTelSdkStarter:              oTelSdkStarter,
		Images:                      images,
		OperatorNamespace:           envVars.operatorNamespace,
		DevelopmentMode:             developmentMode,
	}
	setupLog.Info("Starting the operator configuration resource reconciler.")
	if err := operatorConfigurationReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the operator configuration resource reconciler: %w", err)
	}
	setupLog.Info("The operator configuration resource reconciler has been started.")

	setupLog.Info("Creating the monitoring resource reconciler.")
	monitoringReconciler := &controller.MonitoringReconciler{
		Client:            k8sClient,
		Clientset:         clientset,
		Instrumenter:      instrumenter,
		CollectorManager:  collectorManager,
		Images:            images,
		OperatorNamespace: envVars.operatorNamespace,
	}
	setupLog.Info("Starting the monitoring resource reconciler.")
	if err := monitoringReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the monitoring reconciler: %w", err)
	}
	setupLog.Info("The monitoring resource reconciler has been started.")

	if err := (&webhooks.InstrumentationWebhookHandler{
		Client:               k8sClient,
		Recorder:             mgr.GetEventRecorderFor("dash0-instrumentation-webhook"),
		Images:               images,
		ExtraConfig:          extraConfig,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
		InstrumentationDebug: envVars.instrumentationDebug,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the instrumentation webhook: %w", err)
	}

	if err := (&webhooks.OperatorConfigurationMutatingWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the operator configuration mutating webhook: %w", err)
	}
	if err := (&webhooks.OperatorConfigurationValidationWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the operator configuration validation webhook: %w", err)
	}
	if err := (&webhooks.MonitoringMutatingWebhookHandler{
		Client:            k8sClient,
		OperatorNamespace: envVars.operatorNamespace,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring mutating webhook: %w", err)
	}
	if err := (&webhooks.MonitoringValidationWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring validation webhook: %w", err)
	}
	if err := webhooks.SetupDash0MonitoringConversionWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring conversion webhook: %w", err)
	}

	oTelSdkStarter.WaitForOTelConfig(
		[]selfmonitoringapiaccess.SelfMonitoringMetricsClient{
			operatorConfigurationReconciler,
			monitoringReconciler,
			persesDashboardCrdReconciler,
			prometheusRuleCrdReconciler,
		},
	)
	authTokenClients := []selfmonitoringapiaccess.AuthTokenClient{
		oTelSdkStarter,
		persesDashboardCrdReconciler,
		prometheusRuleCrdReconciler,
	}
	operatorConfigurationReconciler.AuthTokenClients = authTokenClients

	triggerSecretRefExchangeAndStartSelfMonitoringIfPossible(
		ctx,
		oTelSdkStarter,
		authTokenClients,
		operatorConfigurationResource,
		pseudoClusterUID,
		images.GetOperatorVersion(),
		developmentMode,
	)

	return nil
}

func determineCollectorBaseUrl(forceOTelCollectorServiceUrl bool, isIPv6Cluster bool) string {
	oTelCollectorServiceBaseUrl :=
		fmt.Sprintf(
			oTelCollectorServiceBaseUrlPattern,
			envVars.oTelCollectorNamePrefix,
			envVars.operatorNamespace)
	oTelCollectorNodeLocalBaseUrl := fmt.Sprintf(
		oTelCollectorNodeLocalBaseUrlPattern,
		otelcolresources.EnvVarDash0NodeIp,
		otelcolresources.OtlpHttpHostPort,
	)

	if forceOTelCollectorServiceUrl {
		return oTelCollectorServiceBaseUrl
	}

	// Using the node's IPv6 address for the collector base URL should actually just work:
	// if m.instrumentationMetadata.IsIPv6Cluster {
	//	 oTelCollectorNodeLocalBaseUrlPattern = "http://[$(%s)]:%d"
	// }
	//
	// But apparently the Node.js OpenTelemetry SDK tries to resolve that as a hostname, resulting in
	// Error: getaddrinfo ENOTFOUND [2a05:d014:1bc2:3702:fc43:fec6:1d88:ace5]\n    at GetAddrInfoReqWrap.onlookup
	// all [as oncomplete] (node:dns:120:26)
	//
	// To avoid that, we fall back to the service URL of the collector in IPv6 clusters.
	//
	// Would be worth to give this another try after implementing
	// https://linear.app/dash0/issue/ENG-2132.
	if isIPv6Cluster {
		return oTelCollectorServiceBaseUrl
	}

	// By default, and if forceOTelCollectorServiceUrl has not been set, and it is also not an IPv6 cluster, use a
	// node-local route by sending telemetry from workloads to the OTel collector daemonset pod on the same node via
	// the node's IP address and the collector's host port.
	return oTelCollectorNodeLocalBaseUrl
}

func findDeploymentReference(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	deploymentName string,
	logger *logr.Logger,
) (*appsv1.Deployment, error) {
	deploymentReference := &appsv1.Deployment{}
	fullyQualifiedName := fmt.Sprintf("%s/%s", operatorNamespace, deploymentName)
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: operatorNamespace,
		Name:      deploymentName,
	}, deploymentReference); err != nil {
		logger.Error(err, fmt.Sprintf("failed to get reference to deployment %s", deploymentName))
		return nil, err
	}
	if deploymentReference.UID == "" {
		msg := fmt.Sprintf("reference for deployment %s (%s) has no UID", deploymentName, fullyQualifiedName)
		err := fmt.Errorf("%s", msg)
		logger.Error(err, msg)
		return nil, err
	}
	return deploymentReference, nil
}

func executeStartupTasks(
	ctx context.Context,
	operatorConfigurationValues *OperatorConfigurationValues,
	logger *logr.Logger,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	operatorConfigurationResource := createOrUpdateAutoOperatorConfigurationResource(
		ctx,
		startupTasksK8sClient,
		operatorConfigurationValues,
		logger,
	)
	return operatorConfigurationResource
}

func createOrUpdateAutoOperatorConfigurationResource(
	ctx context.Context,
	k8sClient client.Client,
	operatorConfigurationValues *OperatorConfigurationValues,
	logger *logr.Logger,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	if operatorConfigurationValues == nil {
		return nil
	}
	handler := AutoOperatorConfigurationResourceHandler{
		Client:             k8sClient,
		OperatorNamespace:  envVars.operatorNamespace,
		WebhookServiceName: envVars.webhookServiceName,
	}
	if operatorConfigurationResource, err :=
		handler.CreateOrUpdateOperatorConfigurationResource(ctx, operatorConfigurationValues, logger); err != nil {
		logger.Error(err, "Failed to create the requested Dash0 operator configuration resource.")
		return nil
	} else {
		return operatorConfigurationResource
	}
}

// triggerSecretRefExchangeAndStartSelfMonitoringIfPossible starts the OTel SDK directly, instead of waiting for the
// operator configuration resource to be picked up by a reconcile cycle. That is, if the command line parameters used to
// start the operator manager process had instructions to create an auto operator configuration resource,
// executeStartupTasks has already triggered the asynchronous creation of the resource. Instead of waiting for the
// resource to actually be created, and then waiting for a reconcile request being routed to the
// OperatorConfigurationController, just for the purpose of initializing the OTel SDK, we can initialize the OTel SDK
// right away with the values from the command line parameters.
// The function also triggers exchanging the secret ref for a token if necessary (i.e. if the operator configuration
// resource command line parameters specify a secret ref instead of a token).
func triggerSecretRefExchangeAndStartSelfMonitoringIfPossible(
	ctx context.Context,
	oTelSdkStarter *selfmonitoringapiaccess.OTelSdkStarter,
	authTokenClients []selfmonitoringapiaccess.AuthTokenClient,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	pseudoClusterUID string,
	operatorVersion string,
	developmentMode bool,
) {
	if operatorConfigurationResource == nil {
		return
	}

	if operatorConfigurationResource.Spec.Export != nil &&
		operatorConfigurationResource.Spec.Export.Dash0 != nil &&
		operatorConfigurationResource.Spec.Export.Dash0.Authorization.SecretRef != nil {
		if err := selfmonitoringapiaccess.ExchangeSecretRefForToken(
			ctx,
			startupTasksK8sClient,
			authTokenClients,
			envVars.operatorNamespace,
			operatorConfigurationResource,
			&setupLog,
		); err != nil {
			setupLog.Error(err, "cannot exchange secret ref for token")
			// Deliberately not aborting the rest of the operations here, we will try to exchange the secret ref for a
			// token again later via the reconcile cycle of the operator configuration controller; there is no reason to
			// not make the rest of the OTel SDK config available to the oTelSdkStarter here already.
		}
	}

	if operatorConfigurationResource.Spec.SelfMonitoring.Enabled == nil ||
		!*operatorConfigurationResource.Spec.SelfMonitoring.Enabled {
		return
	}

	selfMonitoringConfiguration, err :=
		selfmonitoringapiaccess.ConvertOperatorConfigurationResourceToSelfMonitoringConfiguration(
			operatorConfigurationResource,
			&setupLog,
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
		pseudoClusterUID,
		operatorConfigurationResource.Spec.ClusterName,
		operatorDeploymentSelfReference.Namespace,
		operatorDeploymentSelfReference.UID,
		operatorDeploymentSelfReference.Name,
		operatorVersion,
		developmentMode,
		&setupLog,
	)
}

func deleteMonitoringResourcesInAllNamespaces(logger *logr.Logger) error {
	handler, err := predelete.NewOperatorPreDeleteHandler()
	if err != nil {
		logger.Error(err, "Failed to create the OperatorPreDeleteHandler.")
		return err
	}
	err = handler.DeleteAllMonitoringResources()
	if err != nil {
		logger.Error(err, "Failed to delete all monitoring resources.")
		return err
	}
	return nil
}
