// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
	"github.com/dash0hq/dash0-operator/internal/dash0/util"
	"github.com/dash0hq/dash0-operator/internal/dash0/workloads"
)

type Dash0Reconciler struct {
	client.Client
	ClientSet            *kubernetes.Clientset
	Scheme               *runtime.Scheme
	Recorder             record.EventRecorder
	Images               util.Images
	OtelCollectorBaseUrl string
}

type ModificationMode string

const (
	workkloadTypeLabel     = "workload type"
	workloadNamespaceLabel = "workload namespace"
	workloadNameLabel      = "workload name"

	updateStatusFailedMessage = "Failed to update Dash0 status conditions, requeuing reconcile request."

	Instrumentation   ModificationMode = "Instrumentation"
	Uninstrumentation ModificationMode = "Uninstrumentation"
)

var (
	timeoutForListingPods int64 = 2
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
		"Dash0 cannot %s the existing %s %s, since this type of workload is immutable.",
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

// InstrumentAtStartup is run once, when the controller process starts. Its main purpose is to upgrade workloads that
// have already been instrumented, in namespaces where the Dash0 custom resource already exists. For those workloads,
// it is not guaranteed that a reconcile request will be triggered when the operator controller image is updated and
// restarted - reconcile requests are only triggered when the Dash0 custom resource is installed/changed/deleted.
// Since it runs the full instrumentation process, it might also as a byproduct instrument workloads that are not
// instrumented yet. It will only cover namespaces where a Dash0 custom resource exists, because it works by listing
// all Dash0 custom resources and then instrumenting workloads in the corresponding namespaces.
func (r *Dash0Reconciler) InstrumentAtStartup() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	logger.Info("Applying/updating instrumentation at controller startup.")
	dash0CustomResourcesInNamespace := &operatorv1alpha1.Dash0List{}
	if err := r.Client.List(
		ctx,
		dash0CustomResourcesInNamespace,
		&client.ListOptions{},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 custom resources at controller startup.")
		return
	}

	logger.Info(fmt.Sprintf("Found %d Dash0 custom resources.", len(dash0CustomResourcesInNamespace.Items)))
	for _, dash0CustomResource := range dash0CustomResourcesInNamespace.Items {
		logger.Info(fmt.Sprintf("Processing workloads in Dash0-enabled namespace %s", dash0CustomResource.Namespace))

		if dash0CustomResource.IsMarkedForDeletion() {
			continue
		}
		pseudoReconcileRequest := ctrl.Request{
			NamespacedName: client.ObjectKey{
				Namespace: dash0CustomResource.Namespace,
				Name:      dash0CustomResource.Name,
			},
		}
		_, stop, err := r.verifyUniqueDash0CustomResourceExists(ctx, pseudoReconcileRequest, logger)
		if err != nil || stop {
			// if an error occurred, it has already been logged in verifyUniqueDash0CustomResourceExists
			continue
		}

		err = r.checkSettingsAndInstrumentAllWorkloads(ctx, &dash0CustomResource, &logger)
		if err != nil {
			logger.Error(
				err,
				"Failed to apply/update instrumentation instrumentation at startup in one namespace.",
				"namespace",
				dash0CustomResource.Namespace,
				"name",
				dash0CustomResource.Name,
			)
			continue
		}
	}
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s,verbs=get;list;watch;create;update;patch;delete;deletecollection
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
	logger.Info("Processing reconcile request")

	namespaceStillExists, err := r.checkIfNamespaceExists(ctx, req.Namespace, &logger)
	if err != nil {
		// The error has already been logged in checkIfNamespaceExists.
		return ctrl.Result{}, err
	} else if !namespaceStillExists {
		logger.Info("The namespace seems to have been deleted after this reconcile request has been scheduled. " +
			"Ignoring the reconcile request.")
		return ctrl.Result{}, nil
	}

	dash0CustomResource, stopReconcile, err := r.verifyUniqueDash0CustomResourceExists(ctx, req, logger)
	if err != nil {
		return ctrl.Result{}, err
	} else if stopReconcile {
		return ctrl.Result{}, nil
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
		logger.Info("Initial reconcile in progress.")
		if err = r.checkSettingsAndInstrumentAllWorkloads(ctx, dash0CustomResource, &logger); err != nil {
			// The error has already been logged in checkSettingsAndInstrumentAllWorkloads
			logger.Info("Requeuing reconcile request.")
			return ctrl.Result{}, err
		}
	}

	dash0CustomResource.EnsureResourceIsMarkedAsAvailable()
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
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

func (r *Dash0Reconciler) verifyUniqueDash0CustomResourceExists(
	ctx context.Context,
	req ctrl.Request,
	logger logr.Logger,
) (*operatorv1alpha1.Dash0, bool, error) {
	dash0CustomResource, stopReconcile, err := r.verifyThatDash0CustomResourceExists(ctx, req, &logger)
	if err != nil || stopReconcile {
		return dash0CustomResource, stopReconcile, err
	}
	stopReconcile, err = r.verifyThatDash0CustomResourceIsUniqe(ctx, req, dash0CustomResource, &logger)
	return dash0CustomResource, stopReconcile, err
}

// verifyThatDash0CustomResourceExists loads the Dash0 custom resource that the current reconcile request applies to.
// If that resource does not exist, the function logs a message and returns (nil, true, nil) and expects the caller
// to stop the reconciliation (without requeing it). If any other error occurs while trying to fetch the resource, the
// function logs the error and returns (nil, true, err) and expects the caller to requeue the reconciliation.
func (r *Dash0Reconciler) verifyThatDash0CustomResourceExists(
	ctx context.Context,
	req ctrl.Request,
	logger *logr.Logger,
) (*operatorv1alpha1.Dash0, bool, error) {
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	err := r.Get(ctx, req.NamespacedName, dash0CustomResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(
				"The Dash0 custom resource has not been found, either it hasn't been installed or it has been " +
					"deleted. Ignoring the reconcile request.")
			// stop the reconciliation, and do not requeue it (that is, return (ctrl.Result{}, nil))
			return nil, true, nil
		}
		logger.Error(err, "Failed to get the Dash0 custom resource, requeuing reconcile request.")
		// requeue the reconciliation (that is, return (ctrl.Result{}, err))
		return nil, true, err
	}
	return dash0CustomResource, false, nil
}

