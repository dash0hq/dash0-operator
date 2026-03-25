// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
)

func AvailableConditionBecameTrue(oldConditions []metav1.Condition, newConditions []metav1.Condition) bool {
	oldAvailable := meta.FindStatusCondition(oldConditions, string(dash0common.ConditionTypeAvailable))
	newAvailable := meta.FindStatusCondition(newConditions, string(dash0common.ConditionTypeAvailable))
	wasTrue := oldAvailable != nil && oldAvailable.Status == metav1.ConditionTrue
	isTrue := newAvailable != nil && newAvailable.Status == metav1.ConditionTrue
	return !wasTrue && isTrue
}
