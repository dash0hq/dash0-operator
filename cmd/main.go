// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

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
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/controller"
	"github.com/dash0hq/dash0-operator/internal/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/predelete"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/startup"
	"github.com/dash0hq/dash0-operator/internal/util"
	zaputil "github.com/dash0hq/dash0-operator/internal/util/zap"
	"github.com/dash0hq/dash0-operator/internal/webhooks"
)

type environmentVariables struct {
	operatorNamespace                    string
	deploymentName                       string
	webhookServiceName                   string
	secretRefResolverDeploymentName      string
	tokenUpdateServicePort               string
	oTelCollectorNamePrefix              string
	operatorImage                        string
	initContainerImage                   string
	initContainerImagePullPolicy         corev1.PullPolicy
	collectorImage                       string
	collectorImagePullPolicy             corev1.PullPolicy
	configurationReloaderImage           string
	configurationReloaderImagePullPolicy corev1.PullPolicy
	filelogOffsetSynchImage              string
	filelogOffsetSynchImagePullPolicy    corev1.PullPolicy
	nodeIp                               string
	nodeName                             string
	podName                              string
	podIp                                string
	sendBatchMaxSize                     *uint32
	debugVerbosityDetailed               bool
}

const (
	operatorNamespaceEnvVarName                    = "DASH0_OPERATOR_NAMESPACE"
	deploymentNameEnvVarName                       = "DASH0_DEPLOYMENT_NAME"
	webhookServiceNameEnvVarName                   = "DASH0_WEBHOOK_SERVICE_NAME"
	secretRefResolverDeploymentNameEnvVarName      = "DASH0_SECRET_REF_RESOLVER_DEPLOYMENT_NAME"
	tokenUpdateServicePortEnvVarName               = "DASH0_TOKEN_UPDATE_SERVICE_PORT"
	oTelCollectorNamePrefixEnvVarName              = "OTEL_COLLECTOR_NAME_PREFIX"
	operatorImageEnvVarName                        = "DASH0_OPERATOR_IMAGE"
	initContainerImageEnvVarName                   = "DASH0_INIT_CONTAINER_IMAGE"
	initContainerImagePullPolicyEnvVarName         = "DASH0_INIT_CONTAINER_IMAGE_PULL_POLICY"
	collectorImageEnvVarName                       = "DASH0_COLLECTOR_IMAGE"
	collectorImageImagePullPolicyEnvVarName        = "DASH0_COLLECTOR_IMAGE_PULL_POLICY"
	configurationReloaderImageEnvVarName           = "DASH0_CONFIGURATION_RELOADER_IMAGE"
	configurationReloaderImagePullPolicyEnvVarName = "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY"
	filelogOffsetSynchImageEnvVarName              = "DASH0_FILELOG_OFFSET_SYNCH_IMAGE"
	filelogOffsetSynchImagePullPolicyEnvVarName    = "DASH0_FILELOG_OFFSET_SYNCH_IMAGE_PULL_POLICY"
	k8sNodeIpEnvVarName                            = "K8S_NODE_IP"
	k8sNodeNameEnvVarName                          = "K8S_NODE_NAME"
	k8sPodNameEnvVarName                           = "K8S_POD_NAME"
	k8sPodIpEnvVarName                             = "K8S_POD_IP"

	developmentModeEnvVarName        = "DASH0_DEVELOPMENT_MODE"
	debugVerbosityDetailedEnvVarName = "OTEL_COLLECTOR_DEBUG_VERBOSITY_DETAILED"
	sendBatchMaxSizeEnvVarName       = "OTEL_COLLECTOR_SEND_BATCH_MAX_SIZE"

	oTelColExtraConfigFile = "/etc/config/otelcolextra.yaml"

	//nolint
	mandatoryEnvVarMissingMessageTemplate = "cannot start the Dash0 operator, the mandatory environment variable \"%s\" is missing"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	startupTasksK8sClient           client.Client
	isDocker                        bool
	oTelSdkStarter                  *selfmonitoringapiaccess.OTelSdkStarter
	operatorDeploymentSelfReference *appsv1.Deployment
	envVars                         environmentVariables

	thirdPartyResourceSynchronizationQueue *workqueue.Typed[controller.ThirdPartyResourceSyncJob]
	tokenUpdateService                     *selfmonitoringapiaccess.TokenUpdateService
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dash0v1alpha1.AddToScheme(scheme))

	// required for Perses dashboard controller and Prometheus rules controller.
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(persesv1alpha1.AddToScheme(scheme))
	utilruntime.Must(prometheusv1.AddToScheme(scheme))
}

