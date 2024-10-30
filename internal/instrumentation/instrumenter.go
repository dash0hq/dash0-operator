// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

type Instrumenter struct {
	client.Client
	Clientset            *kubernetes.Clientset
	Recorder             record.EventRecorder
	Images               util.Images
	OTelCollectorBaseUrl string
	IsIPv6Cluster        bool
}

type ImmutableWorkloadError struct {
	workloadType     string
	workloadName     string
	modificationMode util.ModificationMode
}

const (
	workkloadTypeLabel     = "workload type"
	workloadNamespaceLabel = "workload namespace"
	workloadNameLabel      = "workload name"

	updateStatusFailedMessage = "Failed to update Dash0 monitoring status conditions, requeuing reconcile request."
)

var (
	timeoutForListingPods int64 = 2
)

func (e ImmutableWorkloadError) Error() string {
	var modificationParticle string
	switch e.modificationMode {
	case util.ModificationModeInstrumentation:
		modificationParticle = "instrument"
	case util.ModificationModeUninstrumentation:
		modificationParticle = "remove the instrumentation from"
	default:
		modificationParticle = "modify"
	}

	return fmt.Sprintf(
		"Dash0 cannot %s the existing %s %s, since this type of workload is immutable.",
		modificationParticle,
		e.workloadType,
		e.workloadName,
	)
}

// InstrumentAtStartup is run once, when the controller process starts. Its main purpose is to upgrade workloads that
// have already been instrumented, in namespaces where the Dash0 monitoring resource already exists. For those workloads,
// it is not guaranteed that a reconcile request will be triggered when the operator controller image is updated and
// restarted - reconcile requests are only triggered when the Dash0 monitoring resource is installed/changed/deleted.
// Since it runs the full instrumentation process, it might also as a byproduct instrument workloads that are not
// instrumented yet. It will only cover namespaces where a Dash0 monitoring resource exists, because it works by listing
// all Dash0 monitoring resources and then instrumenting workloads in the corresponding namespaces.
func (i *Instrumenter) InstrumentAtStartup(
	ctx context.Context,
	k8sClient client.Client,
	logger *logr.Logger,
) {
	logger.Info("Applying/updating instrumentation at controller startup.")
	allDash0MonitoringResouresInCluster := &dash0v1alpha1.Dash0MonitoringList{}
	if err := k8sClient.List(
		ctx,
		allDash0MonitoringResouresInCluster,
		&client.ListOptions{},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 monitoring resources at controller startup.")
		return
	}

	logger.Info(fmt.Sprintf("Found %d Dash0 monitoring resources.", len(allDash0MonitoringResouresInCluster.Items)))
	for _, dash0MonitoringResource := range allDash0MonitoringResouresInCluster.Items {
		logger.Info(fmt.Sprintf("Processing workloads in Dash0-enabled namespace %s", dash0MonitoringResource.Namespace))

		if dash0MonitoringResource.IsMarkedForDeletion() {
			continue
		}
		pseudoReconcileRequest := ctrl.Request{
			NamespacedName: client.ObjectKey{
				Namespace: dash0MonitoringResource.Namespace,
				Name:      dash0MonitoringResource.Name,
			},
		}
		checkResourceResult, err := util.VerifyThatUniqueResourceExists(
			ctx,
			k8sClient,
			pseudoReconcileRequest,
			&dash0v1alpha1.Dash0Monitoring{},
			updateStatusFailedMessage,
			logger,
		)
		if err != nil || checkResourceResult.StopReconcile || checkResourceResult.ResourceDoesNotExist {
			// if an error occurred, it has already been logged in VerifyThatUniqueResourceExists
			continue
		}

		err = i.CheckSettingsAndInstrumentExistingWorkloads(ctx, &dash0MonitoringResource, logger)
		if err != nil {
			logger.Error(
				err,
				"Failed to apply/update instrumentation instrumentation at startup in one namespace.",
				"namespace",
				dash0MonitoringResource.Namespace,
				"name",
				dash0MonitoringResource.Name,
			)
			continue
		}
	}
}

