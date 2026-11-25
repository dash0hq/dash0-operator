// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// Package scrapeconfigs generates the Prometheus scrape configs from ServiceMonitors, PodMonitors and ScrapeConfigs
// with the Prometheus operator's config generator, and regenerates them whenever one of these resources or a secret
// they reference changes.
package scrapeconfigs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/prometheus-operator/prometheus-operator/pkg/assets"
	monitoringclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	"github.com/prometheus-operator/prometheus-operator/pkg/informers"
	"github.com/prometheus-operator/prometheus-operator/pkg/operator"
	"github.com/prometheus-operator/prometheus-operator/pkg/prometheus"
	prometheusclient "github.com/prometheus/client_golang/prometheus"
	promconfig "github.com/prometheus/prometheus/config"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/tools/cache"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/config"
	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/debounce"
)

const (
	// DefaultUpdateInterval is the minimum time between two generations of the scrape configs.
	DefaultUpdateInterval = 5 * time.Second

	resyncPeriod     = 5 * time.Minute
	cacheSyncTimeout = 15 * time.Second

	// shardPlaceholder is replaced by the collectors' Prometheus receiver with the value of the SHARD environment
	// variable, or 0 if it is not set.
	shardPlaceholder = "$(SHARD)"
	shardOfCollector = "0"
)

// scrapeProtocols are the scrape protocols of the generated scrape configs, in order of preference.
var scrapeProtocols = []monitoringv1.ScrapeProtocol{
	monitoringv1.OpenMetricsText1_0_0,
	monitoringv1.OpenMetricsText0_0_1,
	monitoringv1.PrometheusText1_0_0,
	monitoringv1.PrometheusText0_0_4,
}

// ScrapeConfigs is the result of a generation.
type ScrapeConfigs struct {
	// Served are the scrape configs as served to the collectors. They contain the $(SHARD) placeholder, which the
	// collectors replace.
	Served []*promconfig.ScrapeConfig
	// Discovery are the same scrape configs with the placeholder replaced the way the collectors replace it. They
	// drive the coordinator's own service discovery and relabeling.
	Discovery []*promconfig.ScrapeConfig
}

// Generator watches the Prometheus custom resources and generates the scrape configs.
type Generator struct {
	logger          *slog.Logger
	k8sClient       kubernetes.Interface
	libraryLogger   *slog.Logger
	prometheus      *monitoringv1.Prometheus
	configGenerator *prometheus.ConfigGenerator
	operatorMetrics *operator.Metrics
	eventRecorder   *operator.EventRecorder
	updateInterval  time.Duration

	namespaceInformer cache.SharedIndexInformer
	// resourceInformers holds the informers of the custom resource kinds whose CRD is installed, by resource name.
	resourceInformers map[string]*informers.ForResource
	secretInformers   *informers.ForResource
	secretNamespaces  []string

	// referencedSecrets are the secrets the last generation read. Only a change of one of them requires a new
	// generation.
	referencedSecrets atomic.Pointer[assets.RefTracker]
	// generating is set while a generation runs. Its references are only known at the end, so every secret event
	// during a generation triggers the next one.
	generating atomic.Bool
	// droppedJobs are the jobs the last generation dropped because they reference files, so that each one is only
	// logged once. Generate must not be called concurrently.
	droppedJobs map[string]struct{}
}

