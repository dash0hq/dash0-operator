// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/util/resources"
)

const (
	filelogOffsetStorageVolumeName        = "filelogreceiver-offsets"
	fileLogOffsetMigrateInitContainerName = "filelog-offset-migrate"
	filelogOffsetProbePodSuffix           = "-filelog-offset-probe-"
	usingFilelogOffsetStorageMsg          = "Storing the filelog receiver offsets %s."
	// superPrivilegedContainerSELinuxType is the SELinux type that is not confined by the container policy. The
	// operator sets it on the collector daemonset pods on OpenShift by default (see util.ExtraConfigDefaultsFor); without
	// it, the default type container_t cannot write to a hostPath directory, since the kubelet does not relabel
	// hostPath volumes.
	superPrivilegedContainerSELinuxType = "spc_t"
)

type hostPathProbeOutcome int

const (
	hostPathAdmitted hostPathProbeOutcome = iota
	hostPathDenied
	hostPathProbeInconclusive
)

// hostPathProbeResult is a conclusive outcome of the hostPath admission probe for a specific host path.
type hostPathProbeResult struct {
	hostPath string
	outcome  hostPathProbeOutcome
	detail   string
}

// admissionDenialPatterns match the messages with which the API server reports that an admission controller has
// denied a request. Only these count as a conclusive denial of the hostPath admission probe.
var admissionDenialPatterns = []*regexp.Regexp{
	// validating and mutating admission webhooks (OPA gatekeeper, Kyverno, ...), see ToStatusErr in
	// k8s.io/apiserver/pkg/admission/plugin/webhook/errors
	regexp.MustCompile(`admission webhook ".*" denied the request`),
	// pod security admission, see k8s.io/pod-security-admission/admission
	regexp.MustCompile(`violates PodSecurity "`),
	// OpenShift security context constraints
	regexp.MustCompile(`unable to validate against any security context constraint`),
	// validating admission policies, see k8s.io/apiserver/pkg/admission/plugin/policy/validating
	regexp.MustCompile(`ValidatingAdmissionPolicy '.*' (with binding '.*' )?denied request`),
}

type filelogOffsetStorageKind int

const (
	filelogOffsetStorageNoDaemonSet filelogOffsetStorageKind = iota
	filelogOffsetStorageConfigMap
	filelogOffsetStorageHostPath
	filelogOffsetStorageOtherVolume
)

// liveFilelogOffsetStorage describes how the collector daemonset that currently exists in the cluster stores the
// filelog receiver offsets.
type liveFilelogOffsetStorage struct {
	kind                    filelogOffsetStorageKind
	hasMigrateInitContainer bool
	rolloutComplete         bool
}

func (l liveFilelogOffsetStorage) usesVolume() bool {
	return l.kind == filelogOffsetStorageHostPath || l.kind == filelogOffsetStorageOtherVolume
}

/*
resolveFilelogOffsetStorage decides where the collector daemonset persists the offsets of the filelog receiver and
applies the decision to config.

A volume that the user has configured always wins. Otherwise the storage mode from the extra config decides: configMap
and hostPath are used as they are. Mode auto uses the config map if the platform is known to reject writable hostPath
volumes (GKE Autopilot), or if the SELinux type of the collector pods likely cannot write to them (OpenShift, or nodes
that indicate SELinux, unless the pods run with spc_t). Otherwise a server-side dry run of a collector pod with the
hostPath volume decides: admitted means hostPath, a genuine admission denial means config map. Both outcomes are cached
for the lifetime of the process. An inconclusive dry run (API server errors, quota, RBAC, ...) never moves the daemonset
away from the storage it currently uses; without a daemonset, it means config map for now, the next reconciliation
probes again.

With a volume, the config map of the config map based storage stays in the desired state and the collector pods get an
init container that migrates the offsets from it, until the daemonset has completely rolled out with the volume, see
planFilelogOffsetsMigration.

Nothing is resolved if the desired state contains no collector daemonset.
*/
func (m *OTelColResourceManager) resolveFilelogOffsetStorage(
	ctx context.Context,
	config *oTelColConfig,
	extraConfig util.ExtraConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	config.OffsetStorageVolume = nil
	config.KeepFilelogOffsetsConfigMap = false
	config.MigrateFilelogOffsetsFromConfigMap = false
	if !config.deploysProducerCollectors() {
		return nil
	}

	live, err := m.readLiveFilelogOffsetStorage(ctx, config)
	if err != nil {
		return err
	}
	volume, decision := m.selectFilelogOffsetStorage(
		ctx,
		config,
		extraConfig,
		operatorConfigurationResource,
		live,
		logger,
	)
	config.OffsetStorageVolume = volume
	if volume != nil {
		if err = m.planFilelogOffsetsMigration(ctx, config, live); err != nil {
			return err
		}
	} else if live.usesVolume() {
		logger.Warn(fmt.Sprintf("The collector daemonset is switched from a volume to the config map %s for the "+
			"filelog receiver offsets. The offsets are not carried over from the volume to the config map, so log "+
			"records may be lost or sent twice while the collector pods are replaced.",
			FilelogReceiverOffsetsConfigMapName(config.NamePrefix)))
	}
	m.logFilelogOffsetStorageDecision(decision, logger)
	return nil
}

