// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

const (
	FinalizerId = "operator.dash0.com/finalizer"

	workkloadTypeLabel     = "workload type"
	workloadNamespaceLabel = "workload namespace"
	workloadNameLabel      = "workload name"
)

type Dash0Reconciler struct {
	client.Client
	ClientSet *kubernetes.Clientset
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Images    util.Images
}

type ModificationMode string

const (
	Instrumentation   ModificationMode = "Instrumentation"
	Uninstrumentation ModificationMode = "Uninstrumentation"
)

type ImmutableWorkloadError struct {
	workloadType     string
	workloadName     string
	modificationMode ModificationMode
}

func (e ImmutableWorkloadError) Error() string {
	var modificationParticle string
	switch e.modificationMode {
	case Instrumentation:
		modificationParticle = "instrument"
	case Uninstrumentation:
		modificationParticle = "remove the instrumentation from"
	default:
		modificationParticle = "modify"
	}

	return fmt.Sprintf(
		"Dash0 cannot %s the existing %s %s, since the this type of workload is immutable.",
		modificationParticle,
		e.workloadType,
		e.workloadName,
	)
}

func (r *Dash0Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Dash0{}).
		Complete(r)
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s/finalizers,verbs=update
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *Dash0Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Processig reconcile request")

	namespaceStillExists, err := r.checkIfNamespaceExists(ctx, req.Namespace, &logger)
	if err != nil {
		// The error has already been logged in checkIfNamespaceExists.
		return ctrl.Result{}, err
	} else if !namespaceStillExists {
		logger.Info("The namespace seems to have been deleted after this reconcile request has been scheduled. " +
			"Ignoring the reconcile request.")
		return ctrl.Result{}, nil
	}

	// Check whether the Dash0 custom resource exists.
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	err = r.Get(ctx, req.NamespacedName, dash0CustomResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(
				"The Dash0 custom resource has not been found, either it hasn't been installed or it has been " +
					"deleted. Ignoring the reconcile request.")
			// stop the reconciliation
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get the Dash0 custom resource, requeuing reconcile request.")
		return ctrl.Result{}, err
	}

	isFirstReconcile, err := r.initStatusConditions(ctx, dash0CustomResource, &logger)
	if err != nil {
		// The error has already been logged in initStatusConditions
		return ctrl.Result{}, err
	}

	isMarkedForDeletion, err := r.checkImminentDeletionAndHandleFinalizers(ctx, dash0CustomResource, &logger)
	if err != nil {
		// The error has already been logged in checkImminentDeletionAndHandleFinalizers
		return ctrl.Result{}, err
	} else if isMarkedForDeletion {
		// The Dash0 custom resource is slated for deletion, all cleanup actions (like reverting instrumented resources)
		// have been processed, no further reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	if isFirstReconcile {
		if err = r.handleFirstReconcile(ctx, dash0CustomResource, &logger); err != nil {
			// The error has already been logged in handleFirstReconcile
			return ctrl.Result{}, err
		}
	}

	dash0CustomResource.EnsureResourceIsMarkedAsAvailable()
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		logger.Error(err, "Failed to update Dash0 status conditions, requeuing reconcile request.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Dash0Reconciler) checkIfNamespaceExists(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) (bool, error) {
	_, err := r.ClientSet.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		} else {
			logger.Error(err, "Failed to fetch the current namespace, requeuing reconcile request.")
			return true, err
		}
	}
	return true, nil
}

func (r *Dash0Reconciler) initStatusConditions(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) (bool, error) {
	firstReconcile := false
	needsRefresh := false
	if dash0CustomResource.Status.Conditions == nil || len(dash0CustomResource.Status.Conditions) == 0 {
		dash0CustomResource.SetAvailableConditionToUnknown()
		firstReconcile = true
		needsRefresh = true
	} else if availableCondition :=
		meta.FindStatusCondition(
			dash0CustomResource.Status.Conditions,
			string(util.ConditionTypeAvailable),
		); availableCondition == nil {
		dash0CustomResource.SetAvailableConditionToUnknown()
		needsRefresh = true
	}
	if needsRefresh {
		err := r.refreshStatus(ctx, dash0CustomResource, logger)
		if err != nil {
			// The error has already been logged in refreshStatus
			return firstReconcile, err
		}
	}
	return firstReconcile, nil
}

