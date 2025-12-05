// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bep/debounce"
	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

type ResourceRequirementsWithGoMemLimit struct {
	Limits     corev1.ResourceList `json:"limits,omitempty"`
	Requests   corev1.ResourceList `json:"requests,omitempty"`
	GoMemLimit string              `json:"gomemlimit,omitempty"`
}

// ExtraConfig holds the additional configuration values for the operator, which the operator reads from
// /etc/config/extra.yaml at startup, mostly Kubernetes-related settings like resource requests, resource limits, the
// filelog offset volume and tolerations for the collector daemonset.
type ExtraConfig struct {
	InstrumentationInitContainerResources ResourceRequirementsWithGoMemLimit `json:"initContainerResources"`

	CollectorFilelogOffsetStorageVolume *corev1.Volume `json:"collectorFilelogOffsetStorageVolume,omitempty"`

	CollectorDaemonSetCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetCollectorContainerResources,omitempty"`
	CollectorDaemonSetConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetConfigurationReloaderContainerResources,omitempty"`
	CollectorDaemonSetFileLogOffsetSyncContainerResources     ResourceRequirementsWithGoMemLimit `json:"collectorDaemonSetFileLogOffsetSyncContainerResources,omitempty"`

	DaemonSetTolerations  []corev1.Toleration  `json:"daemonSetTolerations,omitempty"`
	DaemonSetNodeAffinity *corev1.NodeAffinity `json:"daemonSetNodeAffinity,omitempty"`

	CollectorDaemonSetPriorityClassName string `json:"collectorDaemonSetPriorityClassName,omitempty"`

	CollectorDeploymentCollectorContainerResources             ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentCollectorContainerResources,omitempty"`
	CollectorDeploymentConfigurationReloaderContainerResources ResourceRequirementsWithGoMemLimit `json:"collectorDeploymentConfigurationReloaderContainerResources,omitempty"`

	DeploymentTolerations  []corev1.Toleration  `json:"deploymentTolerations,omitempty"`
	DeploymentNodeAffinity *corev1.NodeAffinity `json:"deploymentNodeAffinity,omitempty"`

	CollectorDeploymentPriorityClassName string `json:"collectorDeploymentPriorityClassName,omitempty"`

	TargetAllocatorContainerResources corev1.ResourceRequirements `json:"targetAllocatorContainerResources,omitempty"`
	TargetAllocatorTolerations        []corev1.Toleration         `json:"targetAllocatorTolerations,omitempty"`
	TargetAllocatorNodeAffinity       *corev1.NodeAffinity        `json:"targetAllocatorNodeAffinity,omitempty"`
}

type ExtraConfigClient interface {
	UpdateExtraConfig(context.Context, ExtraConfig, *logr.Logger)
}

const (
	extraConfigDir  = "/etc/config"
	extraConfigFile = "/etc/config/extra.yaml"
)

var (
	ExtraConfigDefaults = ExtraConfig{
		InstrumentationInitContainerResources: ResourceRequirementsWithGoMemLimit{},
		CollectorDaemonSetCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "400MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		CollectorDaemonSetConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
			GoMemLimit: "8MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
		CollectorDaemonSetFileLogOffsetSyncContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			GoMemLimit: "24MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
		},
		CollectorDeploymentCollectorContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
			GoMemLimit: "400MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		CollectorDeploymentConfigurationReloaderContainerResources: ResourceRequirementsWithGoMemLimit{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
			GoMemLimit: "8MiB",
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("12Mi"),
			},
		},
	}
)

// ReadExtraConfigMap reads the config map content from the default location
func ReadExtraConfigMap() (ExtraConfig, error) {
	return readExtraConfigurationFromFile(extraConfigFile)
}

// readExtraConfigurationFromFile reads the config map content from the given file path.
func readExtraConfigurationFromFile(configurationFile string) (ExtraConfig, error) {
	if len(configurationFile) == 0 {
		return ExtraConfig{}, fmt.Errorf("filename is empty")
	}
	content, err := os.ReadFile(configurationFile)
	if err != nil {
		return ExtraConfig{}, fmt.Errorf("the configuration file (%s) is missing or cannot be opened %w", configurationFile, err)
	}

	extraConfig := &ExtraConfig{}
	if err = yaml.Unmarshal(content, extraConfig); err != nil {
		return ExtraConfig{}, fmt.Errorf("cannot unmarshal the configuration file %w", err)
	}
	applyDefaults(
		&extraConfig.InstrumentationInitContainerResources,
		&ExtraConfigDefaults.InstrumentationInitContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetCollectorContainerResources,
		&ExtraConfigDefaults.CollectorDaemonSetCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetConfigurationReloaderContainerResources,
		&ExtraConfigDefaults.CollectorDaemonSetConfigurationReloaderContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDaemonSetFileLogOffsetSyncContainerResources,
		&ExtraConfigDefaults.CollectorDaemonSetFileLogOffsetSyncContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDeploymentCollectorContainerResources,
		&ExtraConfigDefaults.CollectorDeploymentCollectorContainerResources,
	)
	applyDefaults(
		&extraConfig.CollectorDeploymentConfigurationReloaderContainerResources,
		&ExtraConfigDefaults.CollectorDeploymentConfigurationReloaderContainerResources,
	)
	return *extraConfig, nil
}