func (m *OTelColResourceManager) selectFilelogOffsetStorage(
	ctx context.Context,
	config *oTelColConfig,
	extraConfig util.ExtraConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	live liveFilelogOffsetStorage,
	logger logd.Logger,
) (*corev1.Volume, string) {
	if extraConfig.CollectorFilelogOffsetStorageVolume != nil {
		return extraConfig.CollectorFilelogOffsetStorageVolume,
			fmt.Sprintf("in the configured volume %s", extraConfig.CollectorFilelogOffsetStorageVolume.Name)
	}

	hostPathVolume := filelogOffsetHostPathVolume(extraConfig.CollectorFilelogOffsetHostPath, config.OperatorNamespace)
	hostPath := hostPathVolume.HostPath.Path
	inHostPath := fmt.Sprintf("in the hostPath volume %s on each node", hostPath)
	inConfigMap := fmt.Sprintf("in the config map %s", FilelogReceiverOffsetsConfigMapName(config.NamePrefix))
	switch extraConfig.CollectorFilelogOffsetStorageMode {
	case util.FilelogOffsetStorageModeConfigMap:
		return nil, inConfigMap + ", as requested by operator.collectors.filelogOffsetStorageMode=configMap"
	case util.FilelogOffsetStorageModeHostPath:
		return hostPathVolume, inHostPath + ", as requested by operator.collectors.filelogOffsetStorageMode=hostPath"
	}

	configMapBecause := func(reason string) (*corev1.Volume, string) {
		return nil, fmt.Sprintf("%s, since %s; set operator.collectors.filelogOffsetStorageMode=hostPath to use the "+
			"hostPath volume %s anyway", inConfigMap, reason, hostPath)
	}
	runsUnconfined := daemonSetRunsUnconfinedBySELinux(extraConfig)
	nodeSELinuxIndication := m.collectorConfig.NodeSELinuxIndication
	switch {
	case config.IsGkeAutopilot:
		return configMapBecause("GKE Autopilot does not admit writable hostPath volumes")
	case config.IsOpenShift && !runsUnconfined:
		return configMapBecause("on OpenShift, writing to a hostPath volume requires the SELinux type " +
			superPrivilegedContainerSELinuxType + " for the collector daemonset pods, which " +
			"operator.collectors.daemonSetSeLinuxOptions does not request")
	case nodeSELinuxIndication.Indication != "" && !runsUnconfined:
		return configMapBecause(fmt.Sprintf("%s, which indicates that the nodes confine containers with SELinux; "+
			"writing to a hostPath volume then requires the SELinux type %s for the collector daemonset pods, which "+
			"operator.collectors.daemonSetSeLinuxOptions does not request", nodeSELinuxIndication.Indication,
			superPrivilegedContainerSELinuxType))
	}

	var outcome hostPathProbeOutcome
	var detail string
	if nodeSELinuxIndication.InspectionFailed && !runsUnconfined {
		outcome = hostPathProbeInconclusive
		detail = "the nodes could not be inspected for SELinux at operator startup"
	} else {
		outcome, detail = m.probeFilelogOffsetHostPath(
			ctx,
			config,
			extraConfig,
			operatorConfigurationResource,
			hostPathVolume,
			logger,
		)
	}
	switch outcome {
	case hostPathAdmitted:
		return hostPathVolume, inHostPath
	case hostPathDenied:
		return configMapBecause("the cluster does not admit a collector pod with a writable hostPath volume: " + detail)
	default:
		m.warnAboutInconclusiveHostPathProbe(hostPath, detail, logger)
		if live.kind == filelogOffsetStorageHostPath {
			return hostPathVolume, inHostPath + ", which the collector daemonset already uses; it could not be " +
				"determined whether the cluster admits it"
		}
		return nil, inConfigMap + " for now, since it could not be determined whether the cluster admits a collector " +
			"pod with a writable hostPath volume"
	}
}

