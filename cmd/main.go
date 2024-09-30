// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	semconv "go.opentelemetry.io/collector/semconv/v1.27.0"
	otelmetric "go.opentelemetry.io/otel/metric"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/dash0/controller"
	"github.com/dash0hq/dash0-operator/internal/dash0/instrumentation"
	"github.com/dash0hq/dash0-operator/internal/dash0/predelete"
	"github.com/dash0hq/dash0-operator/internal/dash0/selfmonitoring"
	"github.com/dash0hq/dash0-operator/internal/dash0/startup"
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
	"github.com/dash0hq/dash0-operator/internal/dash0/webhooks"
	//+kubebuilder:scaffold:imports
)

type environmentVariables struct {
	operatorNamespace                    string
	deploymentName                       string
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
}

const (
	operatorNamespaceEnvVarName                    = "DASH0_OPERATOR_NAMESPACE"
	deploymentNameEnvVarName                       = "DASH0_DEPLOYMENT_NAME"
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

	developmentModeEnvVarName = "DASH0_DEVELOPMENT_MODE"

	oTelColResourceSpecConfigFile = "/etc/config/otelcolresources.yaml"

	//nolint
	mandatoryEnvVarMissingMessageTemplate = "cannot start the Dash0 operator, the mandatory environment variable \"%s\" is missing"

	meterName = "dash0.operator.manager"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	startupTasksK8sClient   client.Client
	deploymentSelfReference *appsv1.Deployment
	envVars                 environmentVariables

	metricNamePrefix = fmt.Sprintf("%s.", meterName)
	meter            otelmetric.Meter
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(dash0v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	ctx := context.Background()
	var operatorConfigurationEndpoint string
	var operatorConfigurationToken string
	var operatorConfigurationSecretRefName string
	var operatorConfigurationSecretRefKey string
	var isUninstrumentAll bool
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	flag.BoolVar(&isUninstrumentAll, "uninstrument-all", false,
		"If set, the process will remove all Dash0 monitoring resources from all namespaces in the cluste, then "+
			"exit. This will trigger the Dash0 monitoring resources' finalizers in each namespace, which in turn will "+
			"revert the instrumentation of all workloads in all namespaces.")
	flag.StringVar(&operatorConfigurationEndpoint, "operator-configuration-endpoint", "",
		"The Dash0 endpoint gRPC URL for creating an operator configuration resource.")
	flag.StringVar(&operatorConfigurationToken, "operator-configuration-token", "",
		"The Dash0 auth token for creating an operator configuration resource.")
	flag.StringVar(&operatorConfigurationSecretRefName, "operator-configuration-secret-ref-name", "",
		"The name of an existing Kubernetes secret containing the Dash0 auth token, used to creating an operator "+
			"configuration resource.")
	flag.StringVar(&operatorConfigurationSecretRefKey, "operator-configuration-secret-ref-key", "",
		"The key in an existing Kubernetes secret containing the Dash0 auth token, used to creating an operator "+
			"configuration resource.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers.")

	var developmentMode bool
	developmentModeRaw, isSet := os.LookupEnv(developmentModeEnvVarName)
	developmentMode = isSet && strings.ToLower(developmentModeRaw) == "true"
	var opts zap.Options
	if developmentMode {
		opts = zap.Options{
			Development: true,
		}
	} else {
		opts = zap.Options{
			Development: false,
		}
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
	if err = readEnvironmentVariables(); err != nil {
		os.Exit(1)
	}
	if err = initStartupTasksK8sClient(&setupLog); err != nil {
		os.Exit(1)
	}
	if err = findDeploymentSelfReference(
		ctx,
		startupTasksK8sClient,
		envVars.operatorNamespace,
		envVars.deploymentName,
		&setupLog,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager process to lookup its own deployment.")
		os.Exit(1)
	}

	meter =
		common.InitOTelSdk(
			ctx,
			meterName,
			map[string]string{semconv.AttributeK8SDeploymentUID: string(deploymentSelfReference.UID)},
		)

	var operatorConfiguration *startup.OperatorConfigurationValues
	if len(operatorConfigurationEndpoint) > 0 {
		operatorConfiguration = &startup.OperatorConfigurationValues{
			Endpoint: operatorConfigurationEndpoint,
			Token:    operatorConfigurationToken,
			SecretRef: startup.SecretRef{
				Name: operatorConfigurationSecretRefName,
				Key:  operatorConfigurationSecretRefKey,
			},
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
		operatorConfiguration,
		developmentMode,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager process failed to start.")
		os.Exit(1)
	}
}

func startOperatorManager(
	ctx context.Context,
	metricsAddr string,
	secureMetrics bool,
	tlsOpts []func(*tls.Config),
	webhookServer k8swebhook.Server,
	probeAddr string,
	enableLeaderElection bool,
	operatorConfiguration *startup.OperatorConfigurationValues,
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
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to create the manager: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create the clientset client")
	}

	setupLog.Info(
		"configuration:",

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
		"deploymentName",
		envVars.deploymentName,
		"otel collector name prefix",
		envVars.oTelCollectorNamePrefix,

		"development mode",
		developmentMode,
	)

	err = startDash0Controllers(ctx, mgr, clientset, operatorConfiguration, developmentMode)
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

	setupLog.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("unable to set up the signal handler: %w", err)
	}
	// ^mgr.Start(...) blocks. It only returns when the manager is terminating.

	common.ShutDownOTelSdk(ctx)

	return nil
}

func readEnvironmentVariables() error {
	operatorNamespace, isSet := os.LookupEnv(operatorNamespaceEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, operatorNamespaceEnvVarName)
	}

	deploymentName, isSet := os.LookupEnv(deploymentNameEnvVarName)
	if !isSet {
		return fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, deploymentNameEnvVarName)
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

	envVars = environmentVariables{
		operatorNamespace:                    operatorNamespace,
		deploymentName:                       deploymentName,
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
	}

	return nil
}