// verifyThatDash0CustomResourceIsUniqe checks whether there are any additional Dash0 custom resources in the namespace,
// besides the one that the current reconcile request applies to.
func (r *Dash0Reconciler) verifyThatDash0CustomResourceIsUniqe(
	ctx context.Context,
	req ctrl.Request,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) (bool, error) {
	allDash0CustomResourcesInNamespace := &operatorv1alpha1.Dash0List{}
	if err := r.List(
		ctx,
		allDash0CustomResourcesInNamespace,
		&client.ListOptions{
			Namespace: req.Namespace,
		},
	); err != nil {
		logger.Error(err, "Failed to list all Dash0 custom resources, requeuing reconcile request.")
		return true, err
	}

	if len(allDash0CustomResourcesInNamespace.Items) > 1 {
		// There are multiple instances of the Dash0 custom resource in this namespace. If the resource that is
		// currently being reconciled is the one that has been most recently created, we assume that this is the source
		// of truth in terms of configuration settings etc., and we ignore the other instances in this reconcile request
		// (they will be handled when they are being reconciled). If the currently reconciled resource is not the most
		// recent one, we set its status to degraded.
		sort.Sort(SortByCreationTimestamp(allDash0CustomResourcesInNamespace.Items))
		mostRecentResource := allDash0CustomResourcesInNamespace.Items[len(allDash0CustomResourcesInNamespace.Items)-1]
		if mostRecentResource.UID == dash0CustomResource.UID {
			logger.Info(
				"At least one other Dash0 custom resource exists in this namespace. This Dash0 custom " +
					"resource is the most recent one. The state of the other resource(s) will be set to degraded.")
			// continue with the reconcile request for this resource, let the reconcile requests for the other offending
			// resources handle the situation for those resources
			return false, nil
		} else {
			logger.Info(
				"At least one other Dash0 custom resource exists in this namespace, and at least one other Dash0 "+
					"custom resource has been created more recently than this one. Setting the state of this resource "+
					"to degraded.",
				"most recently created Dash0 custom resource",
				fmt.Sprintf("%s (%s)", mostRecentResource.Name, mostRecentResource.UID),
			)
			dash0CustomResource.EnsureResourceIsMarkedAsDegraded(
				"NewerDash0CustomResourceIsPresent",
				"There is a more recently created Dash0 custom resource in this namespace, please remove all but one "+
					"resource instance.",
				"NewerDash0CustomResourceIsPresent",
				"There is a more recently created Dash0 custom resource in this namespace, please remove all but one "+
					"resource instance.",
			)
			if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
				logger.Error(err, updateStatusFailedMessage)
				return true, err
			}
			// stop the reconciliation, and do not requeue it
			return true, nil
		}
	}
	return false, nil
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