func main() {
	ctx := context.Background()
	var operatorConfigurationEndpoint string
	var operatorConfigurationToken string
	var operatorConfigurationSecretRefName string
	var operatorConfigurationSecretRefKey string
	var operatorConfigurationDataset string
	var operatorConfigurationApiEndpoint string
	var operatorConfigurationSelfMonitoringEnabled bool
	var operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled bool
	var operatorConfigurationClusterName string
	var isUninstrumentAll bool
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	flag.BoolVar(
		&isUninstrumentAll,
		"uninstrument-all",
		false,
		"If set, the process will remove all Dash0 monitoring resources from all namespaces in the cluste, then "+
			"exit. This will trigger the Dash0 monitoring resources' finalizers in each namespace, which in turn will "+
			"revert the instrumentation of all workloads in all namespaces.",
	)
	flag.StringVar(
		&operatorConfigurationEndpoint,
		"operator-configuration-endpoint",
		"",
		"The Dash0 endpoint gRPC URL for creating an operator configuration resource.",
	)
	flag.StringVar(
		&operatorConfigurationToken,
		"operator-configuration-token",
		"",
		"The Dash0 auth token for creating an operator configuration resource.",
	)
	flag.StringVar(
		&operatorConfigurationSecretRefName,
		"operator-configuration-secret-ref-name",
		"",
		"The name of an existing Kubernetes secret containing the Dash0 auth token, used to creating an operator "+
			"configuration resource.",
	)
	flag.StringVar(
		&operatorConfigurationSecretRefKey,
		"operator-configuration-secret-ref-key",
		"",
		"The key in an existing Kubernetes secret containing the Dash0 auth token, used to creating an operator "+
			"configuration resource.",
	)
	flag.StringVar(
		&operatorConfigurationDataset,
		"operator-configuration-dataset",
		"default",
		"The Dash0 dataset into which telemetry will be reported and which will be used for API access.",
	)
	flag.StringVar(
		&operatorConfigurationApiEndpoint,
		"operator-configuration-api-endpoint",
		"",
		"The Dash0 API endpoint for managing dashboards and check rules via the operator.",
	)
	flag.BoolVar(
		&operatorConfigurationSelfMonitoringEnabled,
		"operator-configuration-self-monitoring-enabled",
		true,
		"Whether to set selfMonitoring.enabled on the operator configuration resource; will be ignored if "+
			"operator-configuration-endpoint is not set.",
	)
	flag.BoolVar(
		&operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled,
		"operator-configuration-kubernetes-infrastructure-metrics-collection-enabled",
		true,
		"Whether to set kubernetesInfrastructureMetricsCollectionEnabled on the operator configuration resource; "+
			"will be ignored if operator-configuration-endpoint is not set.")
	flag.StringVar(
		&operatorConfigurationClusterName,
		"operator-configuration-cluster-name",
		"",
		"The clusterName to set on the operator configuration resource; will be ignored if"+
			"operator-configuration-endpoint is not set. If set, the value will be added as the resource attribute "+
			"k8s.cluster.name to all telemetry.")
	flag.StringVar(
		&metricsAddr,
		"metrics-bind-address",
		":8080",
		"The address the metric endpoint binds to.",
	)
	flag.StringVar(
		&probeAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.",
	)
	flag.BoolVar(
		&enableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for operator manager. "+
			"Enabling this will ensure there is only one active operator manager.",
	)
	flag.BoolVar(
		&secureMetrics,
		"metrics-secure",
		false,
		"If set, the metrics endpoint is served securely.",
	)
	flag.BoolVar(
		&enableHTTP2,
		"enable-http2",
		false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers.",
	)

	developmentModeRaw, isSet := os.LookupEnv(developmentModeEnvVarName)
	developmentMode := isSet && strings.ToLower(developmentModeRaw) == "true"

	delegatingZapCore := setUpLogging(developmentMode)

	if isUninstrumentAll {
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
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := k8swebhook.NewServer(k8swebhook.Options{
		TLSOpts: tlsOpts,
	})

	var err error
	if err = readEnvironmentVariables(&setupLog); err != nil {
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
	pseudoClusterUID := readPseudoClusterUid(ctx, startupTasksK8sClient, &setupLog)
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

	var operatorConfigurationValues *startup.OperatorConfigurationValues
	if len(operatorConfigurationEndpoint) > 0 {
		operatorConfigurationValues = &startup.OperatorConfigurationValues{
			Endpoint: operatorConfigurationEndpoint,
			Token:    operatorConfigurationToken,
			SecretRef: startup.SecretRef{
				Name: operatorConfigurationSecretRefName,
				Key:  operatorConfigurationSecretRefKey,
			},
			SelfMonitoringEnabled: operatorConfigurationSelfMonitoringEnabled,
			//nolint:lll
			KubernetesInfrastructureMetricsCollectionEnabled: operatorConfigurationKubernetesInfrastructureMetricsCollectionEnabled,
			ClusterName: operatorConfigurationClusterName,
		}
		if len(operatorConfigurationApiEndpoint) > 0 {
			operatorConfigurationValues.ApiEndpoint = operatorConfigurationApiEndpoint
		}
		if len(operatorConfigurationDataset) > 0 {
			operatorConfigurationValues.Dataset = operatorConfigurationDataset
		}
	}

	if err = startOperatorManager(
		ctx,
		metricsAddr,
		secureMetrics,
		tlsOpts,
		webhookServer,
		probeAddr,
		enableLeaderElection,
		operatorConfigurationValues,
		delegatingZapCore,
		pseudoClusterUID,
		developmentMode,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager process failed to start.")
		os.Exit(1)
	}
}

func setUpLogging(developmentMode bool) *zaputil.DelegatingZapCore {
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

	crZapOpts := crzap.UseFlagOptions(&opts)
	o := zaputil.ConvertOptions([]crzap.Opts{crZapOpts})

	// this basically mimics New<type>Config, but with a custom sink
	sink := zapcore.AddSync(o.DestWriter)

	o.ZapOpts = append(o.ZapOpts, zap.ErrorOutput(sink))
	defaultZapCore := zapcore.NewCore(&crzap.KubeAwareEncoder{Encoder: o.Encoder, Verbose: o.Development}, sink, o.Level)

	delegatingZapCore := zaputil.NewDelegatingZapCore()

	// Multiplex log records to stdout (defaultZapCore) and also to the OTel log SDK (delegatingZapCore). Additional
	// plot twist: The OTel logger will only be initialized later, potentially after the operator configuration has been
	// reconciled. The delegatingZapCore will buffer all messages logged at startup up to th point when the OTel logger
	// is actually initialized, then re-spool them to the OTel SDK logger.
	teeCore := zapcore.NewTee(
		defaultZapCore,
		delegatingZapCore,
	)
	crZapRawLogger := zaputil.NewRawFromCore(o, teeCore)
	zapLogger := zapr.NewLogger(crZapRawLogger)

	// Set the created multiplexing logger as the logger for the controller-runtime package.
	ctrl.SetLogger(zapLogger)

	return delegatingZapCore
}

func startOperatorManager(
	ctx context.Context,
	metricsAddr string,
	secureMetrics bool,
	tlsOpts []func(*tls.Config),
	webhookServer k8swebhook.Server,
	probeAddr string,
	enableLeaderElection bool,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	delegatingZapCore *zaputil.DelegatingZapCore,
	pseudoClusterUID string,
	developmentMode bool,
) error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "5ae7ac41.dash0.com",

		// We are deliberately not setting LeaderElectionReleaseOnCancel to true, since we cannot guarantee that the
		// operator manager will terminate immediately, we need to shut down a couple of internal components before
		// terminating (self monitoring OTel SDK shutdown, token update service shutdown, ...).
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

		"development mode",
		developmentMode,
	)

	err = startDash0Controllers(
		ctx,
		mgr,
		clientset,
		operatorConfigurationValues,
		delegatingZapCore,
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

	setupLog.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("unable to set up the signal handler: %w", err)
	}
	// ^mgr.Start(...) blocks. It only returns when the manager is terminating.

	if tokenUpdateService != nil {
		tokenUpdateService.Stop(&setupLog)
	}
	if oTelSdkStarter != nil {
		oTelSdkStarter.ShutDown(ctx, &setupLog)
	}

	return nil
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

	secretRefResolverDeploymentName, isSet := os.LookupEnv(secretRefResolverDeploymentNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, secretRefResolverDeploymentNameEnvVarName)
	}

	tokenUpdateServicePort, isSet := os.LookupEnv(tokenUpdateServicePortEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, tokenUpdateServicePortEnvVarName)
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

	filelogOffsetSynchImage, isSet := os.LookupEnv(filelogOffsetSynchImageEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, filelogOffsetSynchImageEnvVarName)
	}
	filelogOffsetSynchImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(filelogOffsetSynchImagePullPolicyEnvVarName)

	nodeIp, isSet := os.LookupEnv(k8sNodeIpEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sNodeIpEnvVarName)
	}
	nodeName, isSet := os.LookupEnv(k8sNodeNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sNodeNameEnvVarName)
	}
	podName, isSet := os.LookupEnv(k8sPodNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sPodNameEnvVarName)
	}
	podIp, isSet := os.LookupEnv(k8sPodIpEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, k8sPodIpEnvVarName)
	}

	debugVerbosityDetailedRaw, isSet := os.LookupEnv(debugVerbosityDetailedEnvVarName)
	debugVerbosityDetailed := isSet && strings.ToLower(debugVerbosityDetailedRaw) == "true"

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
		secretRefResolverDeploymentName:      secretRefResolverDeploymentName,
		tokenUpdateServicePort:               tokenUpdateServicePort,
		oTelCollectorNamePrefix:              oTelCollectorNamePrefix,
		operatorImage:                        operatorImage,
		initContainerImage:                   initContainerImage,
		initContainerImagePullPolicy:         initContainerImagePullPolicy,
		collectorImage:                       collectorImage,
		collectorImagePullPolicy:             collectorImagePullPolicy,
		configurationReloaderImage:           configurationReloaderImage,
		configurationReloaderImagePullPolicy: configurationReloaderImagePullPolicy,
		filelogOffsetSynchImage:              filelogOffsetSynchImage,
		filelogOffsetSynchImagePullPolicy:    filelogOffsetSynchImagePullPolicy,
		nodeIp:                               nodeIp,
		nodeName:                             nodeName,
		podName:                              podName,
		podIp:                                podIp,
		sendBatchMaxSize:                     sendBatchMaxSize,
		debugVerbosityDetailed:               debugVerbosityDetailed,
	}

	return nil
}

