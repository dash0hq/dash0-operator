// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package profiling

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/collectors/otelcolresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	profilerComponent       = "ebpf-profiler"
	proxyComponent          = "debuginfo-proxy"
	componentLabel          = "app.kubernetes.io/component"
	instanceLabel           = "app.kubernetes.io/instance"
	sourceRoutingAnnotation = "dash0.com/profiler-source-routing"
)

// Config contains installation-level settings for the managed profiler and proxy.
type Config struct {
	Namespace                  string
	NamePrefix                 string
	Enabled                    bool
	TelemetryCollectionEnabled bool
	ProfilerImage              string
	ProfilerImagePullPolicy    corev1.PullPolicy
	ProxyImage                 string
	ProxyImagePullPolicy       corev1.PullPolicy
	IsOpenShift                bool
}

func (c Config) name(component string) string { return c.NamePrefix + "-" + component }

func (c Config) labels(component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "dash0-operator",
		"app.kubernetes.io/managed-by": "dash0-operator",
		instanceLabel:                  c.NamePrefix,
		componentLabel:                 component,
	}
}

func (c Config) metadata(component string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: c.name(component), Namespace: c.Namespace, Labels: c.labels(component)}
}

func (c Config) profiler(configuration *dash0v1alpha1.Dash0OperatorConfiguration, uploads bool) []client.Object {
	spec := configuration.Spec.Profiling.EbpfProfiler
	settings := util.EffectiveExtraConfig(util.ExtraConfigDefaults, util.ExtraConfigDefaults, configuration, nil)
	tracers := spec.Tracers
	if len(tracers) == 0 {
		tracers = []string{"python", "hotspot", "php", "ruby", "perl", "v8", "dotnet", "beam", "luajit", "labels", "cuda"}
	}
	args := []string{
		"--node=$(NODE_NAME)", "--metadata-discovery=local", "--remote-store-format=otlp",
		"--metadata-external-labels=parca.agent=true",
		"--remote-store-insecure", "--remote-store-address=" + otelcolresources.ServiceName(c.NamePrefix) + "." + c.Namespace +
			".svc:4317",
		"--analytics-opt-out", "--telemetry-disable-panic-reporting", "--http-address=",
		"--tracers=" + strings.Join(tracers, ","),
		"--profiling-cpu-sampling-frequency=" + strconv.Itoa(int(ptr.Deref(spec.CPUSamplingFrequency, 19))),
		"--profiling-enable-error-frames=" + strconv.FormatBool(ptr.Deref(spec.EnableErrorFrames, false)),
		"--bpf-map-scale-factor=" + strconv.Itoa(int(ptr.Deref(spec.MapScaleFactor, 0))),
		"--log-level=" + defaultLogLevel(spec.LogLevel),
	}
	if spec.PIDNamespaceTranslationMode != "" {
		args = append(args, "--pid-namespace-translation-mode="+spec.PIDNamespaceTranslationMode)
	}
	if threshold := ptr.Deref(spec.OffCPUThreshold, 0); threshold > 0 {
		args = append(args, "--off-cpu-threshold="+strconv.FormatFloat(threshold, 'g', -1, 64))
	}
	threshold, interval := int32(100), time.Minute
	if sampling := spec.ProbabilisticSampling; sampling != nil {
		threshold = ptr.Deref(sampling.Threshold, 100)
		if sampling.Interval.Duration != 0 {
			interval = sampling.Interval.Duration
		}
	}
	args = append(args, "--profiling-probabilistic-threshold="+strconv.Itoa(int(threshold)),
		"--profiling-probabilistic-interval="+interval.String())
	if uploads {
		args = append(args, "--debuginfo-address="+c.name(proxyComponent)+"."+c.Namespace+".svc:7070", "--debuginfo-insecure")
		if spec.Debuginfo != nil {
			args = append(args,
				"--debuginfo-upload-max-parallel="+strconv.Itoa(int(ptr.Deref(spec.Debuginfo.MaxConcurrentUploads, 25))),
				"--debuginfo-upload-queue-size="+strconv.Itoa(int(ptr.Deref(spec.Debuginfo.QueueSize, 4096))))
		}
	} else {
		args = append(args, "--debuginfo-upload-disable")
	}
	env := []corev1.EnvVar{{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
	}}, {Name: "GOMEMLIMIT", Value: settings.EbpfProfilerContainerResources.EffectiveGoMemLimitPercent(util.GoMemLimitDefaultPercent)}}
	container := corev1.Container{
		Name: profilerComponent, Image: c.ProfilerImage, ImagePullPolicy: c.ProfilerImagePullPolicy, Args: args, Env: env,
		Resources:       settings.EbpfProfilerContainerResources.ToResourceRequirements(),
		SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true), RunAsUser: ptr.To(int64(0)), RunAsNonRoot: ptr.To(false)},
		VolumeMounts: []corev1.VolumeMount{{Name: "proc", MountPath: "/proc", ReadOnly: true},
			{Name: "sys", MountPath: "/sys", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}},
	}
	pod := corev1.PodSpec{
		ServiceAccountName: c.name(profilerComponent), AutomountServiceAccountToken: ptr.To(false), HostPID: true,
		Containers:   []corev1.Container{container},
		NodeSelector: map[string]string{"kubernetes.io/os": "linux"},
		Affinity:     &corev1.Affinity{NodeAffinity: settings.EbpfProfilerNodeAffinity},
		Tolerations:  settings.EbpfProfilerTolerations, PriorityClassName: settings.EbpfProfilerPriorityClassName,
		Volumes: []corev1.Volume{
			{Name: "proc", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/proc"}}},
			{Name: "sys", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys"}}},
			{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
	}
	metadata := c.metadata(profilerComponent)
	metadata.Labels = mergeLabels(settings.EbpfProfilerLabels, metadata.Labels)
	metadata.Annotations = make(map[string]string, len(settings.EbpfProfilerAnnotations)+1)
	maps.Copy(metadata.Annotations, settings.EbpfProfilerAnnotations)
	metadata.Annotations[sourceRoutingAnnotation] = otelcolresources.ProfilerSourceRoutingKey
	ds := &appsv1.DaemonSet{ObjectMeta: metadata, Spec: appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: c.labels(profilerComponent)},
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
			Labels:      mergeLabels(settings.EbpfProfilerPodLabels, c.labels(profilerComponent)),
			Annotations: settings.EbpfProfilerPodAnnotations,
		}, Spec: pod},
	}}
	return []client.Object{&corev1.ServiceAccount{ObjectMeta: c.metadata(profilerComponent), AutomountServiceAccountToken: ptr.To(false)}, ds}
}