func (r *Dash0Reconciler) checkSettingsAndInstrumentAllWorkloads(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) error {
	instrumentWorkloads := util.ReadOptOutSetting(dash0CustomResource.Spec.InstrumentWorkloads)
	instrumentExistingWorkloads := util.ReadOptOutSetting(dash0CustomResource.Spec.InstrumentExistingWorkloads)
	instrumentNewWorkloads := util.ReadOptOutSetting(dash0CustomResource.Spec.InstrumentNewWorkloads)

	if !instrumentWorkloads || (!instrumentExistingWorkloads && !instrumentNewWorkloads) {
		logger.Info(
			"Instrumentation is not enabled, neither new nor existing workloads will be modified to send telemetry " +
				"to Dash0.",
		)
		return nil
	}
	if !instrumentExistingWorkloads {
		logger.Info(
			"Instrumenting existing workloads is not enabled, only new workloads will be modified (at deploy time) " +
				"to send telemetry to Dash0.",
		)
		return nil
	}

	logger.Info("Now instrumenting existing workloads in namespace so they send telemetry to Dash0.")
	if err := r.instrumentAllWorkloads(ctx, dash0CustomResource, logger); err != nil {
		logger.Error(err, "Instrumenting existing workloads failed.")
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
		logger.Error(err, "Cannot update the status of the Dash0 custom resource.")
		return err
	}
	return nil
}

func (r *Dash0Reconciler) instrumentAllWorkloads(
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
		r.ClientSet.BatchV1().CronJobs(namespace).List(ctx, util.EmptyListOptions)
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
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndInstrumentyDaemonSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().DaemonSets(namespace).List(ctx, util.EmptyListOptions)
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
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndInstrumentDeployments(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().Deployments(namespace).List(ctx, util.EmptyListOptions)
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
	}, reconcileLogger)
}

func (r *Dash0Reconciler) findAndAddLabelsToImmutableJobsOnInstrumentation(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.BatchV1().Jobs(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying jobs: %w", err)
	}

	for _, job := range matchingWorkloadsInNamespace.Items {
		r.handleJobJobOnInstrumentation(ctx, job, logger)
	}
	return nil
}

