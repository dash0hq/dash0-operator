// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	k8swebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/backendconnection"
	"github.com/dash0hq/dash0-operator/internal/backendconnection/otelcolresources"
	dash0controller "github.com/dash0hq/dash0-operator/internal/dash0/controller"
	dash0removal "github.com/dash0hq/dash0-operator/internal/dash0/removal"
	dash0util "github.com/dash0hq/dash0-operator/internal/dash0/util"
	dash0webhook "github.com/dash0hq/dash0-operator/internal/dash0/webhook"
	//+kubebuilder:scaffold:imports
)

type environmentVariables struct {
	operatorNamespace                    string
	oTelCollectorNamePrefix              string
	operatorImage                        string
	initContainerImage                   string
	initContainerImagePullPolicy         corev1.PullPolicy
	collectorImage                       string
	collectorImagePullPolicy             corev1.PullPolicy
	configurationReloaderImage           string
	configurationReloaderImagePullPolicy corev1.PullPolicy
}

const (
	operatorNamespaceEnvVarName                         = "DASH0_OPERATOR_NAMESPACE"
	oTelCollectorNamePrefixEnvVarName                   = "OTEL_COLLECTOR_NAME_PREFIX"
	operatorImageEnvVarName                             = "DASH0_OPERATOR_IMAGE"
	initContainerImageEnvVarName                        = "DASH0_INIT_CONTAINER_IMAGE"
	initContainerImagePullPolicyEnvVarName              = "DASH0_INIT_CONTAINER_IMAGE_PULL_POLICY"
	collectorImageEnvVarName                            = "DASH0_COLLECTOR_IMAGE"
	collectorImageImagePullPolicyEnvVarName             = "DASH0_COLLECTOR_IMAGE_PULL_POLICY"
	configurationReloaderImageEnvVarName                = "DASH0_CONFIGURATION_RELOADER_IMAGE"
	configurationReloaderImageImagePullPolicyEnvVarName = "DASH0_CONFIGURATION_RELOADER_IMAGE_PULL_POLICY"

	developmentModeEnvVarName = "DASH0_DEVELOPMENT_MODE"

	//nolint
	mandatoryEnvVarMissingMessageTemplate = "cannot start the Dash0 operator, the mandatory environment variable \"%s\" is missing"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(dash0v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var uninstrumentAll bool
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	flag.BoolVar(&uninstrumentAll, "uninstrument-all", false,
		"If set, the process will remove all Dash0 monitoring resources from all namespaces in the cluster. This "+
			"will trigger the Dash0 monitoring resources' finalizers in each namespace, which in turn will revert the "+
			"instrumentation of all workloads in all namespaces.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

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

	if uninstrumentAll {
		if err := deleteDash0MonitoringResourcesInAllNamespaces(&setupLog); err != nil {
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

	if err := startOperatorManager(
		metricsAddr,
		secureMetrics,
		tlsOpts,
		webhookServer,
		probeAddr,
		enableLeaderElection,
	); err != nil {
		setupLog.Error(err, "The Dash0 operator manager process failed to start.")
		os.Exit(1)
	}
}

func startOperatorManager(
	metricsAddr string,
	secureMetrics bool,
	tlsOpts []func(*tls.Config),
	webhookServer k8swebhook.Server,
	probeAddr string,
	enableLeaderElection bool,
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

	envVars, err := readEnvironmentVariables()
	if err != nil {
		return err
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

		"otel collector name prefix",
		envVars.oTelCollectorNamePrefix,
	)

	err = startDash0Controller(mgr, clientset, envVars)
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

	return nil
}

func startDash0Controller(mgr manager.Manager, clientset *kubernetes.Clientset, envVars *environmentVariables) error {
	oTelCollectorBaseUrl :=
		fmt.Sprintf(
			"http://%s-opentelemetry-collector.%s.svc.cluster.local:4318",
			envVars.oTelCollectorNamePrefix,
			envVars.operatorNamespace)

	images := dash0util.Images{
		OperatorImage:                        envVars.operatorImage,
		InitContainerImage:                   envVars.initContainerImage,
		InitContainerImagePullPolicy:         envVars.initContainerImagePullPolicy,
		CollectorImage:                       envVars.collectorImage,
		CollectorImagePullPolicy:             envVars.collectorImagePullPolicy,
		ConfigurationReloaderImage:           envVars.configurationReloaderImage,
		ConfigurationReloaderImagePullPolicy: envVars.configurationReloaderImagePullPolicy,
	}

	oTelColResourceManager := &otelcolresources.OTelColResourceManager{
		Client:                  mgr.GetClient(),
		OTelCollectorNamePrefix: envVars.oTelCollectorNamePrefix,
	}
	backendConnectionManager := &backendconnection.BackendConnectionManager{
		Client:                 mgr.GetClient(),
		Clientset:              clientset,
		Scheme:                 mgr.GetScheme(),
		OTelColResourceManager: oTelColResourceManager,
	}

	dash0Reconciler := &dash0controller.Dash0Reconciler{
		Client:                   mgr.GetClient(),
		Clientset:                clientset,
		Scheme:                   mgr.GetScheme(),
		Recorder:                 mgr.GetEventRecorderFor("dash0-controller"),
		Images:                   images,
		OTelCollectorNamePrefix:  envVars.oTelCollectorNamePrefix,
		OTelCollectorBaseUrl:     oTelCollectorBaseUrl,
		OperatorNamespace:        envVars.operatorNamespace,
		BackendConnectionManager: backendConnectionManager,
	}
	if err := dash0Reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to set up the Dash0 reconciler: %w", err)
	}
	setupLog.Info("Dash0 reconciler has been set up.")

	if os.Getenv("ENABLE_WEBHOOK") != "false" {
		if err := (&dash0webhook.Handler{
			Client:               mgr.GetClient(),
			Recorder:             mgr.GetEventRecorderFor("dash0-webhook"),
			Images:               images,
			OTelCollectorBaseUrl: oTelCollectorBaseUrl,
		}).SetupWebhookWithManager(mgr); err != nil {
			return fmt.Errorf("unable to create the Dash0 webhook: %w", err)
		}
		setupLog.Info("Dash0 webhook has been set up.")
	} else {
		setupLog.Info("Dash0 webhooks have been disabled via configuration.")
	}

	go func() {
		time.Sleep(10 * time.Second)

		// trigger an unconditional apply/update of instrumentation for all workloads, see godoc comment on
		// Dash0Reconciler#InstrumentAtStartup
		dash0Reconciler.InstrumentAtStartup()
	}()

	return nil
}

func readEnvironmentVariables() (*environmentVariables, error) {
	operatorNamespace, isSet := os.LookupEnv(operatorNamespaceEnvVarName)
	if !isSet {
		return nil, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, operatorNamespaceEnvVarName)
	}

	oTelCollectorNamePrefix, isSet := os.LookupEnv(oTelCollectorNamePrefixEnvVarName)
	if !isSet {
		return nil, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, oTelCollectorNamePrefixEnvVarName)
	}

	operatorImage, isSet := os.LookupEnv(operatorImageEnvVarName)
	if !isSet {
		return nil, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, operatorImageEnvVarName)
	}

	initContainerImage, isSet := os.LookupEnv(initContainerImageEnvVarName)
	if !isSet {
		return nil, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, initContainerImageEnvVarName)
	}
	initContainerImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(initContainerImagePullPolicyEnvVarName)

	collectorImage, isSet := os.LookupEnv(collectorImageEnvVarName)
	if !isSet {
		return nil, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, collectorImageEnvVarName)
	}
	collectorImagePullPolicy := readOptionalPullPolicyFromEnvironmentVariable(collectorImageImagePullPolicyEnvVarName)

	configurationReloaderImage, isSet := os.LookupEnv(configurationReloaderImageEnvVarName)
	if !isSet {
		return nil, fmt.Errorf(mandatoryEnvVarMissingMessageTemplate, configurationReloaderImageEnvVarName)
	}
	configurationReloaderImagePullPolicy :=
		readOptionalPullPolicyFromEnvironmentVariable(configurationReloaderImageImagePullPolicyEnvVarName)

	return &environmentVariables{
		operatorNamespace:                    operatorNamespace,
		oTelCollectorNamePrefix:              oTelCollectorNamePrefix,
		operatorImage:                        operatorImage,
		initContainerImage:                   initContainerImage,
		initContainerImagePullPolicy:         initContainerImagePullPolicy,
		collectorImage:                       collectorImage,
		collectorImagePullPolicy:             collectorImagePullPolicy,
		configurationReloaderImage:           configurationReloaderImage,
		configurationReloaderImagePullPolicy: configurationReloaderImagePullPolicy,
	}, nil
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

func deleteDash0MonitoringResourcesInAllNamespaces(logger *logr.Logger) error {
	handler, err := dash0removal.NewOperatorPreDeleteHandler()
	if err != nil {
		logger.Error(err, "Failed to create the OperatorPreDeleteHandler.")
		return err
	}
	err = handler.DeleteAllDash0MonitoringResources()
	if err != nil {
		logger.Error(err, "Failed to delete all Dash0 monitoring resources.")
		return err
	}
	return nil
}