// New creates a Generator for the namespaces of the configuration.
func New(
	logger *slog.Logger,
	k8sClient kubernetes.Interface,
	monitoringClient monitoringclient.Interface,
	metadataClient metadata.Interface,
	cfg *config.Config,
	updateInterval time.Duration,
) (*Generator, error) {
	libraryLogger := slog.New(minLevelHandler{Handler: logger.Handler(), minLevel: slog.LevelWarn})
	prom := syntheticPrometheus(cfg)
	configGenerator, err := prometheus.NewConfigGenerator(
		libraryLogger, prom, prometheus.WithEndpointSliceSupport(), prometheus.WithInlineTLSConfig(),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create the config generator: %w", err)
	}

	namespaces := toSet(cfg.PrometheusCRs.Namespaces)
	if len(namespaces) == 0 {
		namespaces = toSet([]string{metav1.NamespaceAll})
	}
	noNamespaces := map[string]struct{}{}
	resourceInformers, err := newResourceInformers(
		logger,
		k8sClient,
		informers.NewMonitoringInformerFactories(namespaces, noNamespaces, monitoringClient, resyncPeriod, nil),
	)
	if err != nil {
		return nil, err
	}
	secretNamespaces := cfg.SecretNamespacesOrDefault()
	secretInformers, err := informers.NewInformersForResourceWithTransform(
		informers.NewMetadataInformerFactory(toSet(secretNamespaces), noNamespaces, metadataClient, resyncPeriod, nil),
		corev1.SchemeGroupVersion.WithResource(string(corev1.ResourceSecrets)),
		informers.PartialObjectMetadataStrip(operator.SecretGVK()),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create the secret informers: %w", err)
	}
	namespaceInformer := coreinformers.NewNamespaceInformer(k8sClient, resyncPeriod, cache.Indexers{})
	if err := namespaceInformer.SetTransform(keepNameAndLabels); err != nil {
		return nil, err
	}

	return &Generator{
		logger:            logger,
		k8sClient:         k8sClient,
		libraryLogger:     libraryLogger,
		prometheus:        prom,
		configGenerator:   configGenerator,
		operatorMetrics:   operator.NewMetrics(prometheusclient.NewRegistry()),
		eventRecorder:     operator.NewEventRecorderFactory(false)(k8sClient, "scrape-coordinator")(prom),
		updateInterval:    updateInterval,
		namespaceInformer: namespaceInformer,
		resourceInformers: resourceInformers,
		secretInformers:   secretInformers,
		secretNamespaces:  secretNamespaces,
		droppedJobs:       map[string]struct{}{},
	}, nil
}

// syntheticPrometheus returns the Prometheus object the scrape configs are generated for. It selects all
// ServiceMonitors, PodMonitors and ScrapeConfigs in all watched namespaces and no Probes, like the OpenTelemetry target
// allocator as deployed by the Dash0 operator.
func syntheticPrometheus(cfg *config.Config) *monitoringv1.Prometheus {
	serviceDiscoveryRole := monitoringv1.EndpointSliceRole
	scrapeInterval := monitoringv1.Duration(cfg.PrometheusCRs.ScrapeInterval.Duration.String())
	return &monitoringv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{Namespace: cfg.CollectorNamespace},
		Spec: monitoringv1.PrometheusSpec{
			CommonPrometheusFields: monitoringv1.CommonPrometheusFields{
				ScrapeInterval: scrapeInterval,
				// A nil namespace selector would only select resources in the namespace of the Prometheus object.
				ServiceMonitorNamespaceSelector: &metav1.LabelSelector{},
				PodMonitorNamespaceSelector:     &metav1.LabelSelector{},
				ScrapeConfigNamespaceSelector:   &metav1.LabelSelector{},
				ServiceMonitorSelector:          &metav1.LabelSelector{},
				PodMonitorSelector:              &metav1.LabelSelector{},
				ScrapeConfigSelector:            &metav1.LabelSelector{},
				ServiceDiscoveryRole:            &serviceDiscoveryRole,
				ScrapeProtocols:                 scrapeProtocols,
			},
			EvaluationInterval: scrapeInterval,
		},
	}
}

