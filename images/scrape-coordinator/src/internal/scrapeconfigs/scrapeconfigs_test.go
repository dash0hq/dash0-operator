// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package scrapeconfigs

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	monitoringfake "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned/fake"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	"sigs.k8s.io/yaml"

	"github.com/dash0hq/dash0-operator/images/scrape-coordinator/internal/config"
)

const (
	collectorNamespace = "dash0-system"
	monitoredNamespace = "monitored"
	otherNamespace     = "other"
	updateInterval     = 10 * time.Millisecond
)

var discardLogger = slog.New(slog.DiscardHandler)

var allCRDs = []*metav1.APIResourceList{
	{
		GroupVersion: monitoringv1.SchemeGroupVersion.String(),
		APIResources: []metav1.APIResource{
			{Name: monitoringv1.ServiceMonitorName},
			{Name: monitoringv1.PodMonitorName},
			{Name: monitoringv1.ProbeName},
		},
	},
	{
		GroupVersion: monitoringv1alpha1.SchemeGroupVersion.String(),
		APIResources: []metav1.APIResource{{Name: monitoringv1alpha1.ScrapeConfigName}},
	},
}

func fromYAML[T runtime.Object](t *testing.T, manifest string, obj T) T {
	t.Helper()
	require.NoError(t, yaml.UnmarshalStrict([]byte(manifest), obj))
	return obj
}

func serviceMonitor(t *testing.T, namespace string, name string, endpoint string) *monitoringv1.ServiceMonitor {
	t.Helper()
	return fromYAML(t, `
metadata:
  name: `+name+`
  namespace: `+namespace+`
spec:
  selector: {matchLabels: {app: test}}
  endpoints:
    - `+endpoint, &monitoringv1.ServiceMonitor{})
}

func namespaceObject(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func secretObject(name string, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: monitoredNamespace},
		Data:       map[string][]byte{"username": []byte("user"), "password": []byte(password)},
	}
}

func secretMetadata(name string, resourceVersion string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: monitoredNamespace, ResourceVersion: resourceVersion},
	}
}

type testClients struct {
	kube       *fake.Clientset
	monitoring *monitoringfake.Clientset
	metadata   *metadatafake.FakeMetadataClient
}

func newTestClients(
	t *testing.T,
	crds []*metav1.APIResourceList,
	kubeObjects []runtime.Object,
	monitoringObjects []runtime.Object,
	secrets ...*metav1.PartialObjectMetadata,
) *testClients {
	t.Helper()
	kube := fake.NewClientset(kubeObjects...)
	kube.Discovery().(*fakediscovery.FakeDiscovery).Resources = crds
	scheme := metadatafake.NewTestScheme()
	require.NoError(t, metav1.AddMetaToScheme(scheme))
	metadataObjects := make([]runtime.Object, 0, len(secrets))
	for _, secret := range secrets {
		metadataObjects = append(metadataObjects, secret)
	}
	return &testClients{
		kube: kube,
		// The field manager of NewClientset has no schema for custom resources and rejects every create and update.
		monitoring: monitoringfake.NewSimpleClientset(monitoringObjects...),
		metadata:   metadatafake.NewSimpleMetadataClient(scheme, metadataObjects...),
	}
}

func testConfig() *config.Config {
	cfg, err := config.Parse([]byte(`
collectorNamespace: ` + collectorNamespace + `
pools:
  perNode:
    selector: {matchLabels: {component: agent}}
prometheusCRs:
  namespaces: [` + monitoredNamespace + `]
  secretNamespaces: [` + monitoredNamespace + `]
`))
	if err != nil {
		panic(err)
	}
	return cfg
}

type results struct {
	mu     sync.Mutex
	latest *ScrapeConfigs
	count  int
}

func (r *results) apply(result *ScrapeConfigs) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest = result
	r.count++
}

func (r *results) get() (*ScrapeConfigs, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest, r.count
}

func (r *results) jobs() []string {
	latest, _ := r.get()
	if latest == nil {
		return nil
	}
	return jobNames(latest.Served)
}

func jobNames(scrapeConfigs []*promconfig.ScrapeConfig) []string {
	names := make([]string, 0, len(scrapeConfigs))
	for _, scrapeConfig := range scrapeConfigs {
		names = append(names, scrapeConfig.JobName)
	}
	slices.Sort(names)
	return names
}