func readConfiguration() (*otelcolresources.OTelColResourceSpecs, error) {
	oTelColResourceSpec, err := otelcolresources.ReadOTelColResourcesConfiguration(oTelColResourceSpecConfigFile)
	if err != nil {
		return nil, fmt.Errorf("Cannot read configuration file %s: %w", oTelColResourceSpecConfigFile, err)
	}
	return oTelColResourceSpec, nil
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
	operatorConfiguration *startup.OperatorConfigurationValues,
	developmentMode bool,
) error {
	oTelColResourceSpecs, err := readConfiguration()
	if err != nil {
		os.Exit(1)
	}

	oTelCollectorBaseUrl :=
		fmt.Sprintf(
			"http://%s-opentelemetry-collector.%s.svc.cluster.local:4318",
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

	executeStartupTasks(
		ctx,
		clientset,
		mgr.GetEventRecorderFor("dash0-startup-tasks"),
		operatorConfiguration,
		images,
		oTelCollectorBaseUrl,
		&setupLog,
	)

	logCurrentSelfMonitoringSettings(deploymentSelfReference)

	k8sClient := mgr.GetClient()
	instrumenter := &instrumentation.Instrumenter{
		Client:               k8sClient,
		Clientset:            clientset,
		Recorder:             mgr.GetEventRecorderFor("dash0-monitoring-controller"),
		Images:               images,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
	}
	oTelColResourceManager := &otelcolresources.OTelColResourceManager{
		Client:                  k8sClient,
		Scheme:                  mgr.GetScheme(),
		DeploymentSelfReference: deploymentSelfReference,
		OTelCollectorNamePrefix: envVars.oTelCollectorNamePrefix,
		OTelColResourceSpecs:    oTelColResourceSpecs,
		DevelopmentMode:         developmentMode,
	}
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

	operatorConfigurationReconciler := &controller.OperatorConfigurationReconciler{
		Client:                  k8sClient,
		Clientset:               clientset,
		Scheme:                  mgr.GetScheme(),
		Recorder:                mgr.GetEventRecorderFor("dash0-operator-configuration-controller"),
		DeploymentSelfReference: deploymentSelfReference,
		Images:                  images,
		DevelopmentMode:         developmentMode,
	}
	if err := operatorConfigurationReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the operator configuration reconciler: %w", err)
	}
	operatorConfigurationReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		&setupLog,
	)

	monitoringReconciler := &controller.Dash0Reconciler{
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
	monitoringReconciler.InitializeSelfMonitoringMetrics(
		meter,
		metricNamePrefix,
		&setupLog,
	)

	if err := (&webhooks.InstrumentationWebhookHandler{
		Client:               k8sClient,
		Recorder:             mgr.GetEventRecorderFor("dash0-instrumentation-webhook"),
		Images:               images,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the instrumentation webhook: %w", err)
	}

	if err := (&webhooks.OperatorConfigurationValidationWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the operator configuration validation webhook: %w", err)
	}
	if err := (&webhooks.MonitoringValidationWebhookHandler{
		Client: k8sClient,
	}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create the monitoring validation webhook: %w", err)
	}

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

func findDeploymentSelfReference(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	deploymentName string,
	logger *logr.Logger,
) error {
	deploymentSelfReference = &appsv1.Deployment{}
	fullyQualifiedName := fmt.Sprintf("%s/%s", operatorNamespace, deploymentName)
	if err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: operatorNamespace,
		Name:      deploymentName,
	}, deploymentSelfReference); err != nil {
		logger.Error(err, "failed to get self reference for controller deployment")
		return err
	}
	if deploymentSelfReference.UID == "" {
		msg := fmt.Sprintf("self reference for controller deployment %s has no UID", fullyQualifiedName)
		err := fmt.Errorf(msg)
		logger.Error(err, msg)
		return err
	}
	return nil
}

