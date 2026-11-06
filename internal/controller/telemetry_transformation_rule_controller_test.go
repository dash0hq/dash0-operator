// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"time"

	dash0apiclient "github.com/dash0hq/dash0-api-client-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	. "github.com/dash0hq/dash0-operator/test/util"
)

var _ = Describe("The telemetry transformation rule controller", func() {
	var (
		backend  *transformationRuleTestAPI
		server   *httptest.Server
		r        *TelemetryTransformationRuleReconciler
		resource *dash0v1alpha1.Dash0TelemetryTransformationRule
		kube     client.Client
		config   ApiConfig
		request  reconcile.Request
	)
	ctx := context.Background()
	logger := logd.FromContext(ctx)
	BeforeEach(func() {
		backend = &transformationRuleTestAPI{rules: map[string]transformationRuleTestEntry{}, fail: map[string]int{}}
		server = httptest.NewServer(backend)
		DeferCleanup(server.Close)
		resource = transformationRuleTestResource()
		scheme := runtime.NewScheme()
		Expect(dash0v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(dash0v1beta1.AddToScheme(scheme)).To(Succeed())
		monitoring := &dash0v1beta1.Dash0Monitoring{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Namespace: "namespace"}}
		kube = fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(resource).WithObjects(resource, monitoring).Build()
		config = ApiConfig{Endpoint: server.URL + "/", Dataset: "production", Token: AuthorizationTokenTest}
		r = NewTelemetryTransformationRuleReconciler(kube, "cluster-uid", NewLeaderElectionAwareMock(true),
			NewApiClientPool(dash0apiclient.NewTransport(dash0apiclient.WithTransportMaxRetries(0)), "test"))
		r.defaultApiConfigs.Set([]ApiConfig{config})
		request = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(resource)}
	})
	reconcileRule := func() *dash0v1alpha1.Dash0TelemetryTransformationRule {
		GinkgoHelper()
		_, err := r.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		actual := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
		Expect(kube.Get(ctx, request.NamespacedName, actual)).To(Succeed())
		return actual
	}

	It("creates once, updates after restart, and replaces examples without losing order or integer precision", func() {
		actual := reconcileRule()
		Expect(controllerutil.ContainsFinalizer(actual, telemetryTransformationRuleFinalizer)).To(BeTrue())
		Expect(actual.Status.SynchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusSuccessful))
		Expect(actual.Status.SynchronizationResults).To(HaveLen(1))
		Expect(actual.Status.SynchronizationResults[0].Dash0Id).To(Equal("ttr-1"))
		Expect(actual.Status.SynchronizationResults[0].Dash0Origin).To(Equal(
			"dash0-operator_cluster-uid_production_namespace_normalize"))
		stored := backend.snapshot()
		Expect(stored.Spec.Transformations[0].Name).To(Equal("z-first"))
		Expect(stored.Spec.Transformations[1].Name).To(Equal("a-second"))
		Expect(stored.Spec.Transformations[0].Statements).To(Equal([]string{
			`set(attributes["z"], "1")`, `set(attributes["a"], attributes["z"])`,
		}))
		otlp, err := json.Marshal((*stored.Spec.Examples)[0].Otlp)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(otlp)).To(ContainSubstring("1793789308123456789"))
		Expect(stored.Metadata.Labels.Dash0Comdataset).To(HaveValue(Equal("production")))
		Expect(stored.Metadata.Annotations.Dash0ComupdatedAt.Format(time.RFC3339Nano)).To(Equal("2026-11-04T10:48:28.123456789Z"))

		r = NewTelemetryTransformationRuleReconciler(kube, "cluster-uid", NewLeaderElectionAwareMock(true), r.apiClientPool)
		r.defaultApiConfigs.Set([]ApiConfig{config})
		actual.Spec.Enabled = true
		actual.Spec.Examples = nil
		Expect(kube.Update(ctx, actual)).To(Succeed())
		reconcileRule()
		stored = backend.snapshot()
		Expect(stored.Spec.Enabled).To(BeTrue())
		Expect(stored.Spec.Examples).To(BeNil())
		backend.mu.Lock()
		defer backend.mu.Unlock()
		Expect(backend.creates).To(Equal(1))
		Expect(backend.updates).To(Equal(1))
		Expect(backend.problems).To(BeEmpty())
	})

	It("recovers an ambiguous POST through GET without replaying the create", func() {
		backend.loseCreateResponse = true
		actual := reconcileRule()
		Expect(actual.Status.SynchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusFailed))
		requests, err := r.CreateReconcileRequestsForRetryableSyncErrors(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(requests).To(ConsistOf(request))
		actual = reconcileRule()
		Expect(actual.Status.SynchronizationResults[0].Dash0Id).To(Equal("ttr-1"))
		backend.mu.Lock()
		defer backend.mu.Unlock()
		Expect(backend.creates).To(Equal(1))
		Expect(backend.rules).To(HaveLen(1))
	})

	It("revives a remotely deleted rule under the same origin and ID", func() {
		reconcileRule()
		backend.mu.Lock()
		for origin, rule := range backend.rules {
			rule.deleted = true
			backend.rules[origin] = rule
		}
		backend.mu.Unlock()
		actual := reconcileRule()
		Expect(actual.Status.SynchronizationResults[0].Dash0Id).To(Equal("ttr-1"))
		backend.mu.Lock()
		defer backend.mu.Unlock()
		Expect(backend.rules).To(HaveLen(1))
		Expect(backend.updates).To(Equal(1))
		Expect(backend.problems).To(BeEmpty())
	})

	It("refuses to revive an origin that exists in another dataset", func() {
		reconcileRule()
		backend.mu.Lock()
		for origin, rule := range backend.rules {
			rule.dataset = "other"
			backend.rules[origin] = rule
		}
		backend.mu.Unlock()
		actual := reconcileRule()
		Expect(actual.Status.SynchronizationResults[0].HttpStatusCode).To(Equal(http.StatusConflict))
		backend.mu.Lock()
		defer backend.mu.Unlock()
		for _, rule := range backend.rules {
			Expect(rule.dataset).To(Equal("other"))
		}
	})

	It("retains the deletion finalizer after an API failure and removes it after a successful retry", func() {
		actual := reconcileRule()
		Expect(kube.Delete(ctx, actual)).To(Succeed())
		backend.mu.Lock()
		backend.fail[http.MethodDelete] = http.StatusServiceUnavailable
		backend.mu.Unlock()
		_, err := r.Reconcile(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(kube.Get(ctx, request.NamespacedName, actual)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(actual, telemetryTransformationRuleFinalizer)).To(BeTrue())
		Expect(actual.Status.SynchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusFailed))
		Expect(actual.Status.SynchronizationResults[0].HttpStatusCode).To(Equal(http.StatusServiceUnavailable))
		Expect(actual.Status.SynchronizationResults[0].SynchronizationError).To(ContainSubstring("trace-transformation"))
		backend.mu.Lock()
		delete(backend.fail, http.MethodDelete)
		backend.mu.Unlock()
		_, err = r.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(kube.Get(ctx, request.NamespacedName, actual)).To(MatchError(ContainSubstring("not found")))
	})

	It("treats delete 404 as success and honors per-resource synchronization disabling", func() {
		actual := reconcileRule()
		actual.Labels["dash0.com/enable"] = "false"
		Expect(kube.Update(ctx, actual)).To(Succeed())
		reconcileRule()
		Expect(kube.Delete(ctx, actual)).To(Succeed())
		_, err := r.Reconcile(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(kube.Get(ctx, request.NamespacedName, actual)).To(MatchError(ContainSubstring("not found")))
	})

	DescribeTable("records API error details and selects only transient failures for periodic retries",
		func(status int, retry bool) {
			backend.fail[http.MethodGet] = status
			actual := reconcileRule()
			result := actual.Status.SynchronizationResults[0]
			Expect(result.HttpStatusCode).To(Equal(status))
			Expect(result.SynchronizationError).To(ContainSubstring("trace-transformation"))
			Expect(result.SynchronizationError).To(ContainSubstring("invalid_rule"))
			requests, err := r.CreateReconcileRequestsForRetryableSyncErrors(ctx)
			Expect(err).ToNot(HaveOccurred())
			if retry {
				Expect(requests).To(ConsistOf(request))
			} else {
				Expect(requests).To(BeEmpty())
			}
		},
		Entry("validation", http.StatusBadRequest, false),
		Entry("authorization", http.StatusForbidden, false),
		Entry("rate limiting", http.StatusTooManyRequests, true),
		Entry("server error", http.StatusServiceUnavailable, true),
	)

	It("starts synchronization only after leadership and configuration are available", func() {
		leader := NewLeaderElectionAwareMock(false)
		r.leaderElectionAware = leader
		r.SetDefaultApiConfigs(ctx, []ApiConfig{config}, logger)
		Expect(r.initialSyncStarted).To(BeFalse())
		leader.SetLeader(true)
		r.NotifyOperatorManagerJustBecameLeader(ctx, logger)
		Eventually(func(g Gomega) {
			actual := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
			g.Expect(kube.Get(ctx, request.NamespacedName, actual)).To(Succeed())
			g.Expect(actual.Status.SynchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusSuccessful))
		}).Should(Succeed())

		config.Dataset = "updated-default"
		r.SetDefaultApiConfigs(ctx, []ApiConfig{config}, logger)
		Eventually(func(g Gomega) {
			actual := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
			g.Expect(kube.Get(ctx, request.NamespacedName, actual)).To(Succeed())
			g.Expect(actual.Status.SynchronizationResults[0].Dash0Dataset).To(Equal(config.Dataset))
		}).Should(Succeed())
	})

	It("continues startup synchronization after another rule fails to finalize", func() {
		actual := reconcileRule()
		Expect(kube.Delete(ctx, actual)).To(Succeed())
		backend.mu.Lock()
		backend.fail[http.MethodDelete] = http.StatusServiceUnavailable
		backend.mu.Unlock()
		other := transformationRuleTestResource()
		other.Name = "second"
		Expect(kube.Create(ctx, other)).To(Succeed())
		r.SetDefaultApiConfigs(ctx, []ApiConfig{config}, logger)
		Eventually(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(other), other)).To(Succeed())
			g.Expect(other.Status.SynchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusSuccessful))
		}).Should(Succeed())
	})

	It("selects a namespaced dataset and resynchronizes configuration changes without moving an existing rule", func() {
		reconcileRule()
		namespaced := config
		namespaced.Dataset = "a/b +ä%"
		r.SetNamespacedApiConfigs(ctx, resource.Namespace, []ApiConfig{namespaced}, logger)
		Eventually(func(g Gomega) {
			actual := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
			g.Expect(kube.Get(ctx, request.NamespacedName, actual)).To(Succeed())
			g.Expect(actual.Status.SynchronizationResults[0].Dash0Dataset).To(Equal(namespaced.Dataset))
			g.Expect(actual.Status.SynchronizationStatus).To(Equal(dash0common.Dash0ApiResourceSynchronizationStatusSuccessful))
		}).Should(Succeed())
		backend.mu.Lock()
		defer backend.mu.Unlock()
		Expect(backend.rules).To(HaveLen(2))
		Expect(backend.problems).To(BeEmpty())
	})

	It("has no schema drift against the generated public client spec", func() {
		expectNoUnexpectedApiTypeDrift(reflect.TypeFor[dash0v1alpha1.Dash0TelemetryTransformationRuleSpec](),
			reflect.TypeFor[dash0apiclient.TelemetryTransformationRuleSpec](), nil, nil)
	})
})