func (c Config) proxy(configuration *dash0v1alpha1.Dash0OperatorConfiguration, config proxyConfiguration, revision string) []client.Object {
	settings := util.EffectiveExtraConfig(util.ExtraConfigDefaults, util.ExtraConfigDefaults, configuration, nil)
	security := &corev1.SecurityContext{
		RunAsNonRoot: ptr.To(true), AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true),
		Capabilities:   &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	probe := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{GRPC: &corev1.GRPCAction{Port: proxyPort}}, PeriodSeconds: 10}
	env := slices.Clone(config.env)
	env = append(env, corev1.EnvVar{Name: "GOMEMLIMIT", Value: settings.DebuginfoProxyContainerResources.EffectiveGoMemLimitPercent(util.GoMemLimitDefaultPercent)})
	metadata := c.metadata(proxyComponent)
	metadata.Labels = mergeLabels(settings.DebuginfoProxyLabels, metadata.Labels)
	metadata.Annotations = settings.DebuginfoProxyAnnotations
	podAnnotations := maps.Clone(settings.DebuginfoProxyPodAnnotations)
	if podAnnotations == nil {
		podAnnotations = map[string]string{}
	}
	podAnnotations["dash0.com/configuration-revision"] = revision
	daemonSet := &appsv1.DaemonSet{ObjectMeta: metadata, Spec: appsv1.DaemonSetSpec{
		UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
			Type: appsv1.RollingUpdateDaemonSetStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDaemonSet{
				MaxUnavailable: ptr.To(intstr.FromInt32(1)), MaxSurge: ptr.To(intstr.FromInt32(0)),
			},
		},
		Selector: &metav1.LabelSelector{MatchLabels: c.labels(proxyComponent)},
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
			Labels: mergeLabels(settings.DebuginfoProxyPodLabels, c.labels(proxyComponent)), Annotations: podAnnotations,
		}, Spec: corev1.PodSpec{
			ServiceAccountName: c.name(proxyComponent), AutomountServiceAccountToken: ptr.To(false),
			NodeSelector:    map[string]string{"kubernetes.io/os": "linux"},
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: util.RunAsID(c.IsOpenShift, 65532)},
			Affinity:        &corev1.Affinity{NodeAffinity: settings.DebuginfoProxyNodeAffinity},
			Tolerations:     settings.DebuginfoProxyTolerations, PriorityClassName: settings.DebuginfoProxyPriorityClassName,
			Containers: []corev1.Container{{Name: proxyComponent, Image: c.ProxyImage, ImagePullPolicy: c.ProxyImagePullPolicy,
				Args: []string{"--config=/etc/debuginfo-proxy/config.yaml"}, Env: env,
				Resources: settings.DebuginfoProxyContainerResources.ToResourceRequirements(), SecurityContext: security,
				Ports:          []corev1.ContainerPort{{Name: "grpc", ContainerPort: proxyPort}},
				ReadinessProbe: probe.DeepCopy(), LivenessProbe: probe.DeepCopy(),
				VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/debuginfo-proxy", ReadOnly: true}},
			}},
			Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: c.name(proxyComponent)},
			}}}},
		}},
	}}
	return []client.Object{
		&corev1.ServiceAccount{ObjectMeta: c.metadata(proxyComponent), AutomountServiceAccountToken: ptr.To(false)},
		&corev1.ConfigMap{ObjectMeta: c.metadata(proxyComponent), Data: map[string]string{"config.yaml": config.yaml}},
		&networkingv1.NetworkPolicy{ObjectMeta: c.metadata(proxyComponent), Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: c.labels(proxyComponent)}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: c.labels(profilerComponent)}}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(proxyPort))}},
			}},
		}},
		daemonSet,
		&corev1.Service{ObjectMeta: c.metadata(proxyComponent), Spec: corev1.ServiceSpec{
			Selector:              c.labels(proxyComponent),
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
			Ports:                 []corev1.ServicePort{{Name: "grpc", Port: proxyPort, TargetPort: intstr.FromInt32(proxyPort)}},
		}},
	}
}

func mergeLabels(custom, required map[string]string) map[string]string {
	merged := maps.Clone(custom)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, required)
	return merged
}

func defaultLogLevel(level string) string {
	if level == "" {
		return "info"
	}
	return level
}

func (c Config) validateImages(uploads bool) error {
	if c.ProfilerImage == "" {
		return fmt.Errorf("configure a compatible Parca agent image before enabling eBPF profiling")
	}
	if uploads && c.ProxyImage == "" {
		return fmt.Errorf("configure the debuginfo proxy image before enabling uploads")
	}
	return nil
}
