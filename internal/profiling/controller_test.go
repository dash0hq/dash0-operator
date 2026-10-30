// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/profiling"
	testutil "github.com/dash0hq/dash0-operator/test/util"
)

func TestProfilerRoutesSamplesAndDebuginfoSeparately(t *testing.T) {
	r, configuration := fixture(t)
	configuration.Spec.Profiling.EbpfProfiler = &dash0v1alpha1.EbpfProfiler{
		Enabled: ptr.To(true), CPUSamplingFrequency: ptr.To(int32(37)), OffCPUThreshold: ptr.To(0.17),
		ProbabilisticSampling: &dash0v1alpha1.ProbabilisticSampling{
			Threshold: ptr.To(int32(23)), Interval: metav1.Duration{Duration: 2 * time.Minute},
		},
		EnableErrorFrames: ptr.To(true), Tracers: []string{"python", "hotspot"},
		Debuginfo: &dash0v1alpha1.EbpfProfilerDebuginfo{MaxConcurrentUploads: ptr.To(int32(3)), QueueSize: ptr.To(int32(73))},
	}
	reconcile(t, r, configuration)
	ds := &appsv1.DaemonSet{}
	require.NoError(t, r.Get(context.Background(), key("ebpf-profiler"), ds))
	pod := ds.Spec.Template.Spec
	require.True(t, pod.HostPID)
	require.False(t, *pod.AutomountServiceAccountToken)
	container := pod.Containers[0]
	require.Equal(t, "ghcr.io/parca-dev/parca-agent:v0.51.2", container.Image)
	require.True(t, *container.SecurityContext.Privileged)
	require.Zero(t, *container.SecurityContext.RunAsUser)
	for _, flag := range []string{
		"--remote-store-address=test-opentelemetry-collector-service.operator.svc:4317",
		"--metadata-external-labels=parca.agent=true",
		"--debuginfo-address=test-debuginfo-proxy.operator.svc:7070", "--remote-store-format=otlp", "--metadata-discovery=local",
		"--profiling-cpu-sampling-frequency=37", "--off-cpu-threshold=0.17", "--profiling-probabilistic-threshold=23",
		"--profiling-probabilistic-interval=2m0s", "--profiling-enable-error-frames=true", "--tracers=python,hotspot",
		"--debuginfo-upload-max-parallel=3", "--debuginfo-upload-queue-size=73", "--analytics-opt-out", "--telemetry-disable-panic-reporting",
	} {
		require.Contains(t, container.Args, flag)
	}
	serialized, err := json.Marshal(ds)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "symbolize-go")
	require.NotContains(t, string(serialized), "backend-token")
	require.NotContains(t, string(serialized), "secretKeyRef")
	for _, flag := range []string{"--otlp-logging", "--gpu-metrics-disable", "--gpu-nvtx-disable"} {
		require.NotContains(t, strings.Join(container.Args, " "), flag)
	}
	proxy := &appsv1.DaemonSet{}
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), proxy))
	require.Equal(t, appsv1.RollingUpdateDaemonSetStrategyType, proxy.Spec.UpdateStrategy.Type)
	require.EqualValues(t, 0, proxy.Spec.UpdateStrategy.RollingUpdate.MaxSurge.IntValue())
	require.EqualValues(t, 1, proxy.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable.IntValue())
	require.False(t, ptr.Deref(proxy.Spec.Template.Spec.Containers[0].SecurityContext.Privileged, false))
	require.True(t, *proxy.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem)
	require.NotNil(t, proxy.Spec.Template.Spec.Containers[0].ReadinessProbe.GRPC)
	require.Len(t, proxy.OwnerReferences, 1)
	require.Equal(t, types.UID("operator-deployment"), proxy.OwnerReferences[0].UID)
	service := &corev1.Service{}
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), service))
	require.Equal(t, corev1.ServiceInternalTrafficPolicyLocal, *service.Spec.InternalTrafficPolicy)
	require.Equal(t, proxy.Spec.Selector.MatchLabels, service.Spec.Selector)
	policy := &networkingv1.NetworkPolicy{}
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), policy))
	require.Equal(t, "ebpf-profiler", policy.Spec.Ingress[0].From[0].PodSelector.MatchLabels["app.kubernetes.io/component"])
	require.Nil(t, policy.Spec.Ingress[0].From[0].NamespaceSelector)
	namespace := &corev1.Namespace{}
	require.NoError(t, r.Get(context.Background(), client.ObjectKey{Name: "operator"}, namespace))
	require.Equal(t, map[string]string{"owned-by": "user"}, namespace.Labels)
}

