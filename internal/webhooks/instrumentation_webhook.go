// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/dash0monitoring/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

type InstrumentationWebhookHandler struct {
	Client               client.Client
	Recorder             record.EventRecorder
	Images               util.Images
	ExtraConfig          util.ExtraConfig
	OTelCollectorBaseUrl string
}

type resourceHandler func(h *InstrumentationWebhookHandler, request admission.Request, gvkLabel string, logger *logr.Logger) admission.Response
type routing map[string]map[string]map[string]resourceHandler

const (
	optOutAdmissionAllowedMessage    = "not instrumenting this workload due to dash0.com/enable=false"
	sameVersionNoModificationMessage = "not updating the existing instrumentation for this workload, it has already " +
		"been successfully instrumented by the same operator version"
	actor = util.ActorWebhook
)

var (
	log     = logf.Log.WithName("instrumentation-webhook")
	decoder = scheme.Codecs.UniversalDecoder()

	routes = routing{
		"": {
			"Pod": {
				"v1": (*InstrumentationWebhookHandler).handlePod,
			},
		},
		"batch": {
			"CronJob": {
				"v1": (*InstrumentationWebhookHandler).handleCronJob,
			},
			"Job": {
				"v1": (*InstrumentationWebhookHandler).handleJob,
			},
		},
		"apps": {
			"DaemonSet": {
				"v1": (*InstrumentationWebhookHandler).handleDaemonSet,
			},
			"Deployment": {
				"v1": (*InstrumentationWebhookHandler).handleDeployment,
			},
			"ReplicaSet": {
				"v1": (*InstrumentationWebhookHandler).handleReplicaSet,
			},
			"StatefulSet": {
				"v1": (*InstrumentationWebhookHandler).handleStatefulSet,
			},
		},
	}

	fallbackRoute resourceHandler = func(
		h *InstrumentationWebhookHandler,
		request admission.Request,
		gvkLabel string,
		logger *logr.Logger,
	) admission.Response {
		return logAndReturnAllowed(fmt.Sprintf("resource type not supported: %s", gvkLabel), logger)
	}
)

func (h *InstrumentationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/v1alpha1/inject/dash0", handler)

	return nil
}

func (h *InstrumentationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	logger := log.WithValues(
		"operation",
		request.Operation,
		"gvk",
		request.Kind,
		"namespace",
		request.Namespace,
		"name",
		request.Name,
	)

	targetNamespace := request.Namespace

	dash0List := &dash0v1alpha1.Dash0MonitoringList{}
	if err := h.Client.List(ctx, dash0List, &client.ListOptions{
		Namespace: targetNamespace,
	}); err != nil {
		if apierrors.IsNotFound(err) {
			return admission.Allowed(fmt.Sprintf(
				"There is no Dash0 monitoring resource in the namespace %s, the workload will not be instrumented by "+
					"the webhook.",
				targetNamespace,
			))
		} else {
			// Ideally we would queue a failed instrumentation event here, but we didn't decode the workload resource
			// yet, so there is nothing to bind the event to.
			return logErrorAndReturnAllowed(
				fmt.Errorf(
					"failed to list Dash0 monitoring resources in namespace %s, workload will not be instrumented: %w",
					targetNamespace,
					err,
				),
				&logger,
			)
		}
	}

	if len(dash0List.Items) == 0 {
		return admission.Allowed(
			fmt.Sprintf(
				"There is no Dash0 monitoring resource in the namespace %s, the workload will not be instrumented by "+
					"the webhook.",
				targetNamespace,
			))
	}

	dash0MonitoringResource := dash0List.Items[0]

	if !dash0MonitoringResource.IsAvailable() {
		return admission.Allowed(fmt.Sprintf(
			"The Dash0 monitoring resource in the namespace %s is not in status available, this workload will "+
				"not be modified to send telemetry to Dash0.", targetNamespace))
	}
	if dash0MonitoringResource.IsMarkedForDeletion() {
		return logAndReturnAllowed(
			fmt.Sprintf(
				"The Dash0 monitoring resource in the namespace %s is about to be deleted, this workload will not be "+
					"modified to send telemetry to Dash0.", targetNamespace), &logger)
	}

	actionPartial := "newly deployed"
	if request.Operation == admissionv1.Update {
		actionPartial = "updated"
	}
	instrumentWorkloads := dash0MonitoringResource.ReadInstrumentWorkloadsSetting()
	if instrumentWorkloads == dash0v1alpha1.None {
		return admission.Allowed(
			fmt.Sprintf(
				"Instrumenting workloads is not enabled in namespace %s, this %s workload will not be modified to "+
					"send telemetry to Dash0.",
				targetNamespace,
				actionPartial,
			),
		)
	}

	gkv := request.Kind
	group := gkv.Group
	version := gkv.Version
	kind := gkv.Kind
	gvkLabel := fmt.Sprintf("%s/%s.%s", group, version, kind)

	return routes.routeFor(group, kind, version)(h, request, gvkLabel, &logger)
}

