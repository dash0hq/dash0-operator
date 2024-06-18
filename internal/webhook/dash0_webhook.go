// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
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

	operatorv1alpha1 "github.com/dash0hq/dash0-operator/api/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

type Handler struct {
	Client               client.Client
	Recorder             record.EventRecorder
	Images               util.Images
	OtelCollectorBaseUrl string
}

type resourceHandler func(h *Handler, request admission.Request, gvkLabel string, logger *logr.Logger) admission.Response
type routing map[string]map[string]map[string]resourceHandler

const (
	optOutAdmissionAllowedMessage = "not instrumenting this workload due to dash0.com/enable=false"
)

var (
	log     = logf.Log.WithName("dash0-webhook")
	decoder = scheme.Codecs.UniversalDecoder()

	routes = routing{
		"": {
			"Pod": {
				"v1": (*Handler).handlePod,
			},
		},
		"batch": {
			"CronJob": {
				"v1": (*Handler).handleCronJob,
			},
			"Job": {
				"v1": (*Handler).handleJob,
			},
		},
		"apps": {
			"DaemonSet": {
				"v1": (*Handler).handleDaemonSet,
			},
			"Deployment": {
				"v1": (*Handler).handleDeployment,
			},
			"ReplicaSet": {
				"v1": (*Handler).handleReplicaSet,
			},
			"StatefulSet": {
				"v1": (*Handler).handleStatefulSet,
			},
		},
	}

	fallbackRoute resourceHandler = func(
		h *Handler,
		request admission.Request,
		gvkLabel string,
		logger *logr.Logger,
	) admission.Response {
		msg := fmt.Sprintf("resource type not supported: %s", gvkLabel)
		logger.Info(msg)
		return admission.Allowed(msg)
	}
)

func (h *Handler) SetupWebhookWithManager(mgr ctrl.Manager) error {
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

func (h *Handler) Handle(ctx context.Context, request admission.Request) admission.Response {
	logger := log.WithValues("gvk", request.Kind, "namespace", request.Namespace, "name", request.Name)
	logger.Info("incoming admission request")

	targetNamespace := request.Namespace

	dash0List := &operatorv1alpha1.Dash0List{}
	if err := h.Client.List(ctx, dash0List, &client.ListOptions{
		Namespace: targetNamespace,
	}); err != nil {
		if apierrors.IsNotFound(err) {
			return admission.Allowed(
				fmt.Sprintf(
					"There is no Dash0 custom resource in the namespace %s, the workload will not be instrumented.",
					targetNamespace,
				))
		} else {
			message := fmt.Sprintf("Failed to list Dash0 custom resources in namespace %s, workload will not be instrumented.", targetNamespace)
			logger.Error(err, message)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("%s - %w", message, err))
		}
	}
	if len(dash0List.Items) == 0 {
		return admission.Allowed(
			fmt.Sprintf(
				"There is no Dash0 custom resource in the namespace %s, the workload will not be instrumented.",
				targetNamespace,
			))
	}

	dash0CustomResource := dash0List.Items[0]
	instrumentWorkloads := util.ReadOptOutSetting(dash0CustomResource.Spec.InstrumentWorkloads)
	instrumentNewWorkloads := util.ReadOptOutSetting(dash0CustomResource.Spec.InstrumentNewWorkloads)
	if !instrumentWorkloads {
		message := fmt.Sprintf("Instrumenting workloads is not enabled in namespace %s, this newly deployed workload "+
			"will not be modified to send telemetry to Dash0.", targetNamespace)
		logger.Info(message)
		return admission.Allowed(message)
	}
	if !instrumentNewWorkloads {
		message := fmt.Sprintf("Instrumenting new workloads at deploy-time is not enabled in namespace %s, this newly"+
			"deployed workload will not be modified to send telemetry to Dash0.", targetNamespace)
		logger.Info(message)
		return admission.Allowed(message)
	}

	if !dash0CustomResource.IsAvailable() {
		return admission.Allowed(
			fmt.Sprintf(
				"The Dash0 custome resource in the namespace %s is not in status available, this workload will not be "+
					"modified to send telemetry to Dash0.", targetNamespace))
	}
	if dash0CustomResource.IsMarkedForDeletion() {
		return admission.Allowed(
			fmt.Sprintf(
				"The Dash0 custome resource in the namespace %s is about to be deleted, this workload will not be "+
					"modified to send telemetry to Dash0.", targetNamespace))
	}

	gkv := request.Kind
	group := gkv.Group
	version := gkv.Version
	kind := gkv.Kind
	gvkLabel := fmt.Sprintf("%s/%s.%s", group, version, kind)

	return routes.routeFor(group, kind, version)(h, request, gvkLabel, &logger)
}