func readOTelColExtraConfiguration() (*otelcolresources.OTelColExtraConfig, error) {
	oTelColExtraSpec, err := otelcolresources.ReadOTelColExtraConfiguration(oTelColExtraConfigFile)
	if err != nil {
		return nil, fmt.Errorf("Cannot read configuration file %s: %w", oTelColExtraConfigFile, err)
	}
	return oTelColExtraSpec, nil
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

func startDash0Controllers(
	ctx context.Context,
	mgr manager.Manager,
	clientset *kubernetes.Clientset,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	delegatingZapCore *zaputil.DelegatingZapCore,
	pseudoClusterUID string,
	developmentMode bool,
) error {
	oTelColExtraConfig, err := readOTelColExtraConfiguration()
	if err != nil {
		os.Exit(1)
	}

	oTelCollectorBaseUrl :=
		fmt.Sprintf(
			"http://%s-opentelemetry-collector-service.%s.svc.cluster.local:4318",
			envVars.oTelCollectorNamePrefix,
			envVars.operatorNamespace)
	images := util.Images{
		OperatorImage:                        envVars.operatorImage,
		InitContainerImage:                   envVars.initContainerImage,
		InitContainerImagePullPolicy:         envVars.initContainerImagePullPolicy,
		CollectorImage:                       envVars.collectorImage,
		CollectorImagePullPolicy:             envVars.collectorImagePullPolicy,
		ConfigurationReloaderImage:           envVars.configurationReloaderImage,
		ConfigurationReloaderImagePullPolicy: envVars.configurationReloaderImagePullPolicy,
		FilelogOffsetSynchImage:              envVars.filelogOffsetSynchImage,
		FilelogOffsetSynchImagePullPolicy:    envVars.filelogOffsetSynchImagePullPolicy,
	}
	isIPv6Cluster := strings.Count(envVars.podIp, ":") >= 2

	operatorConfigurationResource := executeStartupTasks(
		ctx,
		clientset,
		mgr.GetEventRecorderFor("dash0-startup-tasks"),
		operatorConfigurationValues,
		images,
		oTelCollectorBaseUrl,
		isIPv6Cluster,
		&setupLog,
	)

	k8sClient := mgr.GetClient()
	instrumenter := &instrumentation.Instrumenter{
		Client:               k8sClient,
		Clientset:            clientset,
		Recorder:             mgr.GetEventRecorderFor("dash0-monitoring-controller"),
		Images:               images,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
		IsIPv6Cluster:        isIPv6Cluster,
	}
	oTelColResourceManager := otelcolresources.NewOTelColResourceManager(
		k8sClient,
		mgr.GetScheme(),
		operatorDeploymentSelfReference,
		envVars.oTelCollectorNamePrefix,
		oTelColExtraConfig,
		envVars.sendBatchMaxSize,
		envVars.nodeIp,
		envVars.nodeName,
		isIPv6Cluster,
		isDocker,
		developmentMode,
		envVars.debugVerbosityDetailed,
	)
	backendConnectionManager := &backendconnection.BackendConnectionManager{
		Client:                 k8sClient,
		Clientset:              clientset,
		OTelColResourceManager: oTelColResourceManager,
	}
	backendConnectionReconciler := &backendconnection.BackendConnectionReconciler{
		Client:                   k8sClient,
		BackendConnectionManager: backendConnectionManager,
		Images:                   images,
		OperatorNamespace:        envVars.operatorNamespace,
		OTelCollectorNamePrefix:  envVars.oTelCollectorNamePrefix,
	}
	if err := backendConnectionReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the backend connection reconciler: %w", err)
	}

	thirdPartyResourceSynchronizationQueue =
		workqueue.NewTypedWithConfig(
			workqueue.TypedQueueConfig[controller.ThirdPartyResourceSyncJob]{
				Name: "dash0-third-party-resource-reconcile-queue",
			})
	persesDashboardCrdReconciler := &controller.PersesDashboardCrdReconciler{
		Client: k8sClient,
		Queue:  thirdPartyResourceSynchronizationQueue,
	}
	if err := persesDashboardCrdReconciler.SetupWithManager(ctx, mgr, startupTasksK8sClient, &setupLog); err != nil {
		return fmt.Errorf("unable to set up the Perses dashboard reconciler: %w", err)
	}
	prometheusRuleCrdReconciler := &controller.PrometheusRuleCrdReconciler{
		Client: k8sClient,
		Queue:  thirdPartyResourceSynchronizationQueue,
	}
	if err := prometheusRuleCrdReconciler.SetupWithManager(ctx, mgr, startupTasksK8sClient, &setupLog); err != nil {
		return fmt.Errorf("unable to set up the Prometheus rule reconciler: %w", err)
	}
	controller.StartProcessingThirdPartySynchronizationQueue(thirdPartyResourceSynchronizationQueue, &setupLog)

	oTelSdkStarter = selfmonitoringapiaccess.NewOTelSdkStarter(delegatingZapCore)

	operatorConfigurationReconciler := &controller.OperatorConfigurationReconciler{
		Client:    k8sClient,
		Clientset: clientset,
		ApiClients: []controller.ApiClient{
			persesDashboardCrdReconciler,
			prometheusRuleCrdReconciler,
		},
		Scheme:                          mgr.GetScheme(),
		Recorder:                        mgr.GetEventRecorderFor("dash0-operator-configuration-controller"),
		BackendConnectionManager:        backendConnectionManager,
		PseudoClusterUID:                pseudoClusterUID,
		OperatorDeploymentNamespace:     operatorDeploymentSelfReference.Namespace,
		OperatorDeploymentUID:           operatorDeploymentSelfReference.UID,
		OperatorDeploymentName:          operatorDeploymentSelfReference.Name,
		OperatorManagerPodName:          envVars.podName,
		OTelSdkStarter:                  oTelSdkStarter,
		Images:                          images,
		OperatorNamespace:               envVars.operatorNamespace,
		SecretRefResolverDeploymentName: envVars.secretRefResolverDeploymentName,
		DevelopmentMode:                 developmentMode,
	}
	if err := operatorConfigurationReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the operator configuration reconciler: %w", err)
	}

	monitoringReconciler := &controller.MonitoringReconciler{
		Client:                   k8sClient,
		Clientset:                clientset,
		Instrumenter:             instrumenter,
		BackendConnectionManager: backendConnectionManager,
		Images:                   images,
		OperatorNamespace:        envVars.operatorNamespace,
	}
	if err := monitoringReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the monitoring reconciler: %w", err)
	}

	if err := (&webhooks.InstrumentationWebhookHandler{
		Client:               k8sClient,
		Recorder:             mgr.GetEventRecorderFor("dash0-instrumentation-webhook"),
		Images:               images,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
		IsIPv6Cluster:        isIPv6Cluster,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the instrumentation webhook: %w", err)
	}

	if err := (&webhooks.OperatorConfigurationValidationWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the operator configuration validation webhook: %w", err)
	}
	if err := (&webhooks.MonitoringMutatingWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring mutating webhook: %w", err)
	}
	if err := (&webhooks.MonitoringValidationWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring validation webhook: %w", err)
	}
	tokenUpdateService = selfmonitoringapiaccess.NewTokenUpdateService(
		envVars.tokenUpdateServicePort,
		[]selfmonitoringapiaccess.AuthTokenClient{
			oTelSdkStarter,
			persesDashboardCrdReconciler,
			prometheusRuleCrdReconciler,
		})
	tokenUpdateService.Start(&setupLog)

	oTelSdkStarter.WaitForOTelConfig(
		[]selfmonitoringapiaccess.SelfMonitoringMetricsClient{
			operatorConfigurationReconciler,
			monitoringReconciler,
			persesDashboardCrdReconciler,
			prometheusRuleCrdReconciler,
		},
	)

	triggerSecretRefExchangeAndStartSelfMonitoringIfPossible(
		ctx,
		oTelSdkStarter,
		operatorConfigurationResource,
		pseudoClusterUID,
		images.GetOperatorVersion(),
		developmentMode,
	)

	return nil
}