func daemonSetRunsUnconfinedBySELinux(extraConfig util.ExtraConfig) bool {
	return extraConfig.DaemonSetSELinuxOptions != nil &&
		extraConfig.DaemonSetSELinuxOptions.Type == superPrivilegedContainerSELinuxType
}

// filelogOffsetHostPathVolume returns the hostPath volume the operator uses for the filelog receiver offsets. The
// operator namespace is part of the path, so that multiple operator installations in one cluster do not share their
// offsets.
func filelogOffsetHostPathVolume(hostPathRoot string, operatorNamespace string) *corev1.Volume {
	if hostPathRoot == "" {
		hostPathRoot = util.DefaultFilelogOffsetHostPath
	}
	return &corev1.Volume{
		Name: filelogOffsetStorageVolumeName,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: path.Join(hostPathRoot, operatorNamespace),
				Type: new(corev1.HostPathDirectoryOrCreate),
			},
		},
	}
}

// probeFilelogOffsetHostPath checks whether the cluster admits the collector daemonset pod with the hostPath volume
// for the filelog receiver offsets, via a server-side dry run of a pod with the daemonset's pod template. The dry run
// goes through pod security admission, validating admission policies and all admission webhooks (OPA gatekeeper,
// Kyverno, ...), without creating anything. Since the API server rejects a pod whose service account does not exist,
// the service account of the daemonset is created first if the operator manages it. A conclusive outcome is cached
// for the lifetime of the process.
func (m *OTelColResourceManager) probeFilelogOffsetHostPath(
	ctx context.Context,
	config *oTelColConfig,
	extraConfig util.ExtraConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	hostPathVolume *corev1.Volume,
	logger logd.Logger,
) (hostPathProbeOutcome, string) {
	hostPath := hostPathVolume.HostPath.Path
	if cached := m.filelogOffsetHostPathProbeResult.Load(); cached != nil && cached.hostPath == hostPath {
		return cached.outcome, cached.detail
	}

	if err := m.ensureDaemonSetServiceAccountExists(ctx, config, operatorConfigurationResource, logger); err != nil {
		return hostPathProbeInconclusive, fmt.Sprintf("the service account %s of the collector daemonset is not "+
			"available: %v", config.collectorDaemonSetServiceAccountName(), err)
	}

	probeConfig := *config
	probeConfig.OffsetStorageVolume = hostPathVolume
	probeConfig.MigrateFilelogOffsetsFromConfigMap = true
	daemonSet, err := assembleCollectorDaemonSet(&probeConfig, extraConfig)
	if err != nil {
		return hostPathProbeInconclusive, fmt.Sprintf("cannot assemble the collector daemonset: %v", err)
	}
	probePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: DaemonSetName(config.NamePrefix) + filelogOffsetProbePodSuffix,
			Namespace:    config.OperatorNamespace,
			Labels:       daemonSet.Spec.Template.Labels,
		},
		Spec: *daemonSet.Spec.Template.Spec.DeepCopy(),
	}

	err = m.Create(ctx, probePod, client.DryRunAll)
	outcome := classifyHostPathProbeError(err)
	if outcome == hostPathProbeInconclusive {
		return outcome, err.Error()
	}
	var detail string
	if err != nil {
		detail = err.Error()
	}
	m.filelogOffsetHostPathProbeResult.Store(&hostPathProbeResult{hostPath: hostPath, outcome: outcome, detail: detail})
	m.lastInconclusiveHostPathProbe.Store(nil)
	return outcome, detail
}