func TestProfilerTracerSelection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tracers  []string
		expected string
	}{
		{name: "omitted", expected: "python,hotspot,php,ruby,perl,v8,dotnet,beam,luajit,labels,cuda"},
		{name: "empty", tracers: []string{}, expected: "python,hotspot,php,ruby,perl,v8,dotnet,beam,luajit,labels,cuda"},
		{name: "explicit without cuda", tracers: []string{"hotspot", "python"}, expected: "hotspot,python"},
		{name: "explicit with cuda", tracers: []string{"cuda", "labels"}, expected: "cuda,labels"},
		{name: "all", tracers: []string{"all"}, expected: "all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, configuration := fixture(t)
			configuration.Spec.Profiling.EbpfProfiler.Tracers = tc.tracers
			reconcile(t, r, configuration)
			ds := &appsv1.DaemonSet{}
			require.NoError(t, r.Get(context.Background(), key("ebpf-profiler"), ds))
			args := ds.Spec.Template.Spec.Containers[0].Args
			var tracerArgs []string
			for _, arg := range args {
				if strings.HasPrefix(arg, "--tracers=") {
					tracerArgs = append(tracerArgs, arg)
				}
			}
			require.Equal(t, []string{"--tracers=" + tc.expected}, tracerArgs)
			for _, flag := range []string{"--symbolize-go", "--otlp-logging", "--gpu-metrics-disable", "--gpu-nvtx-disable"} {
				require.NotContains(t, strings.Join(args, " "), flag)
			}
		})
	}
}

func TestProfilerPIDNamespaceTranslationMode(t *testing.T) {
	r, configuration := fixture(t)
	for _, mode := range []string{"", "none", "auto", "exact", "recursive", ""} {
		configuration.Spec.Profiling.EbpfProfiler.PIDNamespaceTranslationMode = mode
		reconcile(t, r, configuration)
		ds := &appsv1.DaemonSet{}
		require.NoError(t, r.Get(context.Background(), key("ebpf-profiler"), ds))
		var modeArgs []string
		for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
			if strings.HasPrefix(arg, "--pid-namespace-translation-mode=") {
				modeArgs = append(modeArgs, arg)
			}
		}
		if mode == "" {
			require.Empty(t, modeArgs, "omitting the mode must preserve the agent default")
		} else {
			require.Equal(t, []string{"--pid-namespace-translation-mode=" + mode}, modeArgs)
		}
	}
}

func TestProfilerOmitsDisabledOffCPUThreshold(t *testing.T) {
	for name, threshold := range map[string]*float64{"unset": nil, "zero": ptr.To(0.0)} {
		t.Run(name, func(t *testing.T) {
			r, configuration := fixture(t)
			configuration.Spec.Profiling.EbpfProfiler.OffCPUThreshold = threshold
			reconcile(t, r, configuration)
			ds := &appsv1.DaemonSet{}
			require.NoError(t, r.Get(context.Background(), key("ebpf-profiler"), ds))
			require.Contains(t, ds.Spec.Template.Spec.Containers[0].Args, "--profiling-probabilistic-threshold=100")
			for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
				require.False(t, strings.HasPrefix(arg, "--off-cpu-threshold"))
			}
		})
	}
}

