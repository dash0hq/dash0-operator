// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"github.com/dash0hq/dash0-operator/internal/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
)

func setAvailableConditionToUnknown(dash0CustomResource *operatorv1alpha1.Dash0) {
	meta.SetStatusCondition(
		&dash0CustomResource.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconcileStarted",
			Message: "Dash0 has started resource reconciliation.",
		})
}

func ensureResourceIsMarkedAsAvailable(dash0CustomResource *operatorv1alpha1.Dash0) {
	// If the available status is already true, the status condition is not updated, except for Reason, Message and
	// ObservedGeneration timestamp. In particular, LastTransitionTime is not updated. Thus, this operation is
	// effectively idempotent.
	meta.SetStatusCondition(
		&dash0CustomResource.Status.Conditions,
		metav1.Condition{
			Type:    string(util.ConditionTypeAvailable),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileFinished",
			Message: "Dash0 is is active in this namespace now.",
		})
	meta.RemoveStatusCondition(&dash0CustomResource.Status.Conditions, string(util.ConditionTypeDegraded))
}