// ensureDaemonSetServiceAccountExists creates the service account of the collector daemonset, in the same way as the
// reconciliation of the collector resources does, or checks that it exists if it is provided externally.
func (m *OTelColResourceManager) ensureDaemonSetServiceAccountExists(
	ctx context.Context,
	config *oTelColConfig,
	operatorConfigurationResource *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) error {
	if config.usesExternalDaemonSetServiceAccount() {
		return m.Get(
			ctx,
			client.ObjectKey{Namespace: config.OperatorNamespace, Name: config.DaemonSetServiceAccountName},
			&corev1.ServiceAccount{},
		)
	}
	serviceAccount := addCommonMetadata(assembleServiceAccountForDaemonSet(config)).object
	if err := resources.SetOwnerReference(
		m.operatorManagerDeployment, operatorConfigurationResource, m.scheme, serviceAccount, logger,
	); err != nil {
		return err
	}
	_, _, err := m.createOrUpdateResource(ctx, serviceAccount, logger)
	return err
}

// classifyHostPathProbeError maps the error of the dry run to an outcome. Only a denial by an admission controller is
// conclusive; every other error (the service account does not exist, a resource quota is exceeded, the operator lacks
// permissions, timeouts, an admission webhook that does not support dry runs or cannot be reached, ...) says nothing
// about whether the hostPath volume would be admitted.
func classifyHostPathProbeError(err error) hostPathProbeOutcome {
	if err == nil {
		return hostPathAdmitted
	}
	message := err.Error()
	for _, pattern := range admissionDenialPatterns {
		if pattern.MatchString(message) {
			return hostPathDenied
		}
	}
	return hostPathProbeInconclusive
}

// warnAboutInconclusiveHostPathProbe logs an inconclusive outcome of the hostPath admission probe, but only if it
// differs from the previous inconclusive outcome, so that a persistently inconclusive probe does not log on every
// reconciliation.
func (m *OTelColResourceManager) warnAboutInconclusiveHostPathProbe(
	hostPath string,
	detail string,
	logger logd.Logger,
) {
	previous := m.lastInconclusiveHostPathProbe.Swap(&detail)
	if previous != nil && *previous == detail {
		return
	}
	logger.Warn("It could not be determined whether the cluster admits a collector pod with a writable hostPath "+
		"volume for the filelog receiver offsets. The collector daemonset keeps the storage it currently uses (the "+
		"config map, if there is no daemonset yet), the check is repeated on the next reconciliation.",
		"hostPath", hostPath, "reason", detail)
}

func (m *OTelColResourceManager) readLiveFilelogOffsetStorage(
	ctx context.Context,
	config *oTelColConfig,
) (liveFilelogOffsetStorage, error) {
	daemonSet := &appsv1.DaemonSet{}
	if err := m.Get(
		ctx,
		client.ObjectKey{Namespace: config.OperatorNamespace, Name: DaemonSetName(config.NamePrefix)},
		daemonSet,
	); err != nil {
		if apierrors.IsNotFound(err) {
			return liveFilelogOffsetStorage{kind: filelogOffsetStorageNoDaemonSet}, nil
		}
		return liveFilelogOffsetStorage{}, fmt.Errorf("cannot read the collector daemonset: %w", err)
	}
	return describeLiveFilelogOffsetStorage(daemonSet), nil
}