// CheckSettingsAndInstrumentExistingWorkloads is the main instrumentation function that is called in the controller's
// reconcile loop. It checks the settings of the Dash0 monitoring resource and instruments existing workloads
// accodingly.
func (i *Instrumenter) CheckSettingsAndInstrumentExistingWorkloads(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	instrumentWorkloads := dash0MonitoringResource.ReadInstrumentWorkloadsSetting()
	if instrumentWorkloads == dash0v1alpha1.None {
		logger.Info(
			"Instrumentation is not enabled, neither new nor existing workloads will be modified to send telemetry " +
				"to Dash0.",
		)
		return nil
	}
	if instrumentWorkloads == dash0v1alpha1.CreatedAndUpdated {
		logger.Info(
			"Instrumenting existing workloads is not enabled, only new or updated workloads will be modified (at " +
				"deploy time) to send telemetry to Dash0.",
		)
		return nil
	}

	logger.Info("Now instrumenting existing workloads in namespace so they send telemetry to Dash0.")
	if err := i.instrumentAllWorkloads(ctx, dash0MonitoringResource, logger); err != nil {
		logger.Error(err, "Instrumenting existing workloads failed.")
		return err
	}

	return nil
}

func (i *Instrumenter) instrumentAllWorkloads(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	namespace := dash0MonitoringResource.Namespace

	errCronJobs := i.findAndInstrumentCronJobs(ctx, namespace, logger)
	errDaemonSets := i.findAndInstrumentyDaemonSets(ctx, namespace, logger)
	errDeployments := i.findAndInstrumentDeployments(ctx, namespace, logger)
	errJobs := i.findAndAddLabelsToImmutableJobsOnInstrumentation(ctx, namespace, logger)
	errReplicaSets := i.findAndInstrumentReplicaSets(ctx, namespace, logger)
	errStatefulSets := i.findAndInstrumentStatefulSets(ctx, namespace, logger)
	combinedErrors := errors.Join(
		errCronJobs,
		errDaemonSets,
		errDeployments,
		errJobs,
		errReplicaSets,
		errStatefulSets,
	)
	if combinedErrors != nil {
		return combinedErrors
	}
	return nil
}

