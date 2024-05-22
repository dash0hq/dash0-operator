// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dash0hq/dash0-operator/internal/k8sresources"
	. "github.com/dash0hq/dash0-operator/internal/util"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	log     = logf.Log.WithName("dash0-webhook")
	decoder = scheme.Codecs.UniversalDecoder()
)

type Handler struct {
	Recorder record.EventRecorder
	Versions k8sresources.Versions
}

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

	if group == "apps" && version == "v1" && kind == "Deployment" {
		deployment := &appsv1.Deployment{}
		if _, _, err := decoder.Decode(request.Object.Raw, nil, deployment); err != nil {
			err := fmt.Errorf("cannot parse resource into a %s/%s.%s: %w", group, version, kind, err)
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("error while parsing the resource: %w", err))
		}

		hasBeenModified := k8sresources.ModifyDeployment(deployment, request.Namespace, h.Versions, logger)
		if !hasBeenModified {
			return admission.Allowed("no changes")
		}

		marshalled, err := json.Marshal(deployment)
		if err != nil {
			return admission.Allowed(fmt.Errorf("error when marshalling modfied resource to JSON: %w", err).Error())
		}

		if hasBeenModified {
			QueueSuccessfulInstrumentationEvent(h.Recorder, deployment, "webhook")
		}
		return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
	} else {
		logger.Info("resource type not supported", "group", group, "version", version, "kind", kind)
		return admission.Allowed("unknown resource type")
	}
}