func describeLiveFilelogOffsetStorage(daemonSet *appsv1.DaemonSet) liveFilelogOffsetStorage {
	podSpec := daemonSet.Spec.Template.Spec
	live := liveFilelogOffsetStorage{
		hasMigrateInitContainer: slices.ContainsFunc(podSpec.InitContainers, func(c corev1.Container) bool {
			return c.Name == fileLogOffsetMigrateInitContainerName
		}),
		rolloutComplete: daemonSetRolloutIsComplete(daemonSet),
	}
	offsetsVolume := findFilelogOffsetsVolume(podSpec)
	switch {
	case offsetsVolume == nil || offsetsVolume.EmptyDir != nil:
		live.kind = filelogOffsetStorageConfigMap
	case offsetsVolume.HostPath != nil:
		live.kind = filelogOffsetStorageHostPath
	default:
		live.kind = filelogOffsetStorageOtherVolume
	}
	return live
}

// findFilelogOffsetsVolume returns the volume that the collector container mounts for the filelog receiver offsets.
// A user-provided volume can have any name, so the volume is identified via its mount path.
func findFilelogOffsetsVolume(podSpec corev1.PodSpec) *corev1.Volume {
	volumeName := filelogOffsetStorageVolumeName
	for _, container := range podSpec.Containers {
		if container.Name != openTelemetryCollector {
			continue
		}
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.MountPath == offsetsDirPath {
				volumeName = volumeMount.Name
			}
		}
	}
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name == volumeName {
			return &podSpec.Volumes[i]
		}
	}
	return nil
}

// daemonSetRolloutIsComplete reports whether all pods of the daemonset run with its current pod template and are
// available.
func daemonSetRolloutIsComplete(daemonSet *appsv1.DaemonSet) bool {
	status := daemonSet.Status
	return status.ObservedGeneration == daemonSet.Generation &&
		status.UpdatedNumberScheduled == status.DesiredNumberScheduled &&
		status.NumberAvailable == status.DesiredNumberScheduled
}

/*
planFilelogOffsetsMigration decides, for a collector daemonset that stores the filelog receiver offsets in a volume,
whether the config map of the config map based storage stays in the desired state and whether the collector pods get
the init container that restores the offsets from that config map into the empty volume.

The config map is kept as long as it exists and the daemonset has not completely rolled out with a volume: collector
pods that still run with the config map based storage keep updating it until they are replaced, and their replacements
migrate the offsets. Once the rollout is complete, the config map is no longer desired, and is deleted. A fresh
installation with a volume neither creates the config map nor adds the init container. The init container stays as
long as the daemonset has it: it tolerates a missing config map, and removing it would restart all collector pods once
more. On GKE Autopilot, the init container is never added, since the allowlist of the collector daemonset does not
permit it; the offsets are not carried over to the volume there.
*/
func (m *OTelColResourceManager) planFilelogOffsetsMigration(
	ctx context.Context,
	config *oTelColConfig,
	live liveFilelogOffsetStorage,
) error {
	configMapExists, err := m.filelogOffsetsConfigMapExists(ctx, config)
	if err != nil {
		return err
	}
	migrationPending := configMapExists && (!live.usesVolume() || !live.rolloutComplete)
	config.KeepFilelogOffsetsConfigMap = migrationPending
	config.MigrateFilelogOffsetsFromConfigMap = !config.IsGkeAutopilot &&
		(migrationPending || live.hasMigrateInitContainer)
	return nil
}

func (m *OTelColResourceManager) filelogOffsetsConfigMapExists(
	ctx context.Context,
	config *oTelColConfig,
) (bool, error) {
	if err := m.Get(
		ctx,
		client.ObjectKey{
			Namespace: config.OperatorNamespace,
			Name:      FilelogReceiverOffsetsConfigMapName(config.NamePrefix),
		},
		&corev1.ConfigMap{},
	); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("cannot read the filelog receiver offsets config map: %w", err)
	}
	return true, nil
}

// logFilelogOffsetStorageDecision logs where the offsets are stored, once per process and again whenever the decision
// changes, for example after the storage mode has been changed in the extra config map.
func (m *OTelColResourceManager) logFilelogOffsetStorageDecision(decision string, logger logd.Logger) {
	previous := m.lastFilelogOffsetStorageDecision.Swap(&decision)
	if previous != nil && *previous == decision {
		return
	}
	logger.Info(fmt.Sprintf(usingFilelogOffsetStorageMsg, decision))
}