// newResourceInformers creates the informers for the custom resource kinds whose CRD is installed. A CRD that is
// installed later is only picked up after a restart.
func newResourceInformers(
	logger *slog.Logger,
	k8sClient kubernetes.Interface,
	factories informers.FactoriesForNamespaces,
) (map[string]*informers.ForResource, error) {
	result := map[string]*informers.ForResource{}
	for _, resource := range []schema.GroupVersionResource{
		monitoringv1.SchemeGroupVersion.WithResource(monitoringv1.ServiceMonitorName),
		monitoringv1.SchemeGroupVersion.WithResource(monitoringv1.PodMonitorName),
		monitoringv1alpha1.SchemeGroupVersion.WithResource(monitoringv1alpha1.ScrapeConfigName),
	} {
		installed, err := isInstalled(k8sClient, resource)
		if err != nil {
			logger.Warn("cannot check whether the CRD is installed, assuming it is",
				"resource", resource.String(), "error", err)
		} else if !installed {
			logger.Info("the CRD is not installed, its resources are not scraped", "resource", resource.String())
			continue
		}
		forResource, err := informers.NewInformersForResourceWithTransform(factories, resource, dropManagedFields)
		if err != nil {
			return nil, fmt.Errorf("cannot create the informers for %s: %w", resource.String(), err)
		}
		result[resource.Resource] = forResource
	}
	return result, nil
}

func isInstalled(k8sClient kubernetes.Interface, resource schema.GroupVersionResource) (bool, error) {
	resources, err := k8sClient.Discovery().ServerResourcesForGroupVersion(resource.GroupVersion().String())
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, apiResource := range resources.APIResources {
		if apiResource.Name == resource.Resource {
			return true, nil
		}
	}
	return false, nil
}

// Run starts the informers, generates the scrape configs once they have synced, and again after every relevant
// change, at most once per update interval, until ctx is cancelled. apply is called with every result. All
// generations and all calls of apply happen in the goroutine that called Run. Run fails if the namespaces cannot be
// listed, because without them no resource is selected.
func (g *Generator) Run(ctx context.Context, apply func(*ScrapeConfigs)) error {
	trigger := debounce.New(g.updateInterval)
	g.addEventHandlers(trigger)

	go g.namespaceInformer.Run(ctx.Done())
	for _, forResource := range g.resourceInformers {
		forResource.Start(ctx.Done())
	}
	g.secretInformers.Start(ctx.Done())

	syncCtx, cancel := context.WithTimeout(ctx, cacheSyncTimeout)
	defer cancel()
	if !cache.WaitForNamedCacheSync("namespaces", syncCtx.Done(), g.namespaceInformer.HasSynced) {
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("the namespace informer did not sync, check the permission to list and watch namespaces")
	}
	for name, forResource := range g.resourceInformers {
		if !cache.WaitForNamedCacheSync(name, syncCtx.Done(), forResource.HasSynced) {
			g.logger.Error("the informer did not sync, check the permission to list and watch the resource",
				"resource", name)
		}
	}
	if !cache.WaitForNamedCacheSync("secrets", syncCtx.Done(), g.secretInformers.HasSynced) {
		g.logger.Error("the secret informer did not sync, changes of secrets are only picked up with the next change "+
			"of a monitor or the next resync; check the permission to list and watch secrets",
			"namespaces", g.secretNamespaces, "resyncPeriod", resyncPeriod.String())
	}

	generate := func() {
		result, err := g.Generate(ctx)
		if err != nil {
			g.logger.Error("cannot generate the scrape configs", "error", err)
			return
		}
		apply(result)
	}
	generate()
	trigger.Run(ctx, generate)
	return nil
}

func (g *Generator) addEventHandlers(trigger *debounce.Trigger) {
	_, _ = g.namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		// With the empty namespace selectors, a change of a namespace's labels does not change the selection.
		AddFunc:    func(any) { trigger.Notify() },
		DeleteFunc: func(any) { trigger.Notify() },
	})
	for _, forResource := range g.resourceInformers {
		forResource.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(any) { trigger.Notify() },
			UpdateFunc: func(any, any) { trigger.Notify() },
			DeleteFunc: func(any) { trigger.Notify() },
		})
	}
	onSecretEvent := func(obj any) {
		if g.generating.Load() || g.isReferencedSecret(obj) {
			trigger.Notify()
		}
	}
	g.secretInformers.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    onSecretEvent,
		UpdateFunc: func(_, newObj any) { onSecretEvent(newObj) },
		DeleteFunc: onSecretEvent,
	})
}