func (r *Dash0Reconciler) handleFirstReconcile(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	logger.Info("Initial reconcile in progress.")
	instrumentationEnabled := true
	instrumentingExistingWorkloadsEnabled := true
	if !instrumentationEnabled {
		logger.Info(
			"Instrumentation is not enabled, neither new nor existing workloads will be modified to send telemetry " +
				"to Dash0.",
		)
		return nil
	}

	if !instrumentingExistingWorkloadsEnabled {
		logger.Info(
			"Instrumenting existing workloads is not enabled, only new workloads will be modified (at deploy time) " +
				"to send telemetry to Dash0.",
		)
		return nil
	}

	logger.Info("Modifying existing workloads to make them send telemetry to Dash0.")
	if err := r.instrumentWorkloadsResources(ctx, dash0CustomResource, logger); err != nil {
		logger.Error(err, "Instrumenting existing workloads failed, requeuing reconcile request.")
		return err
	}

	return nil
}

func (r *Dash0Reconciler) refreshStatus(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		logger.Error(err, "Cannot update the status of the Dash0 custom resource, requeuing reconcile request.")
		return err
	}
	return nil
}

func (r *Dash0Reconciler) instrumentWorkloadsResources(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	namespace := dash0CustomResource.Namespace

	errCronJobs := r.findAndInstrumentCronJobs(ctx, namespace, logger)
	errDaemonSets := r.findAndInstrumentyDaemonSets(ctx, namespace, logger)
	errDeployments := r.findAndInstrumentDeployments(ctx, namespace, logger)
	errJobs := r.findAndAddLabelsToImmutableJobsOnInstrumentation(ctx, namespace, logger)
	errReplicaSets := r.findAndInstrumentReplicaSets(ctx, namespace, logger)
	errStatefulSets := r.findAndInstrumentStatefulSets(ctx, namespace, logger)
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

func (r *Dash0Reconciler) findAndInstrumentCronJobs(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.BatchV1().CronJobs(namespace).List(ctx, util.WorkloadsWithoutDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying cron jobs: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.instrumentCronJob(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) instrumentCronJob(
	ctx context.Context,
	cronJob batchv1.CronJob,
	reconcileLogger *logr.Logger,
) {
	r.instrumentWorkload(ctx, &cronJobWorkload{
		cronJob: &cronJob,
		images:  r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndInstrumentyDaemonSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().DaemonSets(namespace).List(ctx, util.WorkloadsWithoutDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying daemon sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.instrumentDaemonSet(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) instrumentDaemonSet(
	ctx context.Context,
	daemonSet appsv1.DaemonSet,
	reconcileLogger *logr.Logger,
) {
	r.instrumentWorkload(ctx, &daemonSetWorkload{
		daemonSet: &daemonSet,
		images:    r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndInstrumentDeployments(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().Deployments(namespace).List(ctx, util.WorkloadsWithoutDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying deployments: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.instrumentDeployment(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) instrumentDeployment(
	ctx context.Context,
	deployment appsv1.Deployment,
	reconcileLogger *logr.Logger,
) {
	r.instrumentWorkload(ctx, &deploymentWorkload{
		deployment: &deployment,
		images:     r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndAddLabelsToImmutableJobsOnInstrumentation(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.BatchV1().Jobs(namespace).List(ctx, util.WorkloadsWithoutDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying jobs: %w", err)
	}

	for _, job := range matchingWorkloadsInNamespace.Items {
		r.addLabelsToImmutableJobsOnInstrumentation(ctx, job, logger)
	}
	return nil
}

func (r *Dash0Reconciler) addLabelsToImmutableJobsOnInstrumentation(
	ctx context.Context,
	job batchv1.Job,
	reconcileLogger *logr.Logger,
) {
	if job.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		"Job",
		workloadNamespaceLabel,
		job.GetNamespace(),
		workloadNameLabel,
		job.GetName(),
	)
	retryErr := util.Retry("labelling immutable job", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
		}, &job); err != nil {
			return fmt.Errorf("error when fetching job %s/%s: %w", job.GetNamespace(), job.GetName(), err)
		}
		newWorkloadModifier(r.Images, &logger).AddLabelsToImmutableJob(&job)
		return r.Client.Update(ctx, &job)
	}, &logger)

	if retryErr != nil {
		r.postProcessInstrumentation(&job, false, retryErr, &logger)
	} else {
		r.postProcessInstrumentation(&job, false, ImmutableWorkloadError{
			workloadType:     "job",
			workloadName:     fmt.Sprintf("%s/%s", job.GetNamespace(), job.GetName()),
			modificationMode: Instrumentation,
		}, &logger)
	}
}

func (r *Dash0Reconciler) findAndInstrumentReplicaSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().ReplicaSets(namespace).List(ctx, util.WorkloadsWithoutDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying deployments: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.instrumentReplicaSet(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) instrumentReplicaSet(
	ctx context.Context,
	replicaSet appsv1.ReplicaSet,
	reconcileLogger *logr.Logger,
) {
	// Note: ReplicaSet pods are not restarted automatically by Kubernetes when their spec is changed (for other
	// resource types like deployments or daemonsets this is managed by Kubernetes automatically). For now, we rely on
	// the user to manually restart the pods of their replica sets after they have been instrumented. We could consider
	// finding all pods for that are owned by the replica set and restart them automatically.

	r.instrumentWorkload(ctx, &replicaSetWorkload{
		replicaSet: &replicaSet,
		images:     r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndInstrumentStatefulSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err := r.ClientSet.AppsV1().StatefulSets(namespace).List(ctx, util.WorkloadsWithoutDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying stateful sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.instrumentStatefulSet(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) instrumentStatefulSet(
	ctx context.Context,
	statefulSet appsv1.StatefulSet,
	reconcileLogger *logr.Logger,
) {
	r.instrumentWorkload(ctx, &statefulSetWorkload{
		statefulSet: &statefulSet,
		images:      r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) instrumentWorkload(
	ctx context.Context,
	workload instrumentableWorkload,
	reconcileLogger *logr.Logger,
) {
	objectMeta := workload.getObjectMeta()
	kind := workload.getKind()
	if objectMeta.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		kind,
		workloadNamespaceLabel,
		objectMeta.GetNamespace(),
		workloadNameLabel,
		objectMeta.GetName(),
	)

	hasBeenModified := false
	retryErr := util.Retry(fmt.Sprintf("instrumenting %s", kind), func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
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
		hasBeenModified = workload.instrument(&logger)
		if hasBeenModified {
			return r.Client.Update(ctx, workload.asClientObject())
		} else {
			return nil
		}
	}, &logger)

	r.postProcessInstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
}

func (r *Dash0Reconciler) postProcessInstrumentation(
	resource runtime.Object,
	hasBeenModified bool,
	retryErr error,
	logger *logr.Logger,
) {
	if retryErr != nil {
		e := &ImmutableWorkloadError{}
		if errors.As(retryErr, e) {
			logger.Info(e.Error())
		} else {
			logger.Error(retryErr, "Dash0 instrumentation by controller has not been successful.")
		}
		util.QueueFailedInstrumentationEvent(r.Recorder, resource, "controller", retryErr)
	} else if !hasBeenModified {
		// TODO This also happens for replica sets owned by a deployment and the log message as well as the message on
		// the event are unspecific, would be better if we could differentiate between the two cases.
		// (Also for revert maybe.)
		logger.Info("Dash0 instrumentation was already present on this workload, or the workload is part of a higher " +
			"order workload that will be instrumented, no modification by controller is necessary.")
		util.QueueNoInstrumentationNecessaryEvent(r.Recorder, resource, "controller")
	} else {
		logger.Info("The controller has added Dash0 instrumentation to the workload.")
		util.QueueSuccessfulInstrumentationEvent(r.Recorder, resource, "controller")
	}
}

func (r *Dash0Reconciler) checkImminentDeletionAndHandleFinalizers(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) (bool, error) {
	deletionTimestamp := dash0CustomResource.GetDeletionTimestamp()
	isMarkedForDeletion := deletionTimestamp != nil && !deletionTimestamp.IsZero()
	if !isMarkedForDeletion {
		err := r.addFinalizerIfNecessary(ctx, dash0CustomResource)
		if err != nil {
			logger.Error(err, "Failed to add finalizer to Dash0 custom resource, requeuing reconcile request.")
			return isMarkedForDeletion, err
		}
	} else {
		if controllerutil.ContainsFinalizer(dash0CustomResource, FinalizerId) {
			err := r.runCleanupActions(ctx, dash0CustomResource, logger)
			if err != nil {
				// error has already been logged in runCleanupActions
				return isMarkedForDeletion, err
			}
		}
	}
	return isMarkedForDeletion, nil
}

func (r *Dash0Reconciler) runCleanupActions(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	err := r.uninstrumentWorkloadsIfAvailable(ctx, dash0CustomResource, logger)
	if err != nil {
		logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
		return err
	}

	// The Dash0 custom resource will be deleted after this reconcile finished, so updating the status is
	// probably unnecessary. But for due process we do it anyway. In particular, if deleting it should fail
	// for any reason or take a while, the resource is no longer marked as available.
	dash0CustomResource.EnsureResourceIsMarkedAsUnavailable()
	if err = r.Status().Update(ctx, dash0CustomResource); err != nil {
		logger.Error(err, "Failed to update Dash0 status conditions, requeuing reconcile request.")
		return err
	}

	controllerutil.RemoveFinalizer(dash0CustomResource, FinalizerId)
	if err = r.Update(ctx, dash0CustomResource); err != nil {
		logger.Error(err, "Failed to remove the finalizer from the Dash0 custom resource, requeuing reconcile request.")
		return err
	}
	return nil
}

func (r *Dash0Reconciler) addFinalizerIfNecessary(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
) error {
	finalizerHasBeenAdded := controllerutil.AddFinalizer(dash0CustomResource, FinalizerId)
	if finalizerHasBeenAdded {
		return r.Update(ctx, dash0CustomResource)
	}
	// The resource already had the finalizer, no update necessary.
	return nil
}

func (r *Dash0Reconciler) uninstrumentWorkloadsIfAvailable(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	if dash0CustomResource.IsAvailable() {
		logger.Info("Reverting Dash0's modifications to workloads that have been instrumented to make them send telemetry to Dash0.")
		if err := r.uninstrumentWorkloads(ctx, dash0CustomResource, logger); err != nil {
			logger.Error(err, "Uninstrumenting existing workloads failed.")
			return err
		}
	} else {
		logger.Info("Removing the Dash0 custom resource and running finalizers, but Dash0 is not marked as available." +
			" Dash0 Instrumentation will not be removed from workloads..")
	}
	return nil
}

func (r *Dash0Reconciler) uninstrumentWorkloads(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	namespace := dash0CustomResource.Namespace

	errCronJobs := r.findAndUninstrumentCronJobs(ctx, namespace, logger)
	errDaemonSets := r.findAndUninstrumentDaemonSets(ctx, namespace, logger)
	errDeployments := r.findAndUninstrumentDeployments(ctx, namespace, logger)
	errJobs := r.findAndHandleJobOnUninstrumentation(ctx, namespace, logger)
	errReplicaSets := r.findAndUninstrumentReplicaSets(ctx, namespace, logger)
	errStatefulSets := r.findAndUninstrumentStatefulSets(ctx, namespace, logger)
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

func (r *Dash0Reconciler) findAndUninstrumentCronJobs(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.BatchV1().CronJobs(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented cron jobs: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.uninstrumentCronJob(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) uninstrumentCronJob(
	ctx context.Context,
	cronJob batchv1.CronJob,
	reconcileLogger *logr.Logger,
) {
	r.revertWorkloadInstrumentation(ctx, &cronJobWorkload{
		cronJob: &cronJob,
		images:  r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndUninstrumentDaemonSets(ctx context.Context, namespace string, logger *logr.Logger) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().DaemonSets(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented daemon sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.uninstrumentDaemonSet(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) uninstrumentDaemonSet(
	ctx context.Context,
	daemonSet appsv1.DaemonSet,
	reconcileLogger *logr.Logger,
) {
	r.revertWorkloadInstrumentation(ctx, &daemonSetWorkload{
		daemonSet: &daemonSet,
		images:    r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndUninstrumentDeployments(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().Deployments(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented deployments: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.uninstrumentDeployment(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) uninstrumentDeployment(
	ctx context.Context,
	deployment appsv1.Deployment,
	reconcileLogger *logr.Logger,
) {
	r.revertWorkloadInstrumentation(ctx, &deploymentWorkload{
		deployment: &deployment,
		images:     r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndHandleJobOnUninstrumentation(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err := r.ClientSet.BatchV1().Jobs(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented jobs: %w", err)
	}

	for _, job := range matchingWorkloadsInNamespace.Items {
		r.handleJobOnUninstrumentation(ctx, job, logger)
	}
	return nil
}

func (r *Dash0Reconciler) handleJobOnUninstrumentation(ctx context.Context, job batchv1.Job, reconcileLogger *logr.Logger) {
	if job.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		"Job",
		workloadNamespaceLabel,
		job.GetNamespace(),
		workloadNameLabel,
		job.GetName(),
	)

	createImmutableWorkloadsError := false
	retryErr := util.Retry("removing labels from immutable job", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
		}, &job); err != nil {
			return fmt.Errorf("error when fetching job %s/%s: %w", job.GetNamespace(), job.GetName(), err)
		}
		if util.HasBeenInstrumentedSuccessfully(&job.ObjectMeta) {
			// This job has been instrumented, presumably by the webhook. We cannot undo the instrumentation here, since
			// jobs are immutable.

			// Deliberately not calling newWorkloadModifier(r.Images, &logger).RemoveLabelsFromImmutableJob(&job) here
			// since we cannot remove the instrumentation, so we also have to leave the labels in place.
			createImmutableWorkloadsError = true
			return nil
		} else if util.InstrumenationAttemptHasFailed(&job.ObjectMeta) {
			// There was an attempt to instrument this job (probably by the controller), which has not been successful.
			// We only need remove the labels from that instrumentation attempt to clean up.
			newWorkloadModifier(r.Images, &logger).RemoveLabelsFromImmutableJob(&job)

			// Apparently for jobs we do not need to set the "dash0.com/webhook-ignore-once" label, since changing their
			// labels does not trigger a new admission request.
			return r.Client.Update(ctx, &job)
		} else {
			// No dash0.com/instrumented label is present, do nothing.
			return nil
		}
	}, &logger)

	if retryErr != nil {
		// For the case that the job was instrumented and we could not uninstrument it, we create a
		// ImmutableWorkloadError inside the retry loop. This error is then handled in the postProcessUninstrumentation.
		// The same is true for any other error types (for example errors in r.ClientUpdate).
		r.postProcessUninstrumentation(&job, false, retryErr, &logger)
	} else if createImmutableWorkloadsError {
		//
		r.postProcessUninstrumentation(&job, false, ImmutableWorkloadError{
			workloadType:     "job",
			workloadName:     fmt.Sprintf("%s/%s", job.GetNamespace(), job.GetName()),
			modificationMode: Uninstrumentation,
		}, &logger)
	} else {
		r.postProcessUninstrumentation(&job, false, nil, &logger)
	}
}

func (r *Dash0Reconciler) findAndUninstrumentReplicaSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().ReplicaSets(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented replica sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.uninstrumentReplicaSet(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) uninstrumentReplicaSet(ctx context.Context, replicaSet appsv1.ReplicaSet, reconcileLogger *logr.Logger) {
	// Note: ReplicaSet pods are not restarted automatically by Kubernetes when their spec is change (for other resource
	// types like deployments or daemonsets this is managed by Kubernetes automatically). For now, we rely on the user
	// to manually restart the pods of their replica sets after they have been instrumented. We could consider finding
	// all pods for that are owned by the replica set and restart them automatically.
	r.revertWorkloadInstrumentation(ctx, &replicaSetWorkload{
		replicaSet: &replicaSet,
		images:     r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndUninstrumentStatefulSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().StatefulSets(namespace).List(ctx, util.WorkloadsWithDash0InstrumentedLabelFilter)
	if err != nil {
		return fmt.Errorf("error when querying instrumented stateful sets: %w", err)
	}
	for _, resource := range matchingWorkloadsInNamespace.Items {
		r.uninstrumentStatefulSet(ctx, resource, logger)
	}
	return nil
}

func (r *Dash0Reconciler) uninstrumentStatefulSet(
	ctx context.Context,
	statefulSet appsv1.StatefulSet,
	reconcileLogger *logr.Logger,
) {
	r.revertWorkloadInstrumentation(ctx, &statefulSetWorkload{
		statefulSet: &statefulSet,
		images:      r.Images,
	}, reconcileLogger)
}

func (r *Dash0Reconciler) revertWorkloadInstrumentation(
	ctx context.Context,
	workload instrumentableWorkload,
	reconcileLogger *logr.Logger,
) {
	objectMeta := workload.getObjectMeta()
	kind := workload.getKind()
	if objectMeta.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := reconcileLogger.WithValues(
		workkloadTypeLabel,
		kind,
		workloadNamespaceLabel,
		objectMeta.GetNamespace(),
		workloadNameLabel,
		objectMeta.GetName(),
	)

	hasBeenModified := false
	retryErr := util.Retry(fmt.Sprintf("uninstrumenting %s", kind), func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
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
		hasBeenModified = workload.revert(&logger)
		if hasBeenModified {
			// Changing the workload spec sometimes triggers a new admission request, which would re-instrument the
			// workload via the webhook immediately. To prevent this, we add a label that the webhook can check to
			// prevent instrumentation.
			util.AddWebhookIgnoreOnceLabel(objectMeta)
			return r.Client.Update(ctx, workload.asClientObject())
		} else {
			return nil
		}
	}, &logger)

	r.postProcessUninstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
}

func (r *Dash0Reconciler) postProcessUninstrumentation(
	resource runtime.Object,
	hasBeenModified bool,
	retryErr error,
	logger *logr.Logger,
) {
	if retryErr != nil {
		e := &ImmutableWorkloadError{}
		if errors.As(retryErr, e) {
			logger.Info(e.Error())
		} else {
			logger.Error(retryErr, "Dash0's removal of instrumentation by controller has not been successful.")
		}
		util.QueueFailedUninstrumentationEvent(r.Recorder, resource, "controller", retryErr)
	} else if !hasBeenModified {
		logger.Info("Dash0 instrumentations was not present on this workload, no modification by controller has been " +
			"necessary.")
		util.QueueNoUninstrumentationNecessaryEvent(r.Recorder, resource, "controller")
	} else {
		logger.Info("The controller has removed Dash0 instrumentation from the workload.")
		util.QueueSuccessfulUninstrumentationEvent(r.Recorder, resource, "controller")
	}
}

func newWorkloadModifier(images util.Images, logger *logr.Logger) *workloads.ResourceModifier {
	return workloads.NewResourceModifier(
		util.InstrumentationMetadata{
			Images:         images,
			InstrumentedBy: "controller",
		},
		logger,
	)
}