func (h *Handler) handleCronJob(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	cronJob := &batchv1.CronJob{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, cronJob)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&cronJob.ObjectMeta) {
		return h.postProcess(request, cronJob, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&cronJob.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyCronJob(cronJob)
	return h.postProcess(request, cronJob, hasBeenModified, false, logger)
}

func (h *Handler) handleDaemonSet(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	daemonSet := &appsv1.DaemonSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, daemonSet)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&daemonSet.ObjectMeta) {
		return h.postProcess(request, daemonSet, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&daemonSet.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyDaemonSet(daemonSet)
	return h.postProcess(request, daemonSet, hasBeenModified, false, logger)
}

func (h *Handler) handleDeployment(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	deployment := &appsv1.Deployment{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, deployment)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&deployment.ObjectMeta) {
		return h.postProcess(request, deployment, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&deployment.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyDeployment(deployment)
	return h.postProcess(request, deployment, hasBeenModified, false, logger)
}

func (h *Handler) handleJob(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	job := &batchv1.Job{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, job)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&job.ObjectMeta) {
		return h.postProcess(request, job, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&job.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyJob(job)
	return h.postProcess(request, job, hasBeenModified, false, logger)
}

func (h *Handler) handlePod(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	pod := &corev1.Pod{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, pod)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&pod.ObjectMeta) {
		return h.postProcess(request, pod, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&pod.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyPod(pod)
	return h.postProcess(request, pod, hasBeenModified, false, logger)
}

func (h *Handler) handleReplicaSet(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	replicaSet := &appsv1.ReplicaSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, replicaSet)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&replicaSet.ObjectMeta) {
		return h.postProcess(request, replicaSet, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&replicaSet.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyReplicaSet(replicaSet)
	return h.postProcess(request, replicaSet, hasBeenModified, false, logger)
}

func (h *Handler) handleStatefulSet(
	request admission.Request,
	gvkLabel string,
	logger *logr.Logger,
) admission.Response {
	statefulSet := &appsv1.StatefulSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, statefulSet)
	if failed {
		return responseIfFailed
	}
	if util.CheckAndDeleteIgnoreOnceLabel(&statefulSet.ObjectMeta) {
		return h.postProcess(request, statefulSet, false, true, logger)
	}
	if util.HasOptedOutOfInstrumenationForWorkload(&statefulSet.ObjectMeta) {
		logger.Info(optOutAdmissionAllowedMessage)
		return admission.Allowed(optOutAdmissionAllowedMessage)
	}
	hasBeenModified := h.newWorkloadModifier(logger).ModifyStatefulSet(statefulSet)
	return h.postProcess(request, statefulSet, hasBeenModified, false, logger)
}

func (h *Handler) preProcess(
	request admission.Request,
	gvkLabel string,
	resource runtime.Object,
) (admission.Response, bool) {
	if _, _, err := decoder.Decode(request.Object.Raw, nil, resource); err != nil {
		err := fmt.Errorf("cannot parse resource into a %s: %w", gvkLabel, err)
		util.QueueFailedInstrumentationEvent(h.Recorder, resource, "webhook", err)
		return admission.Errored(http.StatusInternalServerError, err), true
	}
	return admission.Response{}, false
}

func (h *Handler) postProcess(
	request admission.Request,
	resource runtime.Object,
	hasBeenModified bool,
	ignored bool,
	logger *logr.Logger,
) admission.Response {
	if !ignored && !hasBeenModified {
		logger.Info("Dash0 instrumentation already present, no modification by webhook is necessary.")
		util.QueueNoInstrumentationNecessaryEvent(h.Recorder, resource, "webhook")
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(resource)
	if err != nil {
		util.QueueFailedInstrumentationEvent(h.Recorder, resource, "webhook", err)
		return admission.Allowed(fmt.Errorf("error when marshalling modfied resource to JSON: %w", err).Error())
	}

	if ignored {
		logger.Info("Ignoring this admission request due to the presence of dash0.com/webhook-ignore-once")
		// deliberately not queueing an event for this case
		return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
	}

	logger.Info("The webhook has added Dash0 instrumentation to the workload.")
	util.QueueSuccessfulInstrumentationEvent(h.Recorder, resource, "webhook")
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *Handler) newWorkloadModifier(logger *logr.Logger) *workloads.ResourceModifier {
	return workloads.NewResourceModifier(
		util.InstrumentationMetadata{
			Images:               h.Images,
			InstrumentedBy:       "webhook",
			OtelCollectorBaseUrl: h.OtelCollectorBaseUrl,
		},
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
