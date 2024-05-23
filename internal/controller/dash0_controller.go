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
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/k8sresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	resourceTypeLabel      = "resource type"
	resourceNamespaceLabel = "resource namespace"
	resourceNameLabel      = "resource name"
)

type Dash0Reconciler struct {
	client.Client
	ClientSet *kubernetes.Clientset
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Versions  util.Versions
}

type ImmutableResourceError struct {
	resourceType string
	resource     string
}

func (e ImmutableResourceError) Error() string {
	return fmt.Sprintf(
		"Dash0 cannot instrument the existing %s %s, since the this type of resource is immutable.",
		e.resourceType,
		e.resource,
	)
}

func (r *Dash0Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Dash0{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.dash0.com,resources=dash0s/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create
//+kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;replicasets;statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

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
	log := log.FromContext(ctx)
	log.Info("Processig reconcile request")

	// Check whether the Dash0 custom resource exists.
	dash0CustomResource := &operatorv1alpha1.Dash0{}
	err := r.Get(ctx, req.NamespacedName, dash0CustomResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("The Dash0 custom resource has not been found, either it hasn't been installed or it has been deleted. Ignoring the reconcile request.")
			// stop the reconciliation
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get the Dash0 custom resource, requeuing reconcile request.")
		// requeue the request.
		return ctrl.Result{}, err
	}

	isFirstReconcile, err := r.initStatusConditions(ctx, dash0CustomResource, &log)
	if err != nil {
		return ctrl.Result{}, err
	}

	isMarkedForDeletion := r.handleFinalizers(dash0CustomResource)
	if isMarkedForDeletion {
		// Dash0 is marked for deletion, no further reconciliation is necessary.
		return ctrl.Result{}, nil
	}

	if isFirstReconcile {
		r.handleFirstReconcile(ctx, dash0CustomResource, &log)
	}

	ensureResourceIsMarkedAsAvailable(dash0CustomResource)
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Failed to update Dash0 status conditions, requeuing reconcile request.")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Dash0Reconciler) initStatusConditions(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0, log *logr.Logger) (bool, error) {
	firstReconcile := false
	needsRefresh := false
	if dash0CustomResource.Status.Conditions == nil || len(dash0CustomResource.Status.Conditions) == 0 {
		setAvailableConditionToUnknown(dash0CustomResource)
		firstReconcile = true
		needsRefresh = true
	} else if availableCondition := meta.FindStatusCondition(dash0CustomResource.Status.Conditions, string(util.ConditionTypeAvailable)); availableCondition == nil {
		setAvailableConditionToUnknown(dash0CustomResource)
		needsRefresh = true
	}
	if needsRefresh {
		err := r.refreshStatus(ctx, dash0CustomResource, log)
		if err != nil {
			return firstReconcile, err
		}
	}
	return firstReconcile, nil
}

func (r *Dash0Reconciler) handleFinalizers(dash0CustomResource *operatorv1alpha1.Dash0) bool {
	isMarkedForDeletion := dash0CustomResource.GetDeletionTimestamp() != nil
	// if !isMarkedForDeletion {
	//   add finalizers here
	// } else /* if has finalizers */ {
	//   execute finalizers here
	// }
	return isMarkedForDeletion
}

func (r *Dash0Reconciler) handleFirstReconcile(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0, log *logr.Logger) {
	log.Info("Initial reconcile in progress.")
	instrumentationEnabled := true
	instrumentingExistingResourcesEnabled := true
	if !instrumentationEnabled {
		log.Info(
			"Instrumentation is not enabled, neither new nor existing resources will be modified to send telemetry to Dash0.",
		)
		return
	}

	if !instrumentingExistingResourcesEnabled {
		log.Info(
			"Instrumenting existing resources is not enabled, only new resources will be modified (at deploy time) to send telemetry to Dash0.",
		)
		return
	}

	log.Info("Modifying existing resources to make them send telemetry to Dash0.")
	if err := r.modifyExistingResources(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Modifying existing resources failed.")
	}
}

func (r *Dash0Reconciler) refreshStatus(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0, log *logr.Logger) error {
	if err := r.Status().Update(ctx, dash0CustomResource); err != nil {
		log.Error(err, "Cannot update the status of the Dash0 custom resource, requeuing reconcile request.")
		return err
	}
	return nil
}

func (r *Dash0Reconciler) modifyExistingResources(ctx context.Context, dash0CustomResource *operatorv1alpha1.Dash0) error {
	namespace := dash0CustomResource.Namespace

	errCronJobs := r.findAndModifyCronJobs(ctx, namespace)
	errDaemonSets := r.findAndModifyDaemonSets(ctx, namespace)
	errDeployments := r.findAndModifyDeployments(ctx, namespace)
	errJobs := r.findAndHandleJobs(ctx, namespace)
	errReplicaSets := r.findAndModifyReplicaSets(ctx, namespace)
	errStatefulSets := r.findAndModifyStatefulSets(ctx, namespace)
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

func (r *Dash0Reconciler) findAndModifyCronJobs(ctx context.Context, namespace string) error {
	matchingResourcesInNamespace, err := r.ClientSet.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error when querying cron jobs: %w", err)
	}
	for _, resource := range matchingResourcesInNamespace.Items {
		r.modifyCronJob(ctx, resource)
	}
	return nil
}

func (r *Dash0Reconciler) modifyCronJob(ctx context.Context, cronJob batchv1.CronJob) {
	if cronJob.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := log.FromContext(ctx).WithValues(
		resourceTypeLabel,
		"CronJob",
		resourceNamespaceLabel,
		cronJob.GetNamespace(),
		resourceNameLabel,
		cronJob.GetName(),
	)
	hasBeenModified := false
	retryErr := util.Retry("modifying cron job", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: cronJob.GetNamespace(),
			Name:      cronJob.GetName(),
		}, &cronJob); err != nil {
			return fmt.Errorf("error when fetching cron job %s/%s: %w", cronJob.GetNamespace(), cronJob.GetName(), err)
		}
		hasBeenModified = r.newResourceModifier(&logger).ModifyCronJob(&cronJob, cronJob.GetNamespace())
		if hasBeenModified {
			return r.Client.Update(ctx, &cronJob)
		} else {
			return nil
		}
	}, &logger)

	r.postProcess(&cronJob, hasBeenModified, logger, retryErr)
}