func (r *Dash0Reconciler) handleJobJobOnInstrumentation(
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
	var requiredAction ModificationMode
	modifyLabels := true
	createImmutableWorkloadsError := true
	if util.HasOptedOutOfInstrumenation(objectMeta) && util.InstrumenationAttemptHasFailed(objectMeta) {
		// There has been an unsuccessful attempt to instrument this job before, but now the user has added the opt-out
		// label, so we can remove the labels left over from that earlier attempt.
		// "requiredAction = Instrumentation" in the context of immutable jobs means "remove Dash0 labels from the job",
		// no other modification will take place.
		requiredAction = Uninstrumentation
		createImmutableWorkloadsError = false
	} else if util.HasOptedOutOfInstrumenation(objectMeta) && util.HasBeenInstrumentedSuccessfully(objectMeta) {
		// This job has been instrumented successfully, presumably by the webhook. Since then, the opt-out label has
		// been added. The correct action would be to uninstrument it, but since it is immutable, we cannot do that.
		// We will not actually modify this job at all, but create a log message and a corresponding event.
		modifyLabels = false
		requiredAction = Uninstrumentation
	} else if util.HasOptedOutOfInstrumenation(objectMeta) {
		// has opt-out label and there has been no previous instrumentation attempt
		logger.Info("not instrumenting this workload due to dash0.com/enable=false")
		return
	} else if util.HasBeenInstrumentedSuccessfully(objectMeta) || util.InstrumenationAttemptHasFailed(objectMeta) {
		// We already have instrumented this job (via the webhook) or have failed to instrument it, in either case,
		// there is nothing to do here.
		return
	} else {
		// We have not attempted to instrument this job yet, that is, we are seeing this job for the first time now.
		//
		// "requiredAction = Instrumentation" in the context of immutable jobs means "add labels to the job", no other
		// modification will (or can) take place.
		requiredAction = Instrumentation
	}

	retryErr := util.Retry("handling immutable job", func() error {
		if !modifyLabels {
			return nil
		}

		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
		}, &job); err != nil {
			return fmt.Errorf("error when fetching job %s/%s: %w", job.GetNamespace(), job.GetName(), err)
		}

		hasBeenModified := false
		switch requiredAction {
		case Instrumentation:
			hasBeenModified = newWorkloadModifier(r.Images, r.OtelCollectorBaseUrl, &logger).AddLabelsToImmutableJob(&job)
		case Uninstrumentation:
			hasBeenModified = newWorkloadModifier(r.Images, r.OtelCollectorBaseUrl, &logger).RemoveLabelsFromImmutableJob(&job)
		}

		if hasBeenModified {
			return r.Client.Update(ctx, &job)
		} else {
			return nil
		}
	}, &logger)

	postProcess := r.postProcessInstrumentation
	if requiredAction == Uninstrumentation {
		postProcess = r.postProcessUninstrumentation
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

func (r *Dash0Reconciler) findAndInstrumentReplicaSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err :=
		r.ClientSet.AppsV1().ReplicaSets(namespace).List(ctx, util.EmptyListOptions)
	if err != nil {
		return fmt.Errorf("error when querying replica sets: %w", err)
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
	hasBeenUpdated := r.instrumentWorkload(ctx, &replicaSetWorkload{
		replicaSet: &replicaSet,
	}, reconcileLogger)

	if hasBeenUpdated {
		r.restartPodsOfReplicaSet(ctx, replicaSet, reconcileLogger)
	}
}

func (r *Dash0Reconciler) findAndInstrumentStatefulSets(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	matchingWorkloadsInNamespace, err := r.ClientSet.AppsV1().StatefulSets(namespace).List(ctx, util.EmptyListOptions)
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
	}, reconcileLogger)
}

func (r *Dash0Reconciler) instrumentWorkload(
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

	var requiredAction ModificationMode
	if util.WasInstrumentedButHasOptedOutNow(objectMeta) {
		requiredAction = Uninstrumentation
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(objectMeta, r.Images) {
		// No change necessary, this workload has already been instrumented and an opt-out label (which would need to
		// trigger uninstrumentation) has not been added since it has been instrumented.
		logger.Info("not updating the existing instrumentation for this workload, it has already been successfully " +
			"instrumented by the same operator version")
		return false
	} else if util.HasOptedOutOfInstrumenationAndIsUninstrumented(workload.getObjectMeta()) {
		logger.Info("not instrumenting this workload due to dash0.com/enable=false")
		return false
	} else {
		requiredAction = Instrumentation
	}

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

		switch requiredAction {
		case Instrumentation:
			hasBeenModified = workload.instrument(r.Images, r.OtelCollectorBaseUrl, &logger)
		case Uninstrumentation:
			hasBeenModified = workload.revert(r.Images, r.OtelCollectorBaseUrl, &logger)
		}

		if hasBeenModified {
			return r.Client.Update(ctx, workload.asClientObject())
		} else {
			return nil
		}
	}, &logger)

	switch requiredAction {
	case Instrumentation:
		return r.postProcessInstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
	case Uninstrumentation:
		return r.postProcessUninstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
	}
	return false
}