// applyDefaults sets default values for CPU, Memory, and Ephemeral Storage on requests and limits.
func applyDefaults(spec *ResourceRequirementsWithGoMemLimit, defaults *ResourceRequirementsWithGoMemLimit) {
	if spec == nil || defaults == nil {
		// if spec is a nil pointer, we have no target object to update
		// if defaults is a nil pointer, we have no source object to copy values from
		return
	}
	if defaults.Requests != nil {
		if spec.Requests == nil {
			spec.Requests = make(corev1.ResourceList)
		}
		applyDefaultValues(spec.Requests, defaults.Requests)
	}
	if defaults.Limits != nil {
		if spec.Limits == nil {
			spec.Limits = make(corev1.ResourceList)
		}
		applyDefaultValues(spec.Limits, defaults.Limits)

	}
	if spec.GoMemLimit == "" && defaults.GoMemLimit != "" {
		spec.GoMemLimit = defaults.GoMemLimit
	}
}

// applyDefaultValues sets default values for CPU, Memory, and Ephemeral Storage on either requests or limits, expects
// both resourceList and defaults to not be nil.
func applyDefaultValues(resourceList corev1.ResourceList, defaults corev1.ResourceList) {
	if resourceList.Cpu().IsZero() && !defaults.Cpu().IsZero() {
		resourceList[corev1.ResourceCPU] = *defaults.Cpu()
	}
	if resourceList.Memory().IsZero() && !defaults.Memory().IsZero() {
		resourceList[corev1.ResourceMemory] = *defaults.Memory()
	}
	if resourceList.StorageEphemeral().IsZero() && !defaults.StorageEphemeral().IsZero() {
		resourceList[corev1.ResourceEphemeralStorage] = *defaults.StorageEphemeral()
	}

}

func (rr ResourceRequirementsWithGoMemLimit) ToResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits:   rr.Limits,
		Requests: rr.Requests,
	}
}

type ExtraConfigWatcher struct {
	watcher *fsnotify.Watcher
	clients []ExtraConfigClient
}

func NewExtraConfigWatcher() *ExtraConfigWatcher {
	return &ExtraConfigWatcher{
		clients: make([]ExtraConfigClient, 0),
	}
}

func (w *ExtraConfigWatcher) StartWatch(logger *logr.Logger) error {
	return w.watchConfigurationDirectory(extraConfigDir, extraConfigFile, logger)
}

func (w *ExtraConfigWatcher) AddClient(client ExtraConfigClient) {
	w.clients = append(w.clients, client)
}

func (w *ExtraConfigWatcher) watchConfigurationDirectory(
	configurationDir string,
	extraConfigFile string,
	setupLogger *logr.Logger,
) error {
	var err error
	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		setupLogger.Error(err, "cannot establish file watcher")
		return err
	}

	// When the config map is changed, and Kubernetes finally updates it, there a couple of file system events
	// (copying to old file to a backup location, creating the new content as a temporary file, renaming that to
	// the actual name, etc.). Therefore, we debounce these events and only update clients after a debounce timeout of
	// 1 second.
	debouncedFileWatchEvents := debounce.New(1 * time.Second)
	go func() {
		for {
			select {
			case _, ok := <-w.watcher.Events:
				if !ok {
					return
				}
				ctx := context.Background()
				logger := log.FromContext(ctx)
				debouncedFileWatchEvents(func() {
					extraConfig, err := readExtraConfigurationFromFile(extraConfigFile)
					if err != nil {
						logger.Error(err, "cannot read extra config map file after it has been updated")
						return
					}
					for _, client := range w.clients {
						client.UpdateExtraConfig(ctx, extraConfig, &logger)
					}
				})
			case fsnotifyErr, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				ctx := context.Background()
				logger := log.FromContext(ctx)
				logger.Error(fsnotifyErr, "extra config map file watcher error")
			}
		}
	}()

	// Watch the parent directory of the extra config map file. Watching the file directly only works once, since
	// updating the file include a remove operation on the file system level, and fsnotify removes the watch silently
	// once the path added via watcher.Add is removed.
	// Also, firing the fsnotify event can take a minute or a bit more after the config map has been changed.
	if err = w.watcher.Add(configurationDir); err != nil {
		setupLogger.Error(err, "cannot add watcher for extra config directory", "directory", configurationDir)
		return err
	}
	return nil
}

func (w *ExtraConfigWatcher) CloseWatch() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}