func (r *Dash0Reconciler) findAndModifyDaemonSets(ctx context.Context, namespace string) error {
	matchingResourcesInNamespace, err := r.ClientSet.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error when querying daemon sets: %w", err)
	}
	for _, resource := range matchingResourcesInNamespace.Items {
		r.modifyDaemonSet(ctx, resource)
	}
	return nil
}

func (r *Dash0Reconciler) modifyDaemonSet(ctx context.Context, daemonSet appsv1.DaemonSet) {
	if daemonSet.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := log.FromContext(ctx).WithValues(
		resourceTypeLabel,
		"DaemonSet",
		resourceNamespaceLabel,
		daemonSet.GetNamespace(),
		resourceNameLabel,
		daemonSet.GetName(),
	)
	hasBeenModified := false
	retryErr := util.Retry("modifying daemon set", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: daemonSet.GetNamespace(),
			Name:      daemonSet.GetName(),
		}, &daemonSet); err != nil {
			return fmt.Errorf("error when fetching daemon set %s/%s: %w", daemonSet.GetNamespace(), daemonSet.GetName(), err)
		}
		hasBeenModified = r.newResourceModifier(&logger).ModifyDaemonSet(&daemonSet, daemonSet.GetNamespace())
		if hasBeenModified {
			return r.Client.Update(ctx, &daemonSet)
		} else {
			return nil
		}
	}, &logger)

	r.postProcess(&daemonSet, hasBeenModified, logger, retryErr)
}

func (r *Dash0Reconciler) findAndModifyDeployments(ctx context.Context, namespace string) error {
	matchingResourcesInNamespace, err := r.ClientSet.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error when querying deployments: %w", err)
	}
	for _, resource := range matchingResourcesInNamespace.Items {
		r.modifyDeployment(ctx, resource)
	}
	return nil
}

func (r *Dash0Reconciler) modifyDeployment(ctx context.Context, deployment appsv1.Deployment) {
	if deployment.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := log.FromContext(ctx).WithValues(
		resourceTypeLabel,
		"Deployment",
		resourceNamespaceLabel,
		deployment.GetNamespace(),
		resourceNameLabel,
		deployment.GetName(),
	)
	hasBeenModified := false
	retryErr := util.Retry("modifying deployment", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: deployment.GetNamespace(),
			Name:      deployment.GetName(),
		}, &deployment); err != nil {
			return fmt.Errorf("error when fetching deployment %s/%s: %w", deployment.GetNamespace(), deployment.GetName(), err)
		}
		hasBeenModified = r.newResourceModifier(&logger).ModifyDeployment(&deployment, deployment.GetNamespace())
		if hasBeenModified {
			return r.Client.Update(ctx, &deployment)
		} else {
			return nil
		}
	}, &logger)

	r.postProcess(&deployment, hasBeenModified, logger, retryErr)
}

func (r *Dash0Reconciler) findAndHandleJobs(ctx context.Context, namespace string) error {
	matchingResourcesInNamespace, err := r.ClientSet.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error when querying cron jobs: %w", err)
	}

	for _, job := range matchingResourcesInNamespace.Items {
		r.handleJob(ctx, job)
	}
	return nil
}

func (r *Dash0Reconciler) handleJob(ctx context.Context, job batchv1.Job) {
	if job.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := log.FromContext(ctx).WithValues(
		resourceTypeLabel,
		"Job",
		resourceNamespaceLabel,
		job.GetNamespace(),
		resourceNameLabel,
		job.GetName(),
	)
	retryErr := util.Retry("labelling immutable job", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
		}, &job); err != nil {
			return fmt.Errorf("error when fetching job %s/%s: %w", job.GetNamespace(), job.GetName(), err)
		}
		r.newResourceModifier(&logger).AddLabelsToImmutableJob(&job)
		return r.Client.Update(ctx, &job)
	}, &logger)

	if retryErr != nil {
		r.postProcess(&job, false, logger, retryErr)
	} else {
		r.postProcess(
			&job,
			false,
			logger,
			ImmutableResourceError{
				resourceType: "job",
				resource:     fmt.Sprintf("%s/%s", job.GetNamespace(), job.GetName()),
			},
		)
	}
}