func transformationRuleTestResource() *dash0v1alpha1.Dash0TelemetryTransformationRule {
	resource := &dash0v1alpha1.Dash0TelemetryTransformationRule{}
	Expect(json.Unmarshal([]byte(`{
		"apiVersion":"operator.dash0.com/v1alpha1","kind":"Dash0TelemetryTransformationRule",
		"metadata":{"name":"normalize","namespace":"namespace","labels":{"app":"example","dash0.com/id":"copied-id"},
		  "annotations":{"dash0.com/updated-at":"2020-01-01T00:00:00Z","dash0.com/created-at":"2020-01-01T00:00:00Z"}},
		"spec":{"enabled":false,"signal":"logs","context":"log","priority":0,
		  "conditions":["true","false"],"display":{"name":"Normalize"},
		  "transformations":[{"name":"z-first","statements":["set(attributes[\"z\"], \"1\")","set(attributes[\"a\"], attributes[\"z\"])"]},
		    {"name":"a-second","statements":["delete_key(attributes, \"z\")"]}],
		  "examples":[{"name":"synthetic","otlp":{"resourceLogs":[{"scopeLogs":[{"logRecords":[{
		    "timeUnixNano":1793789308123456789,"body":{"stringValue":"synthetic"},"attributes":[]}]}]}]}}]}
	}`), resource)).To(Succeed())
	return resource
}

