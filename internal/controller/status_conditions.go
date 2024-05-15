// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setAvailableConditionToUnknown(dash0CustomResource *operatorv1alpha1.Dash0) {
	meta.SetStatusCondition(
		&dash0CustomResource.Status.Conditions,
		metav1.Condition{
			Type:    string(operatorv1alpha1.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Dash0 has started resource reconciliation.",
		})
}

func makeAvailable(dash0CustomResource *operatorv1alpha1.Dash0) {
	meta.SetStatusCondition(
		&dash0CustomResource.Status.Conditions,
		metav1.Condition{
			Type:    string(operatorv1alpha1.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcilingFinished",
			Message: "Dash0 is is active in this namespace now.",
		})
	meta.RemoveStatusCondition(&dash0CustomResource.Status.Conditions, string(operatorv1alpha1.ConditionTypeDegraded))
}