func (r *Dash0Reconciler) findAndModifyReplicaSets(ctx context.Context, namespace string) error {
	matchingResourcesInNamespace, err := r.ClientSet.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error when querying deployments: %w", err)
	}
	for _, resource := range matchingResourcesInNamespace.Items {
		r.modifyReplicaSet(ctx, resource)
	}
	return nil
}

func (r *Dash0Reconciler) modifyReplicaSet(ctx context.Context, replicaSet appsv1.ReplicaSet) {
	if replicaSet.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := log.FromContext(ctx).WithValues(
		resourceTypeLabel,
		"ReplicaSet",
		resourceNamespaceLabel,
		replicaSet.GetNamespace(),
		resourceNameLabel,
		replicaSet.GetName(),
	)
	hasBeenModified := false
	retryErr := util.Retry("modifying replicaset", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: replicaSet.GetNamespace(),
			Name:      replicaSet.GetName(),
		}, &replicaSet); err != nil {
			return fmt.Errorf("error when fetching replicaset %s/%s: %w", replicaSet.GetNamespace(), replicaSet.GetName(), err)
		}
		hasBeenModified = r.newResourceModifier(&logger).ModifyReplicaSet(&replicaSet, replicaSet.GetNamespace())
		if hasBeenModified {
			return r.Client.Update(ctx, &replicaSet)
		} else {
			return nil
		}
	}, &logger)

	// Note: ReplicaSet pods are not restarted automatically by Kubernetes when their spec is change (for other resource
	// types like deployments or daemonsets this is managed by Kubernetes automatically). For now, we rely on the user
	// to manually restart the pods of their replica sets after they have been instrumented. We could consider finding
	// all pods for that are owned by the replica set and restart them automatically.

	r.postProcess(&replicaSet, hasBeenModified, logger, retryErr)
}

func (r *Dash0Reconciler) findAndModifyStatefulSets(ctx context.Context, namespace string) error {
	matchingResourcesInNamespace, err := r.ClientSet.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error when querying stateful sets: %w", err)
	}
	for _, resource := range matchingResourcesInNamespace.Items {
		r.modifyStatefulSet(ctx, resource)
	}
	return nil
}

func (r *Dash0Reconciler) modifyStatefulSet(ctx context.Context, statefulSet appsv1.StatefulSet) {
	if statefulSet.DeletionTimestamp != nil {
		// do not modify resources that are being deleted
		return
	}
	logger := log.FromContext(ctx).WithValues(
		resourceTypeLabel,
		"StatefulSet",
		resourceNamespaceLabel,
		statefulSet.GetNamespace(),
		resourceNameLabel,
		statefulSet.GetName(),
	)
	hasBeenModified := false
	retryErr := util.Retry("modifying stateful set", func() error {
		if err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: statefulSet.GetNamespace(),
			Name:      statefulSet.GetName(),
		}, &statefulSet); err != nil {
			return fmt.Errorf("error when fetching stateful set %s/%s: %w", statefulSet.GetNamespace(), statefulSet.GetName(), err)
		}
		hasBeenModified = r.newResourceModifier(&logger).ModifyStatefulSet(&statefulSet, statefulSet.GetNamespace())
		if hasBeenModified {
			return r.Client.Update(ctx, &statefulSet)
		} else {
			return nil
		}
	}, &logger)

	r.postProcess(&statefulSet, hasBeenModified, logger, retryErr)
}

func (r *Dash0Reconciler) newResourceModifier(logger *logr.Logger) *k8sresources.ResourceModifier {
	return k8sresources.NewResourceModifier(
		util.InstrumentationMetadata{
			Versions:       r.Versions,
			InstrumentedBy: "controller",
		},
		logger,
	)
}

func (r *Dash0Reconciler) postProcess(
	resource runtime.Object,
	hasBeenModified bool,
	logger logr.Logger,
	retryErr error,
) {
	if retryErr != nil {
		e := &ImmutableResourceError{}
		if errors.As(retryErr, e) {
			logger.Info(e.Error())
		} else {
			logger.Error(retryErr, "Dash0 instrumentation by controller has not been successful.")
		}
		util.QueueFailedInstrumentationEvent(r.Recorder, resource, "controller", retryErr)
	} else if !hasBeenModified {
		logger.Info("Dash0 instrumentation already present, no modification by controller is necessary.")
		util.QueueAlreadyInstrumentedEvent(r.Recorder, resource, "controller")
	} else {
		logger.Info("The controller has added Dash0 instrumentation to the resource.")
		util.QueueSuccessfulInstrumentationEvent(r.Recorder, resource, "controller")
	}
}