func executeStartupTasks(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	eventRecorder record.EventRecorder,
	operatorConfiguration *startup.OperatorConfigurationValues,
	images util.Images,
	oTelCollectorBaseUrl string,
	logger *logr.Logger,
) {
	createOperatorConfiguration(
		ctx,
		startupTasksK8sClient,
		operatorConfiguration,
		logger,
	)
	instrumentAtStartup(
		ctx,
		startupTasksK8sClient,
		clientset,
		eventRecorder,
		images,
		oTelCollectorBaseUrl,
	)
}

func instrumentAtStartup(
	ctx context.Context,
	startupTasksK8sClient client.Client,
	clientset *kubernetes.Clientset,
	eventRecorder record.EventRecorder,
	images util.Images,
	oTelCollectorBaseUrl string,
) {
	startupInstrumenter := &instrumentation.Instrumenter{
		Client:               startupTasksK8sClient,
		Clientset:            clientset,
		Recorder:             eventRecorder,
		Images:               images,
		OTelCollectorBaseUrl: oTelCollectorBaseUrl,
	}

	// Trigger an unconditional apply/update of instrumentation for all workloads in Dash0-enabled namespaces, according
	// to the respective settings of the Dash0 monitoring resource in the namespace. See godoc comment on
	// Instrumenter#InstrumentAtStartup.
	startupInstrumenter.InstrumentAtStartup(ctx, startupTasksK8sClient, &setupLog)
}

func logCurrentSelfMonitoringSettings(deploymentSelfReference *appsv1.Deployment) {
	selfMonitoringConfiguration, err :=
		selfmonitoring.GetSelfMonitoringConfigurationFromControllerDeployment(
			deploymentSelfReference,
			controller.ManagerContainerName,
		)
	if err != nil {
		setupLog.Error(err, "cannot determine whether self-monitoring is enabled in the controller deployment")
	}

	if selfMonitoringConfiguration.Enabled {
		endpointAndHeaders := selfmonitoring.ConvertExportConfigurationToEnvVarSettings(selfMonitoringConfiguration.Export)
		setupLog.Info(
			"Self-monitoring settings on controller deployment:",
			"enabled",
			selfMonitoringConfiguration.Enabled,
			"endpoint",
			endpointAndHeaders.Endpoint,
		)
	} else {
		setupLog.Info(
			"Self-monitoring settings on controller deployment:",
			"enabled",
			selfMonitoringConfiguration.Enabled,
		)
	}
}

func createOperatorConfiguration(
	ctx context.Context,
	k8sClient client.Client,
	operatorConfiguration *startup.OperatorConfigurationValues,
	logger *logr.Logger,
) {
	if operatorConfiguration != nil {
		handler := startup.AutoOperatorConfigurationResourceHandler{
			Client:            k8sClient,
			OperatorNamespace: envVars.operatorNamespace,
			NamePrefix:        envVars.oTelCollectorNamePrefix,
		}
		if err := handler.CreateOperatorConfigurationResource(ctx, operatorConfiguration, logger); err != nil {
			logger.Error(err, "Failed to create the requested Dash0 operator configuration resource.")
		}
	}
}

func deleteMonitoringResourcesInAllNamespaces(logger *logr.Logger) error {
	handler, err := predelete.NewOperatorPreDeleteHandler()
	if err != nil {
		logger.Error(err, "Failed to create the OperatorPreDeleteHandler.")
		return err
	}
	err = handler.DeleteAllDash0MonitoringResources()
	if err != nil {
		logger.Error(err, "Failed to delete all monitoring resources.")
		return err
	}
	return nil
}