// isReferencedSecret returns true if the last generation read the secret, or tried to read it.
func (g *Generator) isReferencedSecret(obj any) bool {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	secretMeta, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	referencedSecrets := g.referencedSecrets.Load()
	if referencedSecrets == nil {
		return false
	}
	return referencedSecrets.Has(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretMeta.GetName(), Namespace: secretMeta.GetNamespace()},
	})
}

// Generate generates the scrape configs from the custom resources in the informer caches. Secrets are read from the
// API server, so every generation sees their current values. It must not be called concurrently.
func (g *Generator) Generate(ctx context.Context) (*ScrapeConfigs, error) {
	g.generating.Store(true)
	store := assets.NewStoreBuilder(g.k8sClient.CoreV1(), g.k8sClient.CoreV1())
	defer func() {
		referencedSecrets := store.RefTracker()
		g.referencedSecrets.Store(&referencedSecrets)
		g.generating.Store(false)
	}()
	selector, err := prometheus.NewResourceSelector(
		g.libraryLogger, g.prometheus, store, g.namespaceInformer, g.operatorMetrics, g.eventRecorder)
	if err != nil {
		return nil, fmt.Errorf("cannot create the resource selector: %w", err)
	}

	serviceMonitors := map[string]*monitoringv1.ServiceMonitor{}
	if forResource, ok := g.resourceInformers[monitoringv1.ServiceMonitorName]; ok {
		selection, err := selector.SelectServiceMonitors(ctx, forResource.ListAllByNamespace)
		if err != nil {
			return nil, fmt.Errorf("cannot select the service monitors: %w", err)
		}
		serviceMonitors = selection.ValidResources()
	}
	podMonitors := map[string]*monitoringv1.PodMonitor{}
	if forResource, ok := g.resourceInformers[monitoringv1.PodMonitorName]; ok {
		selection, err := selector.SelectPodMonitors(ctx, forResource.ListAllByNamespace)
		if err != nil {
			return nil, fmt.Errorf("cannot select the pod monitors: %w", err)
		}
		podMonitors = selection.ValidResources()
	}
	scrapeConfigResources := map[string]*monitoringv1alpha1.ScrapeConfig{}
	if forResource, ok := g.resourceInformers[monitoringv1alpha1.ScrapeConfigName]; ok {
		selection, err := selector.SelectScrapeConfigs(ctx, forResource.ListAllByNamespace)
		if err != nil {
			return nil, fmt.Errorf("cannot select the scrape configs: %w", err)
		}
		scrapeConfigResources = selection.ValidResources()
	}

	generated, err := g.configGenerator.GenerateServerConfiguration(
		g.prometheus,
		serviceMonitors,
		podMonitors,
		map[string]*monitoringv1.Probe{},
		scrapeConfigResources,
		store,
		nil,
		nil,
		nil,
		[]string{},
	)
	if err != nil {
		return nil, fmt.Errorf("cannot generate the Prometheus configuration: %w", err)
	}
	served, err := parseScrapeConfigs(generated)
	if err != nil {
		return nil, err
	}
	discovery, err := parseScrapeConfigs(
		bytes.ReplaceAll(generated, []byte(shardPlaceholder), []byte(shardOfCollector)))
	if err != nil {
		return nil, err
	}
	served = g.dropFileReferences(served)
	servedJobs := make(map[string]struct{}, len(served))
	for _, scrapeConfig := range served {
		servedJobs[scrapeConfig.JobName] = struct{}{}
	}
	discovery = slices.DeleteFunc(discovery, func(scrapeConfig *promconfig.ScrapeConfig) bool {
		_, ok := servedJobs[scrapeConfig.JobName]
		return !ok
	})
	return &ScrapeConfigs{Served: served, Discovery: discovery}, nil
}