func initStartupTasksK8sClient(logger *logr.Logger) error {
	cfg := ctrl.GetConfigOrDie()
	var err error
	if startupTasksK8sClient, err = client.New(cfg, client.Options{
		Scheme: scheme,
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

func readPseudoClusterUid(ctx context.Context, k8sClient client.Client, logger *logr.Logger) string {
	kubeSystemNamespace := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: "kube-system"}, kubeSystemNamespace); err != nil {
		msg := "unable to get the kube-system namespace uid"
		logger.Error(err, msg)
		return "unknown"
	}
	return string(kubeSystemNamespace.UID)
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
	clientset *kubernetes.Clientset,
	eventRecorder record.EventRecorder,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	images util.Images,
	oTelCollectorBaseUrl string,
	isIPv6Cluster bool,
	logger *logr.Logger,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	operatorConfigurationResource := createOrUpdateAutoOperatorConfigurationResource(
		ctx,
		startupTasksK8sClient,
		operatorConfigurationValues,
		logger,
	)
	instrumentAtStartup(
		ctx,
		startupTasksK8sClient,
		clientset,
		eventRecorder,
		images,
		oTelCollectorBaseUrl,
		isIPv6Cluster,
	)
	return operatorConfigurationResource
}

func instrumentAtStartup(
	ctx context.Context,
	startupTasksK8sClient client.Client,
	clientset *kubernetes.Clientset,
	eventRecorder record.EventRecorder,
	images util.Images,
	oTelCollectorBaseUrl string,
	isIPv6Cluster bool,
) {
	startupInstrumenter := &instrumentation.Instrumenter{
		Client:               startupTasksK8sClient,
		Clientset:            clientset,
		Recorder:             eventRecorder,
		Images:               images,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
		IsIPv6Cluster:        isIPv6Cluster,
	}

	// Trigger an unconditional apply/update of instrumentation for all workloads in Dash0-enabled namespaces, according
	// to the respective settings of the Dash0 monitoring resource in the namespace. See godoc comment on
	// Instrumenter#InstrumentAtStartup.
	startupInstrumenter.InstrumentAtStartup(ctx, startupTasksK8sClient, &setupLog)
}

func createOrUpdateAutoOperatorConfigurationResource(
	ctx context.Context,
	k8sClient client.Client,
	operatorConfigurationValues *startup.OperatorConfigurationValues,
	logger *logr.Logger,
) *dash0v1alpha1.Dash0OperatorConfiguration {
	if operatorConfigurationValues == nil {
		return nil
	}
	handler := startup.AutoOperatorConfigurationResourceHandler{
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
		if err := selfmonitoringapiaccess.ExchangeSecretRefForTokenIfNecessary(
			ctx,
			startupTasksK8sClient,
			envVars.operatorNamespace,
			envVars.secretRefResolverDeploymentName,
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
		envVars.podName,
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
