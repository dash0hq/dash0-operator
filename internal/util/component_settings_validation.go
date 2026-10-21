// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"regexp"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
)

// The functions in this file validate the settings in spec.components the way the Kubernetes API server validates the
// pod templates of the workloads they end up in (see pkg/apis/core/validation/validation.go in kubernetes/kubernetes),
// so that an invalid setting does not make the API server reject the operator's update of a workload.

const (
	sysctlSegmentFmt    = "[a-z0-9]([-_a-z0-9]*[a-z0-9])?"
	sysctlNameMaxLength = 253
)

var (
	supportedTaintEffects = []corev1.TaintEffect{
		corev1.TaintEffectNoSchedule,
		corev1.TaintEffectPreferNoSchedule,
		corev1.TaintEffectNoExecute,
	}

	sysctlNameRegexp = regexp.MustCompile("^(" + sysctlSegmentFmt + "[\\./])*" + sysctlSegmentFmt + "$")
)

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
		p := path.Child(componentDaemonSetCollector)
		errs = append(errs, validateWorkloadSettings(p, daemonSet.WorkloadSettings)...)
		errs = append(errs, validateCollectorPodSettings(p, daemonSet.CollectorPodSettings)...)
	}
	if deployment := components.DeploymentCollector; deployment != nil {
		p := path.Child(componentDeploymentCollector)
		errs = append(errs, validateWorkloadSettings(p, deployment.WorkloadSettings)...)
		errs = append(errs, validateCollectorPodSettings(p, deployment.CollectorPodSettings)...)
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
		p := path.Child(componentSignalControlCollector)
		errs = append(errs, validateWorkloadSettings(p, collector.WorkloadSettings)...)
		errs = append(errs, validateCollectorPodSettings(p, collector.CollectorPodSettings)...)
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
	errs = append(errs, validateTolerations(path.Child("tolerations"), settings.Tolerations)...)
	if settings.NodeAffinity != nil {
		errs = append(errs, validateNodeAffinity(path.Child("nodeAffinity"), settings.NodeAffinity)...)
	}
	return errs
}

func validateCollectorPodSettings(path *field.Path, settings dash0v1alpha1.CollectorPodSettings) field.ErrorList {
	errs := validateSysctls(path.Child("sysctls"), settings.Sysctls)
	if settings.PriorityClassName != "" {
		errs = append(errs, validatePriorityClassName(path.Child("priorityClassName"), settings.PriorityClassName)...)
	}
	return errs
}

func validateLabels(path *field.Path, labels map[string]string) field.ErrorList {
	return metav1validation.ValidateLabels(labels, path)
}

func validateAnnotations(path *field.Path, annotations map[string]string) field.ErrorList {
	return apimachineryvalidation.ValidateAnnotations(annotations, path)
}

func validateTolerations(path *field.Path, tolerations []corev1.Toleration) field.ErrorList {
	var errs field.ErrorList
	for i, toleration := range tolerations {
		tolerationPath := path.Index(i)
		if toleration.Key != "" {
			errs = append(errs, metav1validation.ValidateLabelName(toleration.Key, tolerationPath.Child("key"))...)
		} else if toleration.Operator != corev1.TolerationOpExists {
			errs = append(errs, field.Invalid(tolerationPath.Child("operator"), toleration.Operator,
				"must be Exists when key is empty"))
		}
		if toleration.TolerationSeconds != nil && toleration.Effect != corev1.TaintEffectNoExecute {
			errs = append(errs, field.Invalid(tolerationPath.Child("effect"), toleration.Effect,
				"must be NoExecute when tolerationSeconds is set"))
		}
		switch toleration.Operator {
		case corev1.TolerationOpEqual, "":
			for _, msg := range validation.IsValidLabelValue(toleration.Value) {
				errs = append(errs, field.Invalid(tolerationPath.Child("value"), toleration.Value, msg))
			}
		case corev1.TolerationOpExists:
			if toleration.Value != "" {
				errs = append(errs, field.Invalid(tolerationPath.Child("value"), toleration.Value,
					"must be empty when operator is Exists"))
			}
		default:
			// This includes Lt and Gt, which the API server only accepts depending on its configuration.
			errs = append(errs, field.NotSupported(tolerationPath.Child("operator"), toleration.Operator,
				[]corev1.TolerationOperator{corev1.TolerationOpEqual, corev1.TolerationOpExists}))
		}
		if toleration.Effect != "" && !slices.Contains(supportedTaintEffects, toleration.Effect) {
			errs = append(errs, field.NotSupported(tolerationPath.Child("effect"), toleration.Effect, supportedTaintEffects))
		}
	}
	return errs
}

