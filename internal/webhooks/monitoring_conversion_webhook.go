// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
)

func SetupDash0MonitoringConversionWebhookWithManager(mgr ctrl.Manager) error {
	return errors.Join(
		//
		ctrl.NewWebhookManagedBy(mgr, &dash0v1alpha1.Dash0Monitoring{}).Complete(),
		ctrl.NewWebhookManagedBy(mgr, &dash0v1beta1.Dash0Monitoring{}).Complete(),
	)
}
