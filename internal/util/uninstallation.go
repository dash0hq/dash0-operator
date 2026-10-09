// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UninstallationDetector checks whether the Helm chart's pre-delete hook job exists in the operator namespace, that is,
// whether the operator is being uninstalled. The hook deletes all Dash0Monitoring resources and all cluster-scoped
// resources the operator manages; the operator must not recreate any of them from then on until it is gone.
//
// The apiReader must read directly from the API server (not from the informer cache), so that a just-created pre-delete
// hook job is guaranteed to be visible. Helm creates the job before the hook deletes anything. The chart keeps the job
// for a while after it has completed (ttlSecondsAfterFinished), so that it outlives the operator manager pod, which
// Helm only deletes after the hook has finished. A failed job means the uninstallation has been aborted and does not
// count. Jobs created before operatorCreatedAt are left over from a previous installation and are ignored. If the
// uninstallation is aborted after the hook has completed, resources are only recreated after the job has been removed,
// with the next reconcile.
//
// A nil *UninstallationDetector never reports an uninstallation.
type UninstallationDetector struct {
	apiReader         client.Reader
	operatorNamespace string
	operatorCreatedAt time.Time
}

func NewUninstallationDetector(
	apiReader client.Reader,
	operatorNamespace string,
	operatorCreatedAt time.Time,
) *UninstallationDetector {
	return &UninstallationDetector{
		apiReader:         apiReader,
		operatorNamespace: operatorNamespace,
		operatorCreatedAt: operatorCreatedAt,
	}
}

func (d *UninstallationDetector) IsOperatorBeingUninstalled(ctx context.Context) (bool, error) {
	if d == nil {
		return false, nil
	}
	preDeleteHookJobs := &batchv1.JobList{}
	if err := d.apiReader.List(
		ctx,
		preDeleteHookJobs,
		client.InNamespace(d.operatorNamespace),
		client.MatchingLabels{AppKubernetesIoComponentLabel: UninstallationProcessComponent},
	); err != nil {
		return false, err
	}
	for i := range preDeleteHookJobs.Items {
		if indicatesUninstallation(&preDeleteHookJobs.Items[i], d.operatorCreatedAt) {
			return true, nil
		}
	}
	return false, nil
}

func indicatesUninstallation(preDeleteHookJob *batchv1.Job, operatorCreatedAt time.Time) bool {
	if preDeleteHookJob.CreationTimestamp.Time.Before(operatorCreatedAt) {
		return false
	}
	for _, condition := range preDeleteHookJob.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false
		}
	}
	return true
}
