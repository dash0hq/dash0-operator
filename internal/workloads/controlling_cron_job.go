// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"fmt"
	"maps"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
)

const controllingCronJobReadTimeout = 2 * time.Second

// ReadControllingCronJob reads the metadata of the cron job that controls the job. It returns nil if the job is not
// controlled by a cron job, or if that cron job does not exist anymore. An error is returned if the cron job could not
// be read for any other reason, including the read taking longer than controllingCronJobReadTimeout, since the
// instrumentation webhook needs to answer well within its timeout. The reader is expected to read directly from the API
// server, so that reading the cron job does not start an informer for all cron jobs in the cluster.
func ReadControllingCronJob(
	ctx context.Context,
	reader client.Reader,
	namespace string,
	job *batchv1.Job,
) (*metav1.PartialObjectMetadata, error) {
	ownerReference := metav1.GetControllerOfNoCopy(job)
	if ownerReference == nil ||
		ownerReference.APIVersion != util.K8sTypeMetaCronJob.APIVersion ||
		ownerReference.Kind != util.K8sTypeMetaCronJob.Kind {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, controllingCronJobReadTimeout)
	defer cancel()
	cronJob := &metav1.PartialObjectMetadata{TypeMeta: util.K8sTypeMetaCronJob}
	err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ownerReference.Name}, cronJob)
	if apierrors.IsNotFound(err) || (err == nil && cronJob.UID != ownerReference.UID) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the cron job %s/%s controlling the job: %w", namespace, ownerReference.Name, err)
	}
	return cronJob, nil
}

// JobHasOptedOutOfInstrumentation evaluates the instrumentation label selector against the labels of the job merged
// with the labels of the cron job controlling it, if any. The cron job controller only copies the labels of the job
// template to a job, merging in the labels of the cron job makes an opt-out in its top-level metadata apply to its jobs
// as well. If a label is set on both with different values, the value from the job wins.
func JobHasOptedOutOfInstrumentation(
	job *batchv1.Job,
	controllingCronJob *metav1.PartialObjectMetadata,
	instrumentationLabelSelector string,
) bool {
	mergedLabels := make(map[string]string)
	if controllingCronJob != nil {
		maps.Copy(mergedLabels, controllingCronJob.Labels)
	}
	maps.Copy(mergedLabels, job.Labels)
	return util.HasOptedOutOfInstrumentation(&metav1.ObjectMeta{Labels: mergedLabels}, instrumentationLabelSelector)
}