func startGenerator(t *testing.T, clients *testClients) *results {
	t.Helper()
	generator, err := New(discardLogger, clients.kube, clients.monitoring, clients.metadata, testConfig(), updateInterval)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	r := &results{}
	done := make(chan error, 1)
	go func() { done <- generator.Run(ctx, r.apply) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
	return r
}

func eventuallyJobs(t *testing.T, r *results, want []string, message string) {
	t.Helper()
	require.Eventually(t, func() bool { return slices.Equal(want, r.jobs()) }, 5*time.Second, 10*time.Millisecond,
		"%s: got %v", message, r.jobs())
}

func TestGeneratesAJobPerMonitorEndpoint(t *testing.T) {
	probe := fromYAML(t, `
metadata: {name: probe, namespace: `+monitoredNamespace+`}
spec:
  prober: {url: blackbox:9115}
  targets: {staticConfig: {static: ["example.com"]}}
`, &monitoringv1.Probe{})
	podMonitor := fromYAML(t, `
metadata: {name: pm, namespace: `+monitoredNamespace+`}
spec:
  selector: {matchLabels: {app: test}}
  podMetricsEndpoints: [{port: metrics}]
`, &monitoringv1.PodMonitor{})
	scrapeConfig := fromYAML(t, `
metadata: {name: sc, namespace: `+monitoredNamespace+`}
spec:
  staticConfigs: [{targets: ["static:8080"]}]
`, &monitoringv1alpha1.ScrapeConfig{})
	clients := newTestClients(t, allCRDs,
		[]runtime.Object{namespaceObject(monitoredNamespace), namespaceObject(otherNamespace)},
		[]runtime.Object{
			serviceMonitor(t, monitoredNamespace, "sm", "port: metrics"),
			serviceMonitor(t, otherNamespace, "outside-the-allow-list", "port: metrics"),
			podMonitor,
			scrapeConfig,
			probe,
		},
	)

	r := startGenerator(t, clients)
	eventuallyJobs(t, r, []string{
		"podMonitor/monitored/pm/0",
		"scrapeConfig/monitored/sc",
		"serviceMonitor/monitored/sm/0",
	}, "monitors in the watched namespaces are selected, probes are not")
}

func TestMissingCRDsAreSkipped(t *testing.T) {
	onlyServiceMonitors := []*metav1.APIResourceList{{
		GroupVersion: monitoringv1.SchemeGroupVersion.String(),
		APIResources: []metav1.APIResource{{Name: monitoringv1.ServiceMonitorName}},
	}}
	podMonitor := fromYAML(t, `
metadata: {name: pm, namespace: `+monitoredNamespace+`}
spec:
  selector: {matchLabels: {app: test}}
  podMetricsEndpoints: [{port: metrics}]
`, &monitoringv1.PodMonitor{})
	clients := newTestClients(t, onlyServiceMonitors,
		[]runtime.Object{namespaceObject(monitoredNamespace)},
		[]runtime.Object{serviceMonitor(t, monitoredNamespace, "sm", "port: metrics"), podMonitor},
	)

	generator, err := New(discardLogger, clients.kube, clients.monitoring, clients.metadata, testConfig(), updateInterval)
	require.NoError(t, err)
	assert.Contains(t, generator.resourceInformers, monitoringv1.ServiceMonitorName)
	assert.NotContains(t, generator.resourceInformers, monitoringv1.PodMonitorName)
	assert.NotContains(t, generator.resourceInformers, monitoringv1alpha1.ScrapeConfigName)
}

func TestNamespaceCreatedAfterItsMonitor(t *testing.T) {
	clients := newTestClients(t, allCRDs, nil,
		[]runtime.Object{serviceMonitor(t, monitoredNamespace, "sm", "port: metrics")})
	r := startGenerator(t, clients)
	eventuallyJobs(t, r, []string{}, "without the namespace, the monitor is not selected")

	_, err := clients.kube.CoreV1().Namespaces().Create(t.Context(), namespaceObject(monitoredNamespace),
		metav1.CreateOptions{})
	require.NoError(t, err)
	eventuallyJobs(t, r, []string{"serviceMonitor/monitored/sm/0"}, "the namespace is picked up")
}

func TestMonitorChanges(t *testing.T) {
	clients := newTestClients(t, allCRDs, []runtime.Object{namespaceObject(monitoredNamespace)}, nil)
	r := startGenerator(t, clients)
	eventuallyJobs(t, r, []string{}, "no monitors")

	serviceMonitors := clients.monitoring.MonitoringV1().ServiceMonitors(monitoredNamespace)
	_, err := serviceMonitors.Create(t.Context(), serviceMonitor(t, monitoredNamespace, "sm", "port: metrics"),
		metav1.CreateOptions{})
	require.NoError(t, err)
	eventuallyJobs(t, r, []string{"serviceMonitor/monitored/sm/0"}, "a new monitor is picked up")

	require.NoError(t, serviceMonitors.Delete(t.Context(), "sm", metav1.DeleteOptions{}))
	eventuallyJobs(t, r, []string{}, "a deleted monitor is removed")
}

func basicAuthPassword(r *results) string {
	latest, _ := r.get()
	if latest == nil || len(latest.Served) == 0 || latest.Served[0].HTTPClientConfig.BasicAuth == nil {
		return ""
	}
	return string(latest.Served[0].HTTPClientConfig.BasicAuth.Password)
}

const basicAuthEndpoint = `port: metrics
      basicAuth:
        username: {name: auth, key: username}
        password: {name: auth, key: password}`

func TestReferencedSecretChanges(t *testing.T) {
	clients := newTestClients(t, allCRDs,
		[]runtime.Object{namespaceObject(monitoredNamespace), secretObject("auth", "first"), secretObject("unrelated", "")},
		[]runtime.Object{serviceMonitor(t, monitoredNamespace, "sm", basicAuthEndpoint)},
		secretMetadata("auth", "1"), secretMetadata("unrelated", "1"),
	)
	r := startGenerator(t, clients)
	require.Eventually(t, func() bool { return basicAuthPassword(r) == "first" }, 5*time.Second, 10*time.Millisecond)

	time.Sleep(10 * updateInterval)
	_, generations := r.get()
	_, err := clients.secretMetadata().UpdateFake(secretMetadata("unrelated", "2"), metav1.UpdateOptions{})
	require.NoError(t, err)
	time.Sleep(10 * updateInterval)
	_, generationsAfterUnrelatedChange := r.get()
	assert.Equal(t, generations, generationsAfterUnrelatedChange, "a secret no monitor references is ignored")

	_, err = clients.kube.CoreV1().Secrets(monitoredNamespace).Update(t.Context(), secretObject("auth", "second"),
		metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = clients.secretMetadata().UpdateFake(secretMetadata("auth", "2"), metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return basicAuthPassword(r) == "second" }, 5*time.Second, 10*time.Millisecond,
		"the new value of a referenced secret is picked up")
}

func (c *testClients) secretMetadata() metadatafake.MetadataClient {
	return c.metadata.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).
		Namespace(monitoredNamespace).(metadatafake.MetadataClient)
}

