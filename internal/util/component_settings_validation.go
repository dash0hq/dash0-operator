// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

// The functions in this file validate the settings in spec.components the way the Kubernetes API server validates the
// pod templates of the workloads they end up in (see pkg/apis/core/validation/validation.go in kubernetes/kubernetes),
// so that an invalid setting does not make the API server reject the operator's update of a workload.

// validateOperatorConfigurationComponents validates spec.components of the operator configuration resource, which may
// be nil.
func validateOperatorConfigurationComponents(
	components *dash0v1alpha1.OperatorConfigurationComponents,
) field.ErrorList {
	if components == nil {
		return nil
	}
	path := field.NewPath(componentSettingsPath)
	var errs field.ErrorList
	if daemonSet := components.DaemonSetCollector; daemonSet != nil {
		errs = append(errs,
			validateWorkloadSettings(path.Child(componentDaemonSetCollector), daemonSet.WorkloadSettings)...)
	}
	if deployment := components.DeploymentCollector; deployment != nil {
		errs = append(errs,
			validateWorkloadSettings(path.Child(componentDeploymentCollector), deployment.WorkloadSettings)...)
	}
	if targetAllocator := components.TargetAllocator; targetAllocator != nil {
		errs = append(errs,
			validateWorkloadSettings(path.Child(componentTargetAllocator), targetAllocator.WorkloadSettings)...)
	}
	if agent0Connector := components.Agent0Connector; agent0Connector != nil {
		errs = append(errs,
			validateWorkloadSettings(path.Child(componentAgent0Connector), agent0Connector.WorkloadSettings)...)
	}
	return errs
}

// validateSignalControlComponents validates spec.components of the Signal Control resource, which may be nil.
func validateSignalControlComponents(components *dash0v1alpha1.SignalControlComponents) field.ErrorList {
	if components == nil {
		return nil
	}
	path := field.NewPath(componentSettingsPath)
	var errs field.ErrorList
	if collector := components.Collector; collector != nil {
		errs = append(errs,
			validateWorkloadSettings(path.Child(componentSignalControlCollector), collector.WorkloadSettings)...)
	}
	if edgeProxy := components.EdgeProxy; edgeProxy != nil {
		errs = append(errs, validateWorkloadSettings(path.Child(componentEdgeProxy), edgeProxy.WorkloadSettings)...)
	}
	return errs
}

func validateWorkloadSettings(path *field.Path, settings dash0v1alpha1.WorkloadSettings) field.ErrorList {
	errs := validateLabels(path.Child("labels"), settings.Labels)
	errs = append(errs, validateAnnotations(path.Child("annotations"), settings.Annotations)...)
	errs = append(errs, validateLabels(path.Child("podLabels"), settings.PodLabels)...)
	errs = append(errs, validateAnnotations(path.Child("podAnnotations"), settings.PodAnnotations)...)
	return errs
}

func validateLabels(path *field.Path, labels map[string]string) field.ErrorList {
	return metav1validation.ValidateLabels(labels, path)
}

func validateAnnotations(path *field.Path, annotations map[string]string) field.ErrorList {
	return apimachineryvalidation.ValidateAnnotations(annotations, path)
}