func TestDebuginfoDestinationsAndCredentialRotation(t *testing.T) {
	r, configuration := fixture(t)
	configuration.Spec.Exports = append(configuration.Spec.Exports, dash0common.Export{
		Dash0: &dash0common.Dash0Configuration{Endpoint: "http://other-backend:4317", Dataset: "second",
			Authorization: dash0common.Authorization{Token: ptr.To("literal-backend-token")}},
		Grpc: &dash0common.GrpcConfiguration{Endpoint: "never-debuginfo:4317"},
	}, dash0common.Export{Http: &dash0common.HttpConfiguration{Endpoint: "http://never-http:4318"}})
	reconcile(t, r, configuration)
	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), cm))
	original := cm.Data["config.yaml"]
	require.NotContains(t, original, "backend-token")
	require.NotContains(t, original, "never-")
	var config struct {
		Exporters map[string]struct {
			Endpoint string `json:"endpoint"`
			TLS      struct {
				Insecure bool `json:"insecure"`
			} `json:"tls"`
			Headers map[string]string `json:"headers"`
		} `json:"exporters"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(original), &config))
	require.Len(t, config.Exporters, 2)
	for id, exporter := range config.Exporters {
		require.True(t, strings.HasPrefix(id, "debuginfo/"))
		require.Contains(t, exporter.Headers["authorization"], "Bearer ${env:DASH0_DEBUGINFO_TOKEN_")
		if exporter.Endpoint == "other-backend:4317" {
			require.True(t, exporter.TLS.Insecure)
			require.Equal(t, "second", exporter.Headers["dash0-dataset"])
		} else {
			require.Equal(t, "ingress.example:4317", exporter.Endpoint)
			require.False(t, exporter.TLS.Insecure)
			require.Equal(t, "primary", exporter.Headers["dash0-dataset"])
		}
	}
	slices.Reverse(configuration.Spec.Exports)
	reconcile(t, r, configuration)
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), cm))
	require.Equal(t, original, cm.Data["config.yaml"], "reordering exports must preserve routes")
	proxy := &appsv1.DaemonSet{}
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), proxy))
	previousRevision := proxy.Spec.Template.Annotations["dash0.com/configuration-revision"]
	secret := &corev1.Secret{}
	require.NoError(t, r.Get(context.Background(), client.ObjectKey{Namespace: "operator", Name: "authorization"}, secret))
	secret.Data["token"] = []byte("rotated-backend-token")
	require.NoError(t, r.Update(context.Background(), secret))
	_, err := r.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), proxy))
	require.NotEqual(t, previousRevision, proxy.Spec.Template.Annotations["dash0.com/configuration-revision"])
	require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), cm))
	require.Equal(t, original, cm.Data["config.yaml"])
	for _, env := range proxy.Spec.Template.Spec.Containers[0].Env {
		if env.ValueFrom != nil {
			require.Equal(t, "authorization", env.ValueFrom.SecretKeyRef.Name)
		}
	}
}

func TestProfilerLifecycle(t *testing.T) {
	for _, disable := range []string{"uploads", "dash0-exports", "profiler", "helm", "telemetry", "delete"} {
		t.Run(disable, func(t *testing.T) {
			r, configuration := fixture(t)
			reconcile(t, r, configuration)
			switch disable {
			case "uploads":
				configuration.Spec.Profiling.EbpfProfiler.Debuginfo = &dash0v1alpha1.EbpfProfilerDebuginfo{UploadEnabled: ptr.To(false)}
			case "dash0-exports":
				configuration.Spec.Exports = []dash0common.Export{{Grpc: &dash0common.GrpcConfiguration{Endpoint: "collector:4317"}}}
			case "profiler":
				configuration.Spec.Profiling.EbpfProfiler.Enabled = ptr.To(false)
			case "helm":
				r.Config.Enabled = false
			case "telemetry":
				r.Config.TelemetryCollectionEnabled = false
			case "delete":
				require.NoError(t, r.Delete(context.Background(), configuration))
			}
			if disable == "delete" {
				_, err := r.Reconcile(context.Background(), ctrl.Request{})
				require.NoError(t, err)
			} else {
				reconcile(t, r, configuration)
			}
			for _, object := range []client.Object{&appsv1.DaemonSet{}, &corev1.ConfigMap{}, &corev1.ServiceAccount{}, &corev1.Service{}, &networkingv1.NetworkPolicy{}} {
				require.True(t, apierrors.IsNotFound(r.Get(context.Background(), key("debuginfo-proxy"), object)))
			}
			ds := &appsv1.DaemonSet{}
			err := r.Get(context.Background(), key("ebpf-profiler"), ds)
			if disable == "uploads" || disable == "dash0-exports" {
				require.NoError(t, err)
				require.Contains(t, ds.Spec.Template.Spec.Containers[0].Args, "--debuginfo-upload-disable")
			} else {
				require.True(t, apierrors.IsNotFound(err))
			}
			if disable != "delete" {
				r.Config.Enabled, r.Config.TelemetryCollectionEnabled = true, true
				configuration.Spec.Profiling.EbpfProfiler.Enabled = ptr.To(true)
				configuration.Spec.Profiling.EbpfProfiler.Debuginfo = nil
				configuration.Spec.Exports = []dash0common.Export{{Dash0: &dash0common.Dash0Configuration{
					Endpoint: "ingress.example:4317", Authorization: dash0common.Authorization{Token: ptr.To("replacement-token")},
				}}}
				reconcile(t, r, configuration)
				require.NoError(t, r.Get(context.Background(), key("ebpf-profiler"), ds))
				require.NoError(t, r.Get(context.Background(), key("debuginfo-proxy"), &appsv1.DaemonSet{}))
			}
		})
	}
}

func TestProxyPolicyRemainsUntilPodsTerminate(t *testing.T) {
	for _, tc := range []struct{ uploadsOnly, holdDaemonSet bool }{{false, true}, {true, true}, {true, false}} {
		t.Run(fmt.Sprintf("uploads-only=%t/hold-daemonset=%t", tc.uploadsOnly, tc.holdDaemonSet), func(t *testing.T) {
			r, configuration := fixture(t)
			reconcile(t, r, configuration)
			ctx := context.Background()
			proxy := &appsv1.DaemonSet{}
			require.NoError(t, r.Get(ctx, key("debuginfo-proxy"), proxy))
			if tc.holdDaemonSet {
				proxy.Finalizers = []string{"test.dash0.com/hold"}
				require.NoError(t, r.Update(ctx, proxy))
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "terminating-proxy", Namespace: proxy.Namespace, Labels: proxy.Spec.Template.Labels,
			}}
			pod.Finalizers = []string{"test.dash0.com/hold"}
			require.NoError(t, r.Create(ctx, pod))
			if tc.uploadsOnly {
				configuration.Spec.Profiling.EbpfProfiler.Debuginfo = &dash0v1alpha1.EbpfProfilerDebuginfo{UploadEnabled: ptr.To(false)}
			} else {
				configuration.Spec.Profiling.EbpfProfiler.Enabled = ptr.To(false)
			}
			require.NoError(t, r.Update(ctx, configuration))
			result, err := r.Reconcile(ctx, ctrl.Request{})
			require.NoError(t, err)
			require.Positive(t, result.RequeueAfter)
			require.NoError(t, r.Get(ctx, key("debuginfo-proxy"), &networkingv1.NetworkPolicy{}))
			require.NoError(t, r.Delete(ctx, pod))
			result, err = r.Reconcile(ctx, ctrl.Request{})
			require.NoError(t, err)
			require.Positive(t, result.RequeueAfter)
			require.NoError(t, r.Get(ctx, key("debuginfo-proxy"), &networkingv1.NetworkPolicy{}))
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			require.NotNil(t, pod.DeletionTimestamp)
			pod.Finalizers = nil
			require.NoError(t, r.Update(ctx, pod))
			result, err = r.Reconcile(ctx, ctrl.Request{})
			require.NoError(t, err)
			if tc.holdDaemonSet {
				require.Positive(t, result.RequeueAfter)
				require.NoError(t, r.Get(ctx, key("debuginfo-proxy"), &networkingv1.NetworkPolicy{}))
				require.NoError(t, r.Get(ctx, key("debuginfo-proxy"), proxy))
				proxy.Finalizers = nil
				require.NoError(t, r.Update(ctx, proxy))
				result, err = r.Reconcile(ctx, ctrl.Request{})
				require.NoError(t, err)
			}
			require.Zero(t, result.RequeueAfter)
			require.True(t, apierrors.IsNotFound(r.Get(ctx, key("debuginfo-proxy"), &networkingv1.NetworkPolicy{})))
		})
	}
}

func TestProfilerReconcilesWithNamespacedOperandPermissions(t *testing.T) {
	r, configuration := fixture(t)
	require.NoError(t, rbacv1.AddToScheme(r.Scheme))
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases", "operator.dash0.com_dash0operatorconfigurations.yaml")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", testutil.EnvtestK8sVersion, goruntime.GOOS, goruntime.GOARCH)),
	}
	adminConfig, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	admin, err := client.New(adminConfig, client.Options{Scheme: r.Scheme})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "operator"}}))
	configuration.ResourceVersion = ""
	require.NoError(t, admin.Create(ctx, configuration))
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "authorization", Namespace: "operator"}, Data: map[string][]byte{"token": []byte("initial-token")}}
	require.NoError(t, admin.Create(ctx, secret))
	user, err := testEnv.AddUser(envtest.User{Name: "profiler-controller"}, adminConfig)
	require.NoError(t, err)
	for _, object := range []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "profiler-config-reader"}, Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.dash0.com"}, Resources: []string{"dash0operatorconfigurations"}, Verbs: []string{"get", "list", "watch"},
		}}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "profiler-config-reader"},
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "profiler-config-reader"},
			Subjects: []rbacv1.Subject{{Kind: "User", Name: "profiler-controller"}}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "profiler-operands", Namespace: "operator"}, Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"secrets", "services", "serviceaccounts", "configmaps"}, Verbs: []string{"*"}},
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}},
			{APIGroups: []string{"apps"}, Resources: []string{"daemonsets"}, Verbs: []string{"*"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"*"}},
		}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "profiler-operands", Namespace: "operator"},
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "profiler-operands"},
			Subjects: []rbacv1.Subject{{Kind: "User", Name: "profiler-controller"}}},
	} {
		require.NoError(t, admin.Create(ctx, object))
	}
	mgr, err := ctrl.NewManager(user.Config(), ctrl.Options{Scheme: r.Scheme, Metrics: metricsserver.Options{BindAddress: "0"}})
	require.NoError(t, err)
	require.NoError(t, r.SetupWithManager(mgr))
	managerCtx, cancel := context.WithCancel(ctx)
	managerStopped := make(chan error, 1)
	go func() { managerStopped <- mgr.Start(managerCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-managerStopped:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Error("profiler manager did not stop")
		}
	})
	proxy := &appsv1.DaemonSet{}
	require.Eventually(t, func() bool {
		return admin.Get(ctx, key("debuginfo-proxy"), proxy) == nil
	}, 20*time.Second, 100*time.Millisecond)
	previousRevision := proxy.Spec.Template.Annotations["dash0.com/configuration-revision"]
	require.NoError(t, admin.Get(ctx, client.ObjectKeyFromObject(secret), secret))
	secret.Data["token"] = []byte("rotated-token")
	require.NoError(t, admin.Update(ctx, secret))
	require.Eventually(t, func() bool {
		return admin.Get(ctx, key("debuginfo-proxy"), proxy) == nil &&
			proxy.Spec.Template.Annotations["dash0.com/configuration-revision"] != previousRevision
	}, 10*time.Second, 100*time.Millisecond, "Secret updates must restart the proxy without a configuration update")
	_, err = r.Reconcile(ctx, ctrl.Request{})
	require.NoError(t, err, "reconciling API-defaulted resources must succeed")
	require.NoError(t, admin.Delete(ctx, configuration))
	require.Eventually(t, func() bool {
		removed := true
		for _, component := range []string{"ebpf-profiler", "debuginfo-proxy"} {
			ds := &appsv1.DaemonSet{}
			if apierrors.IsNotFound(admin.Get(ctx, key(component), ds)) {
				continue
			}
			removed = false
			if ds.DeletionTimestamp != nil {
				// envtest has no garbage collector to complete foreground deletion.
				ds.Finalizers = nil
				require.NoError(t, admin.Update(ctx, ds))
			}
		}
		return removed
	}, 10*time.Second, 100*time.Millisecond)
}

func fixture(t *testing.T) (*profiling.Reconciler, *dash0v1alpha1.Dash0OperatorConfiguration) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, networkingv1.AddToScheme(scheme))
	require.NoError(t, dash0v1alpha1.AddToScheme(scheme))
	configuration := &dash0v1alpha1.Dash0OperatorConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "configuration"}, Spec: dash0v1alpha1.Dash0OperatorConfigurationSpec{
		Profiling: &dash0v1alpha1.Profiling{Enabled: ptr.To(true), EbpfProfiler: &dash0v1alpha1.EbpfProfiler{Enabled: ptr.To(true)}},
		Exports: []dash0common.Export{{Dash0: &dash0common.Dash0Configuration{Endpoint: "https://ingress.example:4317", Dataset: "primary",
			Authorization: dash0common.Authorization{SecretRef: &dash0common.SecretRef{Name: "authorization", Key: "token"}},
		}}},
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configuration,
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "authorization", Namespace: "operator"}, Data: map[string][]byte{"token": []byte("backend-token")}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "operator", Labels: map[string]string{"owned-by": "user"}}},
	).Build()
	r := &profiling.Reconciler{Client: c, Scheme: scheme, OperatorDeployment: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "operator", Namespace: "operator", UID: "operator-deployment",
	}}, Config: profiling.Config{Namespace: "operator", NamePrefix: "test", Enabled: true, TelemetryCollectionEnabled: true,
		ProfilerImage: "ghcr.io/parca-dev/parca-agent:v0.51.2", ProxyImage: "debuginfo-proxy:test",
	}}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(configuration), configuration))
	return r, configuration
}

func reconcile(t *testing.T, r *profiling.Reconciler, configuration *dash0v1alpha1.Dash0OperatorConfiguration) {
	t.Helper()
	require.NoError(t, r.Update(context.Background(), configuration))
	_, err := r.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
}

func key(component string) client.ObjectKey {
	return client.ObjectKey{Namespace: "operator", Name: "test-" + component}
}