func (i *Instrumenter) findAndInstrumentCronJobs(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.BatchV1().CronJobs(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying cron jobs: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.instrumentCronJob(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) instrumentCronJob(
	ctx context.Context,
	cronJob batchv1.CronJob,
	reconcileLogger *logr.Logger,
) {
	i.instrumentWorkload(ctx, &cronJobWorkload{
		cronJob: &cronJob,
	}, reconcileLogger)
}

func (i *Instrumenter) findAndInstrumentyDaemonSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().DaemonSets(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying daemon sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.instrumentDaemonSet(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) instrumentDaemonSet(
	ctx context.Context,
	daemonSet appsv1.DaemonSet,
	reconcileLogger *logr.Logger,
) {
	i.instrumentWorkload(ctx, &daemonSetWorkload{
		daemonSet: &daemonSet,
	}, reconcileLogger)
}

func (i *Instrumenter) findAndInstrumentDeployments(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().Deployments(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying deployments: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.instrumentDeployment(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) instrumentDeployment(
	ctx context.Context,
	deployment appsv1.Deployment,
	reconcileLogger *logr.Logger,
) {
	i.instrumentWorkload(ctx, &deploymentWorkload{
		deployment: &deployment,
	}, reconcileLogger)
}

func (i *Instrumenter) findAndAddLabelsToImmutableJobsOnInstrumentation(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.BatchV1().Jobs(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying jobs: %w", err)
	}

	for _, job := range matchingWorkloadsInNamespace.Items {
		i.handleJobJobOnInstrumentation(ctx, job, logger)
	}
	return nil
}

func (i *Instrumenter) handleJobJobOnInstrumentation(
	ctx context.Context,
	job batchv1.Job,
	reconcileLogger *logr.Logger,
) {
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		"Job",
		workloadNamespaceLabel,
		job.GetNamespace(),
		workloadNameLabel,
		job.GetName(),
	)
	if job.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		logger.Info("not instrumenting this workload since it is about to be deleted (a deletion timestamp is set)")
		return
	}

	objectMeta := &job.ObjectMeta
	var requiredAction util.ModificationMode
	modifyLabels := true
	createImmutableWorkloadsError := true
	if util.HasOptedOutOfInstrumentation(objectMeta) && util.InstrumentationAttemptHasFailed(objectMeta) {
		// There has been an unsuccessful attempt to instrument this job before, but now the user has added the opt-out
		// label, so we can remove the labels left over from that earlier attempt.
		// "requiredAction = Instrumentation" in the context of immutable jobs means "remove Dash0 labels from the job",
		// no other modification will take place.
		requiredAction = util.ModificationModeUninstrumentation
		createImmutableWorkloadsError = false
	} else if util.HasOptedOutOfInstrumentation(objectMeta) && util.HasBeenInstrumentedSuccessfully(objectMeta) {
		// This job has been instrumented successfully, presumably by the webhook. Since then, the opt-out label has
		// been added. The correct action would be to uninstrument it, but since it is immutable, we cannot do that.
		// We will not actually modify this job at all, but create a log message and a corresponding event.
		modifyLabels = false
		requiredAction = util.ModificationModeUninstrumentation
	} else if util.HasOptedOutOfInstrumentation(objectMeta) {
		// has opt-out label and there has been no previous instrumentation attempt
		logger.Info("not instrumenting this workload due to dash0.com/enable=false")
		return
	} else if util.HasBeenInstrumentedSuccessfully(objectMeta) || util.InstrumentationAttemptHasFailed(objectMeta) {
		// We already have instrumented this job (via the webhook) or have failed to instrument it, in either case,
		// there is nothing to do here.
		return
	} else {
		// We have not attempted to instrument this job yet, that is, we are seeing this job for the first time now.
		//
		// "requiredAction = Instrumentation" in the context of immutable jobs means "add labels to the job", no other
		// modification will (or can) take place.
		requiredAction = util.ModificationModeInstrumentation
	}

	retryErr := util.Retry("handling immutable job", func() error {
		if !modifyLabels {
			return nil
		}

		if err := i.Client.Get(ctx, client.ObjectKey{
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
		}, &job); err != nil {
			return fmt.Errorf("error when fetching job %s/%s: %w", job.GetNamespace(), job.GetName(), err)
		}

		hasBeenModified := false
		switch requiredAction {
		case util.ModificationModeInstrumentation:
			hasBeenModified = newWorkloadModifier(i.Images, i.OTelCollectorBaseUrl, i.IsIPv6Cluster, &logger).AddLabelsToImmutableJob(&job)
		case util.ModificationModeUninstrumentation:
			hasBeenModified = newWorkloadModifier(i.Images, i.OTelCollectorBaseUrl, i.IsIPv6Cluster, &logger).RemoveLabelsFromImmutableJob(&job)
		}

		if hasBeenModified {
			return i.Client.Update(ctx, &job)
		} else {
			return nil
		}
	}, &logger)

	postProcess := i.postProcessInstrumentation
	if requiredAction == util.ModificationModeUninstrumentation {
		postProcess = i.postProcessUninstrumentation
	}
	if retryErr != nil {
		postProcess(&job, false, retryErr, &logger)
	} else if createImmutableWorkloadsError {
		// One way or another we are in a situation were we would have wanted to instrument/uninstrument the job, but
		// could not. Passing an ImmutableWorkloadError to postProcess will make sure we write a corresponding log
		// message and create a corresponding event.
		postProcess(&job, false, ImmutableWorkloadError{
			workloadType:     "job",
			workloadName:     fmt.Sprintf("%s/%s", job.GetNamespace(), job.GetName()),
			modificationMode: requiredAction,
		}, &logger)
	} else {
		postProcess(&job, false, nil, &logger)
	}
}

func (i *Instrumenter) findAndInstrumentReplicaSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying replica sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.instrumentReplicaSet(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) instrumentReplicaSet(
	ctx context.Context,
	replicaSet appsv1.ReplicaSet,
	reconcileLogger *logr.Logger,
) {
	hasBeenUpdated := i.instrumentWorkload(ctx, &replicaSetWorkload{
		replicaSet: &replicaSet,
	}, reconcileLogger)

	if hasBeenUpdated {
		i.restartPodsOfReplicaSet(ctx, replicaSet, reconcileLogger)
	}
}

func (i *Instrumenter) findAndInstrumentStatefulSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err := i.Clientset.AppsV1().StatefulSets(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying stateful sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.instrumentStatefulSet(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) instrumentStatefulSet(
	ctx context.Context,
	statefulSet appsv1.StatefulSet,
	reconcileLogger *logr.Logger,
) {
	i.instrumentWorkload(ctx, &statefulSetWorkload{
		statefulSet: &statefulSet,
	}, reconcileLogger)
}

func (i *Instrumenter) instrumentWorkload(
	ctx context.Context,
	workload instrumentableWorkload,
	reconcileLogger *logr.Logger,
) bool {
	objectMeta := workload.getObjectMeta()
	kind := workload.getKind()
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		kind,
		workloadNamespaceLabel,
		objectMeta.GetNamespace(),
		workloadNameLabel,
		objectMeta.GetName(),
	)
	if objectMeta.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		logger.Info("not instrumenting this workload since it is about to be deleted (a deletion timestamp is set)")
		return false
	}

	var requiredAction util.ModificationMode
	if util.WasInstrumentedButHasOptedOutNow(objectMeta) {
		requiredAction = util.ModificationModeUninstrumentation
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(objectMeta, i.Images) {
		// No change necessary, this workload has already been instrumented and an opt-out label (which would need to
		// trigger uninstrumentation) has not been added since it has been instrumented.
		logger.Info("not updating the existing instrumentation for this workload, it has already been successfully " +
			"instrumented by the same operator version")
		return false
	} else if util.HasOptedOutOfInstrumentationAndIsUninstrumented(workload.getObjectMeta()) {
		logger.Info("not instrumenting this workload due to dash0.com/enable=false")
		return false
	} else {
		requiredAction = util.ModificationModeInstrumentation
	}

	hasBeenModified := false
	retryErr := util.Retry(fmt.Sprintf("instrumenting %s", kind), func() error {
		if err := i.Client.Get(ctx, client.ObjectKey{
			Namespace: objectMeta.GetNamespace(),
			Name:      objectMeta.GetName(),
		}, workload.asClientObject()); err != nil {
			return fmt.Errorf(
				"error when fetching %s %s/%s: %w",
				kind,
				objectMeta.GetNamespace(),
				objectMeta.GetName(),
				err,
			)
		}

		switch requiredAction {
		case util.ModificationModeInstrumentation:
			hasBeenModified = workload.instrument(i.Images, i.OTelCollectorBaseUrl, i.IsIPv6Cluster, &logger)
		case util.ModificationModeUninstrumentation:
			hasBeenModified = workload.revert(i.Images, i.OTelCollectorBaseUrl, i.IsIPv6Cluster, &logger)
		}

		if hasBeenModified {
			return i.Client.Update(ctx, workload.asClientObject())
		} else {
			return nil
		}
	}, &logger)

	switch requiredAction {
	case util.ModificationModeInstrumentation:
		return i.postProcessInstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
	case util.ModificationModeUninstrumentation:
		return i.postProcessUninstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
	}
	return false
}

func (i *Instrumenter) postProcessInstrumentation(
	resource runtime.Object,
	hasBeenModified bool,
	retryErr error,
	logger *logr.Logger,
) bool {
	if retryErr != nil {
		e := &ImmutableWorkloadError{}
		if errors.As(retryErr, e) {
			logger.Info(e.Error())
		} else {
			logger.Error(retryErr, "Dash0 instrumentation by controller has not been successful.")
		}
		util.QueueFailedInstrumentationEvent(i.Recorder, resource, "controller", retryErr)
		return false
	} else if !hasBeenModified {
		// TODO This also happens for replica sets owned by a deployment and the log message as well as the message on
		// the event are unspecific, would be better if we could differentiate between the two cases.
		// (Also for revert maybe.)
		logger.Info("Dash0 instrumentation was already present on this workload, or the workload is part of a higher " +
			"order workload that will be instrumented, no modification by the controller is necessary.")
		util.QueueNoInstrumentationNecessaryEvent(i.Recorder, resource, "controller")
		return false
	} else {
		logger.Info("The controller has added Dash0 instrumentation to the workload.")
		util.QueueSuccessfulInstrumentationEvent(i.Recorder, resource, "controller")
		return true
	}
}

// UninstrumentWorkloadsIfAvailable is the main uninstrumentation function that is called in the controller's reconcile
// loop. It checks whether the Dash0 monitoring resource is marked as available; if it is, it uninstruments existing
// workloads.
func (i *Instrumenter) UninstrumentWorkloadsIfAvailable(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	if dash0MonitoringResource.IsAvailable() {
		logger.Info("Reverting Dash0's modifications to workloads that have been instrumented to make them send telemetry to Dash0.")
		if err := i.uninstrumentWorkloads(ctx, dash0MonitoringResource, logger); err != nil {
			logger.Error(err, "Uninstrumenting existing workloads failed.")
			return err
		}
	} else {
		logger.Info("Removing the Dash0 monitoring resource and running finalizers, but Dash0 is not marked as available." +
			" Dash0 Instrumentation will not be removed from workloads..")
	}
	return nil
}

func (i *Instrumenter) uninstrumentWorkloads(
	ctx context.Context,
	dash0MonitoringResource *dash0v1alpha1.Dash0Monitoring,
	logger *logr.Logger,
) error {
	namespace := dash0MonitoringResource.Namespace

	errCronJobs := i.findAndUninstrumentCronJobs(ctx, namespace, logger)
	errDaemonSets := i.findAndUninstrumentDaemonSets(ctx, namespace, logger)
	errDeployments := i.findAndUninstrumentDeployments(ctx, namespace, logger)
	errJobs := i.findAndHandleJobOnUninstrumentation(ctx, namespace, logger)
	errReplicaSets := i.findAndUninstrumentReplicaSets(ctx, namespace, logger)
	errStatefulSets := i.findAndUninstrumentStatefulSets(ctx, namespace, logger)
	combinedErrors := errors.Join(
		errCronJobs,
		errDaemonSets,
		errDeployments,
		errJobs,
		errReplicaSets,
		errStatefulSets,
	)
	if combinedErrors != nil {
		return combinedErrors
	}
	return nil
}

func (i *Instrumenter) findAndUninstrumentCronJobs(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.BatchV1().CronJobs(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented cron jobs: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.uninstrumentCronJob(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) uninstrumentCronJob(
	ctx context.Context,
	cronJob batchv1.CronJob,
	reconcileLogger *logr.Logger,
) {
	i.revertWorkloadInstrumentation(ctx, &cronJobWorkload{
		cronJob: &cronJob,
	}, reconcileLogger)
}

func (i *Instrumenter) findAndUninstrumentDaemonSets(ctx context.Context, namespace string, logger *logr.Logger) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().DaemonSets(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented daemon sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.uninstrumentDaemonSet(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) uninstrumentDaemonSet(
	ctx context.Context,
	daemonSet appsv1.DaemonSet,
	reconcileLogger *logr.Logger,
) {
	i.revertWorkloadInstrumentation(ctx, &daemonSetWorkload{
		daemonSet: &daemonSet,
	}, reconcileLogger)
}

func (i *Instrumenter) findAndUninstrumentDeployments(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().Deployments(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented deployments: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.uninstrumentDeployment(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) uninstrumentDeployment(
	ctx context.Context,
	deployment appsv1.Deployment,
	reconcileLogger *logr.Logger,
) {
	i.revertWorkloadInstrumentation(ctx, &deploymentWorkload{
		deployment: &deployment,
	}, reconcileLogger)
}

func (i *Instrumenter) findAndHandleJobOnUninstrumentation(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err := i.Clientset.BatchV1().Jobs(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented jobs: %w", err)
	}

	for _, job := range matchingWorkloadsInNamespace.Items {
		i.handleJobOnUninstrumentation(ctx, job, logger)
	}
	return nil
}

func (i *Instrumenter) handleJobOnUninstrumentation(ctx context.Context, job batchv1.Job, reconcileLogger *logr.Logger) {
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		"Job",
		workloadNamespaceLabel,
		job.GetNamespace(),
		workloadNameLabel,
		job.GetName(),
	)
	if job.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		logger.Info("not uninstrumenting this workload since it is about to be deleted (a deletion timestamp is set)")
		return
	}

	// Note: In contrast to the instrumentation logic, there is no need to check for dash0.com/enable=false here:
	// If it is set, the workload would not have been instrumented in the first place, hence the label selector filter
	// looking for dash0.com/instrumented=true would not have matched. Or if the workload is actually instrumented,
	// although it has dash0.com/enabled=false it must have been set after the instrumentation, in which case
	// uninstrumenting it is the correct thing to do.

	createImmutableWorkloadsError := false
	retryErr := util.Retry("removing labels from immutable job", func() error {
		if err := i.Client.Get(ctx, client.ObjectKey{
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
		}, &job); err != nil {
			return fmt.Errorf("error when fetching job %s/%s: %w", job.GetNamespace(), job.GetName(), err)
		}
		if util.HasBeenInstrumentedSuccessfully(&job.ObjectMeta) {
			// This job has been instrumented, presumably by the webhook. We cannot undo the instrumentation here, since
			// jobs are immutable.

			// Deliberately not calling newWorkloadModifier(i.Images, &logger).RemoveLabelsFromImmutableJob(&job) here
			// since we cannot remove the instrumentation, so we also have to leave the labels in place.
			createImmutableWorkloadsError = true
			return nil
		} else if util.InstrumentationAttemptHasFailed(&job.ObjectMeta) {
			// There was an attempt to instrument this job (probably by the controller), which has not been successful.
			// We only need remove the labels from that instrumentation attempt to clean up.
			newWorkloadModifier(i.Images, i.OTelCollectorBaseUrl, i.IsIPv6Cluster, &logger).RemoveLabelsFromImmutableJob(&job)

			// Apparently for jobs we do not need to set the "dash0.com/webhook-ignore-once" label, since changing their
			// labels does not trigger a new admission request.
			return i.Client.Update(ctx, &job)
		} else {
			// No dash0.com/instrumented label is present, do nothing.
			return nil
		}
	}, &logger)

	if retryErr != nil {
		// For the case that the job was instrumented, and we could not uninstrument it, we create a
		// ImmutableWorkloadError inside the retry loop. This error is then handled in the postProcessUninstrumentation.
		// The same is true for any other error types (for example errors in i.ClientUpdate).
		i.postProcessUninstrumentation(&job, false, retryErr, &logger)
	} else if createImmutableWorkloadsError {
		i.postProcessUninstrumentation(&job, false, ImmutableWorkloadError{
			workloadType:     "job",
			workloadName:     fmt.Sprintf("%s/%s", job.GetNamespace(), job.GetName()),
			modificationMode: util.ModificationModeUninstrumentation,
		}, &logger)
	} else {
		i.postProcessUninstrumentation(&job, false, nil, &logger)
	}
}

func (i *Instrumenter) findAndUninstrumentReplicaSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented replica sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.uninstrumentReplicaSet(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) uninstrumentReplicaSet(ctx context.Context, replicaSet appsv1.ReplicaSet, reconcileLogger *logr.Logger) {
	hasBeenUpdated := i.revertWorkloadInstrumentation(ctx, &replicaSetWorkload{
		replicaSet: &replicaSet,
	}, reconcileLogger)

	if hasBeenUpdated {
		i.restartPodsOfReplicaSet(ctx, replicaSet, reconcileLogger)
	}
}

func (i *Instrumenter) findAndUninstrumentStatefulSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		i.Clientset.AppsV1().StatefulSets(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented stateful sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		i.uninstrumentStatefulSet(ctx, resource, logger)
	}
	return nil
}

func (i *Instrumenter) uninstrumentStatefulSet(
	ctx context.Context,
	statefulSet appsv1.StatefulSet,
	reconcileLogger *logr.Logger,
) {
	i.revertWorkloadInstrumentation(ctx, &statefulSetWorkload{
		statefulSet: &statefulSet,
	}, reconcileLogger)
}

func (i *Instrumenter) revertWorkloadInstrumentation(
	ctx context.Context,
	workload instrumentableWorkload,
	reconcileLogger *logr.Logger,
) bool {
	objectMeta := workload.getObjectMeta()
	kind := workload.getKind()
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		kind,
		workloadNamespaceLabel,
		objectMeta.GetNamespace(),
		workloadNameLabel,
		objectMeta.GetName(),
	)
	if objectMeta.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		logger.Info("not uninstrumenting this workload since it is about to be deleted (a deletion timestamp is set)")
		return false
	}

	// Note: In contrast to the instrumentation logic, there is no need to check for dash0.com/enable=false here:
	// If it is set, the workload would not have been instrumented in the first place, hence the label selector filter
	// looking for dash0.com/instrumented=true would not have matched. Or if the workload is actually instrumented,
	// although it has dash0.com/enabled=false it must have been set after the instrumentation, in which case
	// uninstrumenting it is the correct thing to do.

	hasBeenModified := false
	retryErr := util.Retry(fmt.Sprintf("uninstrumenting %s", kind), func() error {
		if err := i.Client.Get(ctx, client.ObjectKey{
			Namespace: objectMeta.GetNamespace(),
			Name:      objectMeta.GetName(),
		}, workload.asClientObject()); err != nil {
			return fmt.Errorf(
				"error when fetching %s %s/%s: %w",
				kind,
				objectMeta.GetNamespace(),
				objectMeta.GetName(),
				err,
			)
		}
		hasBeenModified = workload.revert(i.Images, i.OTelCollectorBaseUrl, i.IsIPv6Cluster, &logger)
		if hasBeenModified {
			// Changing the workload spec sometimes triggers a new admission request, which would re-instrument the
			// workload via the webhook immediately. To prevent this, we add a label that the webhook can check to
			// prevent instrumentation.
			util.AddWebhookIgnoreOnceLabel(objectMeta)
			return i.Client.Update(ctx, workload.asClientObject())
		} else {
			return nil
		}
	}, &logger)

	return i.postProcessUninstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
}

func (i *Instrumenter) postProcessUninstrumentation(
	resource runtime.Object,
	hasBeenModified bool,
	retryErr error,
	logger *logr.Logger,
) bool {
	if retryErr != nil {
		e := &ImmutableWorkloadError{}
		if errors.As(retryErr, e) {
			logger.Info(e.Error())
		} else {
			logger.Error(retryErr, "Dash0's removal of instrumentation by controller has not been successful.")
		}
		util.QueueFailedUninstrumentationEvent(i.Recorder, resource, "controller", retryErr)
		return false
	} else if !hasBeenModified {
		logger.Info("Dash0 instrumentations was not present on this workload, no modification by the controller has " +
			"been necessary.")
		util.QueueNoUninstrumentationNecessaryEvent(i.Recorder, resource, "controller")
		return false
	} else {
		logger.Info("The controller has removed the Dash0 instrumentation from the workload.")
		util.QueueSuccessfulUninstrumentationEvent(i.Recorder, resource, "controller")
		return true
	}
}

func newWorkloadModifier(images util.Images, oTelCollectorBaseUrl string, isIPv6Cluster bool, logger *logr.Logger) *workloads.ResourceModifier {
	return workloads.NewResourceModifier(
		util.InstrumentationMetadata{
			Images:               images,
			InstrumentedBy:       "controller",
			OTelCollectorBaseUrl: oTelCollectorBaseUrl,
			IsIPv6Cluster:        isIPv6Cluster,
		},
		logger,
	)
}

func (i *Instrumenter) restartPodsOfReplicaSet(
	ctx context.Context,
	replicaSet appsv1.ReplicaSet,
	logger *logr.Logger,
) {
	// Note: ReplicaSet pods are not restarted automatically by Kubernetes when their spec is changed (for other
	// resource types like deployments or daemonsets this is managed by Kubernetes automatically). Therefore, we
	// find all pods owned by the replica set and explicitly delete them to trigger a restart.
	allPodsInNamespace, err :=
		i.Clientset.
			CoreV1().
			Pods(replicaSet.Namespace).
			List(ctx, metav1.ListOptions{
				TimeoutSeconds: &timeoutForListingPods,
			})
	if err != nil {
		logger.Error(
			err,
			fmt.Sprintf(
				"Failed to list all pods in the namespaces for the purpose of restarting the pods owned by the "+
					"replica set %s/%s (%s), pods will not be restarted automatically.",
				replicaSet.Namespace,
				replicaSet.Name,
				replicaSet.UID,
			))
		return
	}

	podsOfReplicaSet := slices.DeleteFunc(allPodsInNamespace.Items, func(pod corev1.Pod) bool {
		ownerReferences := pod.GetOwnerReferences()
		for _, ownerReference := range ownerReferences {
			if ownerReference.Kind == "ReplicaSet" &&
				ownerReference.Name == replicaSet.Name &&
				ownerReference.UID == replicaSet.UID {
				return false
			}
		}
		return true
	})

	for _, pod := range podsOfReplicaSet {
		err := i.Client.Delete(ctx, &pod)
		if err != nil {
			logger.Info(
				fmt.Sprintf(
					"Failed to restart pod owned by the replica "+
						"set %s/%s (%s), this pod will not be restarted automatically.",
					replicaSet.Namespace,
					replicaSet.Name,
					replicaSet.UID,
				))
		}
	}
}