func parseScrapeConfigs(generated []byte) ([]*promconfig.ScrapeConfig, error) {
	prometheusConfig := &promconfig.Config{}
	if err := yaml.Unmarshal(generated, prometheusConfig); err != nil {
		return nil, fmt.Errorf("cannot parse the generated Prometheus configuration: %w", err)
	}
	return prometheusConfig.ScrapeConfigs, nil
}

// dropFileReferences drops the scrape configs that reference a file. The collector would read the file, so a monitor
// could make it send its service account token, or any other file it can read, to an arbitrary endpoint.
func (g *Generator) dropFileReferences(scrapeConfigs []*promconfig.ScrapeConfig) []*promconfig.ScrapeConfig {
	droppedJobs := map[string]struct{}{}
	kept := make([]*promconfig.ScrapeConfig, 0, len(scrapeConfigs))
	for _, scrapeConfig := range scrapeConfigs {
		reference := fileReference(scrapeConfig)
		if reference == "" {
			kept = append(kept, scrapeConfig)
			continue
		}
		droppedJobs[scrapeConfig.JobName] = struct{}{}
		if _, logged := g.droppedJobs[scrapeConfig.JobName]; !logged {
			g.logger.Info("dropping a scrape config that references a file, the targets of the monitor are not scraped",
				"monitor", monitorOfJob(scrapeConfig.JobName), "job", scrapeConfig.JobName, "reference", reference)
		}
	}
	g.droppedJobs = droppedJobs
	return kept
}

// fileReference returns the setting of the scrape config that references a file, or an empty string.
func fileReference(scrapeConfig *promconfig.ScrapeConfig) string {
	httpClientConfig := scrapeConfig.HTTPClientConfig
	switch {
	case httpClientConfig.Authorization != nil && httpClientConfig.Authorization.CredentialsFile != "":
		return "authorization.credentials_file"
	case httpClientConfig.TLSConfig.CAFile != "":
		return "tls_config.ca_file"
	case httpClientConfig.TLSConfig.CertFile != "":
		return "tls_config.cert_file"
	case httpClientConfig.TLSConfig.KeyFile != "":
		return "tls_config.key_file"
	}
	return ""
}

// monitorOfJob returns the kind, namespace and name of the resource a job was generated from, e.g.
// "serviceMonitor/my-namespace/my-monitor" for the job "serviceMonitor/my-namespace/my-monitor/0".
func monitorOfJob(job string) string {
	parts := strings.SplitN(job, "/", 4)
	if len(parts) < 3 {
		return job
	}
	return strings.Join(parts[:3], "/")
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// dropManagedFields removes the managed fields of cached custom resources. They are never read and can make up a large
// part of an object.
func dropManagedFields(obj any) (any, error) {
	if accessor, err := meta.Accessor(obj); err == nil {
		accessor.SetManagedFields(nil)
	}
	return obj, nil
}

// keepNameAndLabels reduces a namespace to the fields the resource selector reads.
func keepNameAndLabels(obj any) (any, error) {
	namespace, ok := obj.(*corev1.Namespace)
	if !ok {
		return obj, nil
	}
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:            namespace.Name,
			Labels:          namespace.Labels,
			ResourceVersion: namespace.ResourceVersion,
		},
	}, nil
}

// minLevelHandler drops log records below a minimum level. The Prometheus operator libraries log routine operations at
// info level.
type minLevelHandler struct {
	slog.Handler
	minLevel slog.Level
}

func (h minLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.minLevel && h.Handler.Enabled(ctx, level)
}

func (h minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithAttrs(attrs), minLevel: h.minLevel}
}

func (h minLevelHandler) WithGroup(name string) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithGroup(name), minLevel: h.minLevel}
}
