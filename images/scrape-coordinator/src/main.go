// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// The scrape coordinator discovers the scrape targets of Prometheus custom resources and assigns them to collectors. It
// replaces the OpenTelemetry target allocator and serves the same endpoints to the Prometheus receiver.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	monitoringclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	"github.com/prometheus/client_golang/prometheus"
	promdiscovery "github.com/prometheus/prometheus/discovery"
	_ "github.com/prometheus/prometheus/discovery/install"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"

	"github.com/dash0hq/dash0-operator/images/pkg/common"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/allocation"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/collectors"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/config"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/scrapeconfigs"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/server"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/targets"
)

const containerName = "scrape-coordinator"

func main() {
	os.Exit(runCoordinator())
}

func runCoordinator() int {
	configPath := flag.String("config", config.DefaultConfigFilePath, "path to the configuration file")
	debugEndpoints := flag.Bool("debug-endpoints", false, "serve the assignments of targets to collectors on "+
		"/debug/assignments")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn or error")
	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		slog.Error("invalid log level", "level", *logLevel, "error", err)
		return 1
	}

	ctx := context.Background()
	meter := common.InitOTelSdkFromEnvVars(ctx, allocation.MeterName, containerName)
	// Shutting down with the base context, the signal context below is cancelled when the termination signal arrives,
	// and a cancelled context would keep the OTel SDK from flushing.
	defer common.ShutDownOTelSdk(ctx)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	common.StartPprofServerIfConfigured(logger)

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("cannot load the configuration", "error", err)
		return 1
	}
	logger.Info("starting the scrape coordinator", "collectorNamespace", cfg.CollectorNamespace,
		"namespaces", cfg.PrometheusCRs.Namespaces, "secretNamespaces", cfg.SecretNamespacesOrDefault(),
		"tls", cfg.Server.TLS != nil)

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("cannot create the Kubernetes client configuration", "error", err)
		return 1
	}
	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		logger.Error("cannot create the Kubernetes client", "error", err)
		return 1
	}
	monitoringClient, err := monitoringclient.NewForConfig(restConfig)
	if err != nil {
		logger.Error("cannot create the monitoring client", "error", err)
		return 1
	}
	metadataClient, err := metadata.NewForConfig(restConfig)
	if err != nil {
		logger.Error("cannot create the metadata client", "error", err)
		return 1
	}

	allocator := allocation.New(logger, meter, []string{allocation.PoolPerNode})

	srv, err := server.New(logger, allocator, server.Options{
		ListenAddr:               cfg.Server.ListenAddr,
		TLS:                      cfg.Server.TLS,
		AllowInsecureAuthSecrets: cfg.Server.AllowInsecureAuthSecrets,
		DebugEndpoints:           *debugEndpoints,
	})
	if err != nil {
		logger.Error("cannot create the server", "error", err)
		return 1
	}
	generator, err := scrapeconfigs.New(
		logger, k8sClient, monitoringClient, metadataClient, cfg, scrapeconfigs.DefaultUpdateInterval)
	if err != nil {
		logger.Error("cannot create the scrape config generator", "error", err)
		return 1
	}
	targetBuilder := targets.NewBuilder(logger, targets.DefaultUpdateInterval)
	collectorWatcher := collectors.NewWatcher(
		logger,
		k8sClient,
		cfg.CollectorNamespace,
		[]collectors.Pool{{Name: allocation.PoolPerNode, Selector: cfg.Pools.PerNode.Selector}},
		cfg.CollectorNotReadyGracePeriod.Duration,
		collectors.DefaultUpdateInterval,
	)

	// A component that fails cancels the context of the group, which stops all others.
	group, groupCtx := errgroup.WithContext(signalCtx)

	// The service discovery metrics are required by the discovery manager, but not exported.
	sdRegistry := prometheus.NewRegistry()
	sdMetrics, err := promdiscovery.CreateAndRegisterSDMetrics(sdRegistry)
	if err != nil {
		logger.Error("cannot register the service discovery metrics", "error", err)
		return 1
	}
	discoveryManager := promdiscovery.NewManager(
		groupCtx, logger.With("component", "discovery-manager"), sdRegistry, sdMetrics)
	if discoveryManager == nil {
		logger.Error("cannot create the discovery manager")
		return 1
	}

	group.Go(func() error {
		if err := discoveryManager.Run(); !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})
	group.Go(func() error {
		targetBuilder.Run(groupCtx, discoveryManager.SyncCh(), allocator.SetTargets)
		return nil
	})
	group.Go(func() error { return collectorWatcher.Run(groupCtx, allocator.SetCollectors) })
	group.Go(func() error { return srv.Run(groupCtx) })
	group.Go(func() error {
		return generator.Run(groupCtx, func(result *scrapeconfigs.ScrapeConfigs) {
			applyScrapeConfigs(logger, result, srv, targetBuilder, discoveryManager)
		})
	})

	err = group.Wait()
	if signalCtx.Err() != nil {
		logger.Info("received a termination signal")
	}
	if err != nil {
		logger.Error("the scrape coordinator stopped with an error", "error", err)
		return 1
	}
	logger.Info("the scrape coordinator has stopped")
	return 0
}

// applyScrapeConfigs serves the scrape configs and updates the service discovery and the relabeling of the targets.
// It runs in the generation goroutine of the scrape config generator, the only goroutine that marshals Prometheus
// configs (see server.SetScrapeConfigs).
func applyScrapeConfigs(
	logger *slog.Logger,
	result *scrapeconfigs.ScrapeConfigs,
	srv *server.Server,
	targetBuilder *targets.Builder,
	discoveryManager *promdiscovery.Manager,
) {
	if err := srv.SetScrapeConfigs(result.Served); err != nil {
		logger.Error("cannot serve the scrape configs", "error", err)
		return
	}
	targetBuilder.SetScrapeConfigs(result.Discovery)
	serviceDiscoveryConfigs := make(map[string]promdiscovery.Configs, len(result.Discovery))
	for _, scrapeConfig := range result.Discovery {
		serviceDiscoveryConfigs[scrapeConfig.JobName] = scrapeConfig.ServiceDiscoveryConfigs
	}
	if err := discoveryManager.ApplyConfig(serviceDiscoveryConfigs); err != nil {
		logger.Error("cannot apply the scrape configs to the service discovery", "error", err)
		return
	}
	logger.Info("the scrape configs have been updated", "jobs", len(result.Served))
}