type transformationRuleTestEntry struct {
	definition dash0apiclient.TelemetryTransformationRuleDefinition
	dataset    string
	deleted    bool
}

type transformationRuleTestAPI struct {
	mu                 sync.Mutex
	rules              map[string]transformationRuleTestEntry
	fail               map[string]int
	creates, updates   int
	loseCreateResponse bool
	problems           []string
}

func (b *transformationRuleTestAPI) snapshot() dash0apiclient.TelemetryTransformationRuleDefinition {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, rule := range b.rules {
		return rule.definition
	}
	return dash0apiclient.TelemetryTransformationRuleDefinition{}
}

func (b *transformationRuleTestAPI) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int) {
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"type":"invalid_rule","message":"test failure","traceId":"trace-transformation"}}`, status)
	}
	if status := b.fail[request.Method]; status != 0 {
		fail(status)
		return
	}
	dataset := request.URL.Query().Get("dataset")
	origin := strings.TrimPrefix(request.URL.Path, "/api/telemetry-transformation-rules/")
	entry, exists := b.rules[origin]
	switch request.Method {
	case http.MethodGet:
		if !exists || entry.deleted || entry.dataset != dataset {
			fail(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(entry.definition)
	case http.MethodPost, http.MethodPut:
		var definition dash0apiclient.TelemetryTransformationRuleDefinition
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&definition); err != nil {
			fail(http.StatusBadRequest)
			return
		}
		if definition.Metadata.Labels == nil || definition.Metadata.Labels.Dash0Comorigin == nil ||
			definition.Metadata.Labels.Dash0Comdataset == nil || *definition.Metadata.Labels.Dash0Comdataset != dataset {
			b.problems = append(b.problems, "missing or inconsistent origin/dataset")
			fail(http.StatusBadRequest)
			return
		}
		if request.Method == http.MethodPost {
			origin = *definition.Metadata.Labels.Dash0Comorigin
			if _, exists := b.rules[origin]; exists {
				fail(http.StatusConflict)
				return
			}
			b.creates++
			if definition.Metadata.Annotations != nil {
				b.problems = append(b.problems, "server metadata on POST")
			}
			id := fmt.Sprintf("ttr-%d", b.creates)
			definition.Metadata.Labels.Dash0Comid = &id
		} else {
			if !exists {
				fail(http.StatusNotFound)
				return
			}
			if entry.dataset != dataset {
				fail(http.StatusConflict)
				return
			}
			if !entry.deleted && (definition.Metadata.Annotations == nil ||
				definition.Metadata.Annotations.Dash0ComupdatedAt == nil ||
				!definition.Metadata.Annotations.Dash0ComupdatedAt.Equal(*entry.definition.Metadata.Annotations.Dash0ComupdatedAt)) {
				b.problems = append(b.problems, "missing or stale update token")
				fail(http.StatusBadRequest)
				return
			}
			b.updates++
			definition.Metadata.Labels.Dash0Comid = entry.definition.Metadata.Labels.Dash0Comid
		}
		updatedAt := time.Date(2026, 11, 4, 10, 48, 28, 123456789+b.updates, time.UTC)
		definition.Metadata.Annotations = &dash0apiclient.TelemetryTransformationRuleAnnotations{Dash0ComupdatedAt: &updatedAt}
		b.rules[origin] = transformationRuleTestEntry{definition: definition, dataset: dataset}
		if request.Method == http.MethodPost && b.loseCreateResponse {
			b.loseCreateResponse = false
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		_ = json.NewEncoder(w).Encode(definition)
	case http.MethodDelete:
		if !exists || entry.deleted || entry.dataset != dataset {
			fail(http.StatusNotFound)
			return
		}
		entry.deleted = true
		b.rules[origin] = entry
	default:
		fail(http.StatusMethodNotAllowed)
	}
}