func validateNodeAffinity(path *field.Path, nodeAffinity *corev1.NodeAffinity) field.ErrorList {
	var errs field.ErrorList
	// Unlike the API server, accept required node affinity without node selector terms, which removes the default.
	if required := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution; required != nil {
		termsPath := path.Child("requiredDuringSchedulingIgnoredDuringExecution", "nodeSelectorTerms")
		for i, term := range required.NodeSelectorTerms {
			errs = append(errs, validateNodeSelectorTerm(termsPath.Index(i), term, true)...)
		}
	}
	for i, term := range nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		termPath := path.Child("preferredDuringSchedulingIgnoredDuringExecution").Index(i)
		if term.Weight < 1 || term.Weight > 100 {
			errs = append(errs, field.Invalid(termPath.Child("weight"), term.Weight, "must be in the range 1-100"))
		}
		// Like the API server, accept label values in preferred terms that are invalid as label values.
		errs = append(errs, validateNodeSelectorTerm(termPath.Child("preference"), term.Preference, false)...)
	}
	return errs
}

func validateNodeSelectorTerm(
	path *field.Path,
	term corev1.NodeSelectorTerm,
	validateLabelValues bool,
) field.ErrorList {
	var errs field.ErrorList
	for i, requirement := range term.MatchExpressions {
		requirementPath := path.Child("matchExpressions").Index(i)
		valuesPath := requirementPath.Child("values")
		switch requirement.Operator {
		case corev1.NodeSelectorOpIn, corev1.NodeSelectorOpNotIn:
			if len(requirement.Values) == 0 {
				errs = append(errs, field.Required(valuesPath, "must be specified when operator is In or NotIn"))
			}
		case corev1.NodeSelectorOpExists, corev1.NodeSelectorOpDoesNotExist:
			if len(requirement.Values) > 0 {
				errs = append(errs, field.Forbidden(valuesPath,
					"may not be specified when operator is Exists or DoesNotExist"))
			}
		case corev1.NodeSelectorOpGt, corev1.NodeSelectorOpLt:
			if len(requirement.Values) != 1 {
				errs = append(errs, field.Required(valuesPath, "must be a single value when operator is Gt or Lt"))
			}
		default:
			errs = append(errs, field.Invalid(requirementPath.Child("operator"), requirement.Operator,
				"not a valid selector operator"))
		}
		errs = append(errs, metav1validation.ValidateLabelName(requirement.Key, requirementPath.Child("key"))...)
		if validateLabelValues {
			for j, value := range requirement.Values {
				for _, msg := range validation.IsValidLabelValue(value) {
					errs = append(errs, field.Invalid(valuesPath.Index(j), value, msg))
				}
			}
		}
	}
	for i, requirement := range term.MatchFields {
		requirementPath := path.Child("matchFields").Index(i)
		switch requirement.Operator {
		case corev1.NodeSelectorOpIn, corev1.NodeSelectorOpNotIn:
			if len(requirement.Values) != 1 {
				errs = append(errs, field.Required(requirementPath.Child("values"),
					"must be a single value when operator is In or NotIn"))
			}
		default:
			errs = append(errs, field.Invalid(requirementPath.Child("operator"), requirement.Operator,
				"not a valid selector operator"))
		}
		if requirement.Key != metav1.ObjectNameField {
			errs = append(errs, field.Invalid(requirementPath.Child("key"), requirement.Key,
				"not a valid field selector key"))
			continue
		}
		for j, value := range requirement.Values {
			for _, msg := range apimachineryvalidation.NameIsDNSSubdomain(value, false) {
				errs = append(errs, field.Invalid(requirementPath.Child("values").Index(j), value, msg))
			}
		}
	}
	return errs
}

func validateSysctls(path *field.Path, sysctls []corev1.Sysctl) field.ErrorList {
	var errs field.ErrorList
	names := sets.New[string]()
	for i, sysctl := range sysctls {
		namePath := path.Index(i).Child("name")
		switch {
		case sysctl.Name == "":
			errs = append(errs, field.Required(namePath, ""))
		case len(sysctl.Name) > sysctlNameMaxLength || !sysctlNameRegexp.MatchString(sysctl.Name):
			errs = append(errs, field.Invalid(namePath, sysctl.Name, fmt.Sprintf(
				"must have at most %d characters and match the regular expression %s", sysctlNameMaxLength,
				sysctlNameRegexp)))
		case names.Has(sysctl.Name):
			errs = append(errs, field.Duplicate(namePath, sysctl.Name))
		}
		names.Insert(sysctl.Name)
	}
	return errs
}

func validatePriorityClassName(path *field.Path, priorityClassName string) field.ErrorList {
	msgs := apimachineryvalidation.NameIsDNSSubdomain(priorityClassName, false)
	errs := make(field.ErrorList, 0, len(msgs))
	for _, msg := range msgs {
		errs = append(errs, field.Invalid(path, priorityClassName, msg))
	}
	return errs
}