func (h *InstrumentationWebhookHandler) handleCronJob(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	cronJob := &batchv1.CronJob{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, cronJob, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&cronJob.ObjectMeta) {
		return h.postProcessInstrumentation(
			request,
			cronJob,
			workloads.NewIgnoredOnceResult(),
			false,
			logger,
		)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&cronJob.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&cronJob.ObjectMeta) {
		modificationResult := h.newWorkloadModifier(logger).RevertCronJob(cronJob)
		return h.postProcessUninstrumentation(request, cronJob, modificationResult, logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&cronJob.ObjectMeta, h.Images) {
		// deliberately not logging this, would be very noisy
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyCronJob(cronJob)
		return h.postProcessInstrumentation(request, cronJob, modificationResult, false, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleDaemonSet(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	daemonSet := &appsv1.DaemonSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, daemonSet, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&daemonSet.ObjectMeta) {
		return h.postProcessInstrumentation(request, daemonSet, workloads.NewIgnoredOnceResult(), false, logger)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&daemonSet.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&daemonSet.ObjectMeta) {
		modificationResult := h.newWorkloadModifier(logger).RevertDaemonSet(daemonSet)
		return h.postProcessUninstrumentation(request, daemonSet, modificationResult, logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&daemonSet.ObjectMeta, h.Images) {
		// deliberately not logging this, would be very noisy
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyDaemonSet(daemonSet)
		return h.postProcessInstrumentation(request, daemonSet, modificationResult, false, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleDeployment(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	deployment := &appsv1.Deployment{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, deployment, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&deployment.ObjectMeta) {
		return h.postProcessInstrumentation(request, deployment, workloads.NewIgnoredOnceResult(), false, logger)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&deployment.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&deployment.ObjectMeta) {
		modificationResult := h.newWorkloadModifier(logger).RevertDeployment(deployment)
		return h.postProcessUninstrumentation(request, deployment, modificationResult, logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&deployment.ObjectMeta, h.Images) {
		// deliberately not logging this, would be very noisy
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyDeployment(deployment)
		return h.postProcessInstrumentation(request, deployment, modificationResult, false, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleJob(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	job := &batchv1.Job{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, job, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&job.ObjectMeta) {
		return h.postProcessInstrumentation(request, job, workloads.NewIgnoredOnceResult(), false, logger)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&job.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&job.ObjectMeta) {
		// This should not happen, since it can only happen for an admission request with operation=UPDATE, and we are
		// not listening to updates for jobs. We cannot uninstrument jobs if the user adds an opt-out label after the
		// job has been already instrumented, since jobs are immutable.
		return h.postProcessUninstrumentation(request, job, workloads.NewNotModifiedImmutableWorkloadCannotBeRevertedResult(), logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&job.ObjectMeta, h.Images) {
		// This should not happen either.
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyJob(job)
		return h.postProcessInstrumentation(request, job, modificationResult, false, logger)
	}
}

func (h *InstrumentationWebhookHandler) handlePod(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	pod := &corev1.Pod{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, pod, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&pod.ObjectMeta) {
		return h.postProcessInstrumentation(request, pod, workloads.NewIgnoredOnceResult(), true, logger)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&pod.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&pod.ObjectMeta) {
		// This should not happen, since it can only happen for an admission request with operation=UPDATE, and we are
		// not listening to updates for pods. We cannot uninstrument ownerless pods if the user adds an opt-out label
		// after the pod has been already instrumented, since we cannot restart ownerless pods, which makes them
		// effectively immutable.
		return h.postProcessUninstrumentation(request, pod, workloads.NewNotModifiedImmutableWorkloadCannotBeRevertedResult(), logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&pod.ObjectMeta, h.Images) {
		// This should not happen either.
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyPod(pod)
		return h.postProcessInstrumentation(request, pod, modificationResult, true, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleReplicaSet(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	replicaSet := &appsv1.ReplicaSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, replicaSet, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&replicaSet.ObjectMeta) {
		return h.postProcessInstrumentation(request, replicaSet, workloads.NewIgnoredOnceResult(), false, logger)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&replicaSet.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&replicaSet.ObjectMeta) {
		modificationResult := h.newWorkloadModifier(logger).RevertReplicaSet(replicaSet)
		return h.postProcessUninstrumentation(request, replicaSet, modificationResult, logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&replicaSet.ObjectMeta, h.Images) {
		// deliberately not logging this, would be very noisy
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyReplicaSet(replicaSet)
		return h.postProcessInstrumentation(request, replicaSet, modificationResult, false, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleStatefulSet(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	statefulSet := &appsv1.StatefulSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, statefulSet, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&statefulSet.ObjectMeta) {
		return h.postProcessInstrumentation(request, statefulSet, workloads.NewIgnoredOnceResult(), false, logger)
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(&statefulSet.ObjectMeta) {
		return logAndReturnAllowed(optOutAdmissionAllowedMessage, logger)
	} else if util.WasInstrumentedButHasOptedOutNow(&statefulSet.ObjectMeta) {
		modificationResult := h.newWorkloadModifier(logger).RevertStatefulSet(statefulSet)
		return h.postProcessUninstrumentation(request, statefulSet, modificationResult, logger)
	} else if util.HasBeenInstrumentedSuccessfullyByThisVersion(&statefulSet.ObjectMeta, h.Images) {
		// deliberately not logging this, would be very noisy
		return admission.Allowed(sameVersionNoModificationMessage)
	} else {
		modificationResult := h.newWorkloadModifier(logger).ModifyStatefulSet(statefulSet)
		return h.postProcessInstrumentation(request, statefulSet, modificationResult, false, logger)
	}
}

func (h *InstrumentationWebhookHandler) preProcess(
	request admission.Request,
	gvkLabel string,
	resource runtime.Object,
	logger *logr.Logger,
) (admission.Response, bool) {
	if _, _, err := decoder.Decode(request.Object.Raw, nil, resource); err != nil {
		wrappedErr := fmt.Errorf("cannot parse resource into a %s: %w", gvkLabel, err)
		util.QueueFailedInstrumentationEvent(h.Recorder, resource, "webhook", wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger), true
	}
	return admission.Response{}, false
}

func (h *InstrumentationWebhookHandler) postProcessInstrumentation(
	request admission.Request,
	resource runtime.Object,
	modificationResult workloads.ModificationResult,
	isPod bool,
	logger *logr.Logger,
) admission.Response {
	if !modificationResult.IgnoredOnce && !modificationResult.HasBeenModified {
		msg := modificationResult.RenderReasonMessage(actor)
		if !modificationResult.SkipLogging {
			logger.Info(msg)
		}
		if !isPod {
			util.QueueNoInstrumentationNecessaryEvent(h.Recorder, resource, msg)
		}
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(resource)
	if err != nil {
		wrappedErr := fmt.Errorf("error when marshalling modfied resource to JSON: %w", err)
		util.QueueFailedInstrumentationEvent(h.Recorder, resource, "webhook", wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger)
	}

	// For ignored workloads, we still need to return PatchResponseFromRaw. Although we do not modify the workload
	// itself, we do need to remove the ignore-once label, which is a modification.
	if modificationResult.IgnoredOnce {
		logger.Info(modificationResult.RenderReasonMessage(actor))
		// deliberately not queueing an event for this case
		return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
	}

	logger.Info("The webhook has added Dash0 instrumentation to the workload.")
	util.QueueSuccessfulInstrumentationEvent(h.Recorder, resource, "webhook")
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *InstrumentationWebhookHandler) postProcessUninstrumentation(
	request admission.Request,
	resource runtime.Object,
	modificationResult workloads.ModificationResult,
	logger *logr.Logger,
) admission.Response {
	if modificationResult.ImmutableWorkload {
		err := errors.New(modificationResult.RenderReasonMessage(actor))
		util.QueueFailedUninstrumentationEvent(h.Recorder, resource, actor, err)
		logger.Info(err.Error())
		return admission.Allowed(err.Error())
	}

	if !modificationResult.HasBeenModified {
		if !modificationResult.SkipLogging {
			logger.Info(modificationResult.RenderReasonMessage(actor))
		}
		util.QueueNoUninstrumentationNecessaryEvent(h.Recorder, resource, actor)
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(resource)
	if err != nil {
		wrappedErr := fmt.Errorf("error when marshalling modfied resource to JSON: %w", err)
		util.QueueFailedUninstrumentationEvent(h.Recorder, resource, actor, wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger)
	}

	logger.Info("The webhook has removed the Dash0 instrumentation from the workload.")
	util.QueueSuccessfulUninstrumentationEvent(h.Recorder, resource, actor)
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *InstrumentationWebhookHandler) newWorkloadModifier(logger *logr.Logger) *workloads.ResourceModifier {
	return workloads.NewResourceModifier(
		util.InstrumentationMetadata{
			Images:               h.Images,
			InstrumentedBy:       actor,
			OTelCollectorBaseUrl: h.OTelCollectorBaseUrl,
		},
		h.ExtraConfig,
		logger,
	)
}

func (r *routing) routeFor(group, kind, version string) resourceHandler {
	routesForGroup := (*r)[group]
	if routesForGroup == nil {
		return nil
	}
	routesForKind := routesForGroup[kind]
	if routesForKind == nil {
		return nil
	}
	routesForVersion := routesForKind[version]
	if routesForVersion == nil {
		return fallbackRoute
	}
	return routesForVersion
}

func logAndReturnAllowed(message string, logger *logr.Logger) admission.Response {
	logger.Info(message)
	return admission.Allowed(message)
}

func logErrorAndReturnAllowed(err error, logger *logr.Logger) admission.Response {
	logger.Error(err, "an error occurred while processing the admission request")

	// Note: We never return admission.Errored or admission.Denied, even in case an error happens, because we do not
	// want to block the deployment of workloads. If instrumenting a new workload fails it is not great, but having
	// the webhook actually getting in the way of deploying a workload would be much worse.
	return admission.Allowed(err.Error())
}