func TestSecretCreatedAfterTheMonitor(t *testing.T) {
	clients := newTestClients(t, allCRDs,
		[]runtime.Object{namespaceObject(monitoredNamespace)},
		[]runtime.Object{serviceMonitor(t, monitoredNamespace, "sm", basicAuthEndpoint)},
	)
	r := startGenerator(t, clients)
	eventuallyJobs(t, r, []string{}, "a monitor whose secret is missing is invalid")

	_, err := clients.kube.CoreV1().Secrets(monitoredNamespace).Create(t.Context(), secretObject("auth", "first"),
		metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = clients.secretMetadata().CreateFake(secretMetadata("auth", "1"), metav1.CreateOptions{})
	require.NoError(t, err)
	eventuallyJobs(t, r, []string{"serviceMonitor/monitored/sm/0"}, "the monitor becomes valid with its secret")
	assert.Equal(t, "first", basicAuthPassword(r))
}

func generateOnce(t *testing.T, clients *testClients) *ScrapeConfigs {
	t.Helper()
	generator, err := New(discardLogger, clients.kube, clients.monitoring, clients.metadata, testConfig(), updateInterval)
	require.NoError(t, err)
	var result *ScrapeConfigs
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, generator.Run(ctx, func(scrapeConfigs *ScrapeConfigs) {
		result = scrapeConfigs
		cancel()
	}))
	require.NotNil(t, result)
	return result
}

func TestScrapeConfigsWithFileReferencesAreDropped(t *testing.T) {
	clients := newTestClients(t, allCRDs,
		[]runtime.Object{namespaceObject(monitoredNamespace)},
		[]runtime.Object{
			serviceMonitor(t, monitoredNamespace, "bearer-token-file", "{port: metrics, bearerTokenFile: /token}"),
			serviceMonitor(t, monitoredNamespace, "ca-file", "{port: metrics, tlsConfig: {caFile: /ca.crt}}"),
			serviceMonitor(t, monitoredNamespace, "regular", "port: metrics"),
		},
	)
	result := generateOnce(t, clients)
	assert.Equal(t, []string{"serviceMonitor/monitored/regular/0"}, jobNames(result.Served))
	assert.Equal(t, []string{"serviceMonitor/monitored/regular/0"}, jobNames(result.Discovery))
}

func TestShardPlaceholder(t *testing.T) {
	clients := newTestClients(t, allCRDs,
		[]runtime.Object{namespaceObject(monitoredNamespace)},
		[]runtime.Object{serviceMonitor(t, monitoredNamespace, "sm", "port: metrics")},
	)
	result := generateOnce(t, clients)
	require.Len(t, result.Served, 1)
	require.Len(t, result.Discovery, 1)
	shardingRule := func(scrapeConfig *promconfig.ScrapeConfig) string {
		for _, relabelConfig := range scrapeConfig.RelabelConfigs {
			if slices.Contains(relabelConfig.SourceLabels, "__tmp_disable_sharding") {
				return relabelConfig.Regex.String()
			}
		}
		return ""
	}
	assert.Equal(t, "$(SHARD);|.+;.+", shardingRule(result.Served[0]), "the collectors replace the placeholder")
	assert.Equal(t, "0;|.+;.+", shardingRule(result.Discovery[0]), "replaced the way the collectors replace it")
}