func (r *Dash0Reconciler) postProcessInstrumentation(
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
		util.QueueFailedInstrumentationEvent(r.Recorder, resource, "controller", retryErr)
		return false
	} else if !hasBeenModified {
		// TODO This also happens for replica sets owned by a deployment and the log message as well as the message on
		// the event are unspecific, would be better if we could differentiate between the two cases.
		// (Also for revert maybe.)
		logger.Info("Dash0 instrumentation was already present on this workload, or the workload is part of a higher " +
			"order workload that will be instrumented, no modification by the controller is necessary.")
		util.QueueNoInstrumentationNecessaryEvent(r.Recorder, resource, "controller")
		return false
	} else {
		logger.Info("The controller has added Dash0 instrumentation to the workload.")
		util.QueueSuccessfulInstrumentationEvent(r.Recorder, resource, "controller")
		return true
	}
}

func (r *Dash0Reconciler) checkImminentDeletionAndHandleFinalizers(
	ctx context.Context,
	dash0CustomResource *operatorv1alpha1.Dash0,
	logger *logr.Logger,
) (bool, error) {
	isMarkedForDeletion := dash0CustomResource.IsMarkedForDeletion()
	if !isMarkedForDeletion {
		err := r.addFinalizerIfNecessary(ctx, dash0CustomResource)
		if err != nil {
			logger.Error(err, "Failed to add finalizer to Dash0 custom resource, requeuing reconcile request.")
			return isMarkedForDeletion, err
		}
	} else {
		if controllerutil.ContainsFinalizer(dash0CustomResource, operatorv1alpha1.FinalizerId) {
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
	uninstrumentWorkloadsOnDelete := util.ReadOptOutSetting(dash0CustomResource.Spec.UninstrumentWorkloadsOnDelete)
	if !uninstrumentWorkloadsOnDelete {
		logger.Info(
			"Reverting instrumentation modifications is not enabled, the Dash0 Kubernetes operator will not attempt " +
				"any changes made to workloads.",
		)
		return nil
	}

	err := r.uninstrumentWorkloadsIfAvailable(ctx, dash0CustomResource, logger)
	if err != nil {
		logger.Error(err, "Failed to uninstrument workloads, requeuing reconcile request.")
		return err
	}

	// The Dash0 custom resource will be deleted after this reconcile finished, so updating the status is
	// probably unnecessary. But for due process we do it anyway. In particular, if deleting it should fail
	// for any reason or take a while, the resource is no longer marked as available.
	dash0CustomResource.EnsureResourceIsMarkedAsAboutToBeDeleted()
	if err = r.Status().Update(ctx, dash0CustomResource); err != nil {
		logger.Error(err, updateStatusFailedMessage)
		return err
	}

	controllerutil.RemoveFinalizer(dash0CustomResource, operatorv1alpha1.FinalizerId)
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
	finalizerHasBeenAdded := controllerutil.AddFinalizer(dash0CustomResource, operatorv1alpha1.FinalizerId)
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

	// Note: In contrast to the instrumentation logic, there is no need to check for dash.com/enable=false here:
	// If it is set, the workload would not have been instrumented in the first place, hence the label selector filter
	// looking for dash0.com/instrumented=true would not have matched. Or if the workload is actually instrumented,
	// although it has dash0.com/enabled=false it must have been set after the instrumentation, in which case
	// uninstrumenting it is the correct thing to do.

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
			newWorkloadModifier(r.Images, r.OtelCollectorBaseUrl, &logger).RemoveLabelsFromImmutableJob(&job)

			// Apparently for jobs we do not need to set the "dash0.com/webhook-ignore-once" label, since changing their
			// labels does not trigger a new admission request.
			return r.Client.Update(ctx, &job)
		} else {
			// No dash0.com/instrumented label is present, do nothing.
			return nil
		}
	}, &logger)

	if retryErr != nil {
		// For the case that the job was instrumented, and we could not uninstrument it, we create a
		// ImmutableWorkloadError inside the retry loop. This error is then handled in the postProcessUninstrumentation.
		// The same is true for any other error types (for example errors in r.ClientUpdate).
		r.postProcessUninstrumentation(&job, false, retryErr, &logger)
	} else if createImmutableWorkloadsError {
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
	hasBeenUpdated := r.revertWorkloadInstrumentation(ctx, &replicaSetWorkload{
		replicaSet: &replicaSet,
	}, reconcileLogger)

	if hasBeenUpdated {
		r.restartPodsOfReplicaSet(ctx, replicaSet, reconcileLogger)
	}
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
	}, reconcileLogger)
}

