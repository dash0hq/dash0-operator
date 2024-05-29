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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/dash0hq/dash0-operator/internal/k8sresources"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type Handler struct {
	Recorder record.EventRecorder
	Versions util.Versions
}

type resourceHandler func(h *Handler, request admission.Request, gvkLabel string, logger *logr.Logger) admission.Response
type routing map[string]map[string]map[string]resourceHandler

var (
	log     = logf.Log.WithName("dash0-webhook")
	decoder = scheme.Codecs.UniversalDecoder()

	routes = routing{
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

func (h *Handler) Handle(_ context.Context, request admission.Request) admission.Response {
	logger := log.WithValues("gvk", request.Kind, "namespace", request.Namespace, "name", request.Name)
	logger.Info("incoming admission request")

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
	hasBeenModified := h.newResourceModifier(logger).ModifyCronJob(cronJob)
	return h.postProcess(request, cronJob, hasBeenModified, logger)
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
	hasBeenModified := h.newResourceModifier(logger).ModifyDaemonSet(daemonSet)
	return h.postProcess(request, daemonSet, hasBeenModified, logger)
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
	hasBeenModified := h.newResourceModifier(logger).ModifyDeployment(deployment)
	return h.postProcess(request, deployment, hasBeenModified, logger)
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
	hasBeenModified := h.newResourceModifier(logger).ModifyJob(job)
	return h.postProcess(request, job, hasBeenModified, logger)
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
	hasBeenModified := h.newResourceModifier(logger).ModifyReplicaSet(replicaSet)
	return h.postProcess(request, replicaSet, hasBeenModified, logger)
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
	hasBeenModified := h.newResourceModifier(logger).ModifyStatefulSet(statefulSet)
	return h.postProcess(request, statefulSet, hasBeenModified, logger)
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
	logger *logr.Logger,
) admission.Response {
	if !hasBeenModified {
		logger.Info("Dash0 instrumentation already present, no modification by webhook is necessary.")
		util.QueueAlreadyInstrumentedEvent(h.Recorder, resource, "webhook")
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(resource)
	if err != nil {
		util.QueueFailedInstrumentationEvent(h.Recorder, resource, "webhook", err)
		return admission.Allowed(fmt.Errorf("error when marshalling modfied resource to JSON: %w", err).Error())
	}

	logger.Info("The webhook has added Dash0 instrumentation to the resource.")
	util.QueueSuccessfulInstrumentationEvent(h.Recorder, resource, "webhook")
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *Handler) newResourceModifier(logger *logr.Logger) *k8sresources.ResourceModifier {
	return k8sresources.NewResourceModifier(
		util.InstrumentationMetadata{
			Versions:       h.Versions,
			InstrumentedBy: "webhook",
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