func (r *Dash0Reconciler) revertWorkloadInstrumentation(
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

	// Note: In contrast to the instrumentation logic, there is no need to check for dash.com/enable=false here:
	// If it is set, the workload would not have been instrumented in the first place, hence the label selector filter
	// looking for dash0.com/instrumented=true would not have matched. Or if the workload is actually instrumented,
	// although it has dash0.com/enabled=false it must have been set after the instrumentation, in which case
	// uninstrumenting it is the correct thing to do.

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
		hasBeenModified = workload.revert(r.Images, r.OtelCollectorBaseUrl, &logger)
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

	return r.postProcessUninstrumentation(workload.asRuntimeObject(), hasBeenModified, retryErr, &logger)
}

func (r *Dash0Reconciler) postProcessUninstrumentation(
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
		util.QueueFailedUninstrumentationEvent(r.Recorder, resource, "controller", retryErr)
		return false
	} else if !hasBeenModified {
		logger.Info("Dash0 instrumentations was not present on this workload, no modification by the controller has " +
			"been necessary.")
		util.QueueNoUninstrumentationNecessaryEvent(r.Recorder, resource, "controller")
		return false
	} else {
		logger.Info("The controller has removed the Dash0 instrumentation from the workload.")
		util.QueueSuccessfulUninstrumentationEvent(r.Recorder, resource, "controller")
		return true
	}
}

func newWorkloadModifier(images util.Images, otelCollectorBaseUrl string, logger *logr.Logger) *workloads.ResourceModifier {
	return workloads.NewResourceModifier(
		util.InstrumentationMetadata{
			Images:               images,
			InstrumentedBy:       "controller",
			OtelCollectorBaseUrl: otelCollectorBaseUrl,
		},
		logger,
	)
}

type SortByCreationTimestamp []operatorv1alpha1.Dash0

func (s SortByCreationTimestamp) Len() int {
	return len(s)
}
func (s SortByCreationTimestamp) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s SortByCreationTimestamp) Less(i, j int) bool {
	return s[i].CreationTimestamp.Before(&s[j].CreationTimestamp)
}

func (r *Dash0Reconciler) restartPodsOfReplicaSet(
	ctx context.Context,
	replicaSet appsv1.ReplicaSet,
	logger *logr.Logger,
) {
	// Note: ReplicaSet pods are not restarted automatically by Kubernetes when their spec is changed (for other
	// resource types like deployments or daemonsets this is managed by Kubernetes automatically). Therefore, we
	// find all pods owned by the replica set and explicitly delete them to trigger a restart.
	allPodsInNamespace, err :=
		r.ClientSet.
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
		err := r.Client.Delete(ctx, &pod)
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
