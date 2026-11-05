// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
	"github.com/dash0hq/dash0-operator/internal/workloads"
)

type InstrumentationWebhookHandler struct {
	Client                       client.Client
	APIReader                    client.Reader
	Recorder                     events.EventRecorder
	ClusterInstrumentationConfig *util.ClusterInstrumentationConfig
	PodOwnerResolver             *workloads.PodOwnerResolver
}

type resourceHandler func(
	h *InstrumentationWebhookHandler,
	ctx context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response
type routing map[string]map[string]map[string]resourceHandler

const (
	optOutAdmissionAllowedMessage    = "not instrumenting this workload as it does not match the instrumentation label selector \"%s\""
	sameVersionNoModificationMessage = "not updating the existing instrumentation for this workload, it has already " +
		"been successfully instrumented by the same operator version"
	actor = util.ActorWebhook
)

var (
	instrumentationWebhookLog = logf.Log.WithName("instrumentation-webhook")
	decoder                   = scheme.Codecs.UniversalDecoder()

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
		_ context.Context,
		request admission.Request,
		gvkLabel string,
		namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
		logger logd.Logger,
	) admission.Response {
		return logAndReturnAllowed(fmt.Sprintf("resource type not supported: %s", gvkLabel), logger)
	}
)

// NewInstrumentationWebhookHandler creates the handler for the instrumentation webhook. The client is expected to be
// the cached client of the operator manager, apiReader reads directly from the API server. It is used to read the cron
// job controlling a job, so that admitting a job does not start an informer for all cron jobs in the cluster, and to
// read the owners of a pod that are not in the cache yet. informers is used to start watching the owners of pods once
// the first namespace uses instrumentWorkloads.mode=pods (see workloads.NewPodOwnerResolver).
func NewInstrumentationWebhookHandler(
	client client.Client,
	apiReader client.Reader,
	informers cache.Informers,
	recorder events.EventRecorder,
	clusterInstrumentationConfig *util.ClusterInstrumentationConfig,
) *InstrumentationWebhookHandler {
	if clusterInstrumentationConfig == nil {
		panic("clusterInstrumentationConfig must not be nil")
	}
	return &InstrumentationWebhookHandler{
		Client:                       client,
		APIReader:                    apiReader,
		Recorder:                     recorder,
		ClusterInstrumentationConfig: clusterInstrumentationConfig,
		PodOwnerResolver:             workloads.NewPodOwnerResolver(client, apiReader, informers),
	}
}

func (h *InstrumentationWebhookHandler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	webhook := &admission.Webhook{
		Handler: h,
	}

	handler, err := admission.StandaloneWebhook(webhook, admission.StandaloneOptions{})
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/workloads/inject", handler)

	return nil
}

func (h *InstrumentationWebhookHandler) UpdateExtraConfig(_ context.Context, extraConfig util.ExtraConfig, _ logd.Logger) {
	h.ClusterInstrumentationConfig.ExtraConfig.Store(&extraConfig)
}

// newRequestLogger derives a logger with the attributes of one admission request.
func newRequestLogger(request admission.Request) logd.Logger {
	return logd.NewLogger(
		instrumentationWebhookLog.WithValues(
			"operation",
			request.Operation,
			"gvk",
			request.Kind,
			"namespace",
			request.Namespace,
			"name",
			request.Name,
		),
	)
}

func (h *InstrumentationWebhookHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	if response, ignoredOnce := consumeIgnoreOnceLabel(request); ignoredOnce {
		return response
	}

	targetNamespace := request.Namespace

	dash0List := &dash0v1beta1.Dash0MonitoringList{}
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
				newRequestLogger(request),
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
	logger := newRequestLogger(request)
	logger.Debug(
		"found Dash0 monitoring resource in namespace",
		"namespace",
		targetNamespace,
		"available",
		dash0MonitoringResource.IsAvailable(),
		"markedForDeletion",
		dash0MonitoringResource.IsMarkedForDeletion(),
	)

	if !dash0MonitoringResource.IsAvailable() {
		return admission.Allowed(fmt.Sprintf(
			"The Dash0 monitoring resource in the namespace %s is not in status available, this workload will "+
				"not be modified to send telemetry to Dash0.", targetNamespace))
	}
	if dash0MonitoringResource.IsMarkedForDeletion() {
		return logAndReturnAllowed(
			fmt.Sprintf(
				"The Dash0 monitoring resource in the namespace %s is about to be deleted, this workload will not be "+
					"modified to send telemetry to Dash0.", targetNamespace), logger)
	}

	actionPartial := "newly deployed"
	if request.Operation == admissionv1.Update {
		actionPartial = "updated"
	}
	instrumentWorkloadsMode := dash0MonitoringResource.ReadInstrumentWorkloadsMode()
	if instrumentWorkloadsMode == dash0common.InstrumentWorkloadsModeNone {
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

	if instrumentWorkloadsMode == dash0common.InstrumentWorkloadsModePods && kind != util.K8sTypeMetaPod.Kind {
		return h.handleWorkloadWithPodLevelInstrumentation(
			request,
			targetNamespace,
			gvkLabel,
			actionPartial,
			dash0MonitoringResource.GetNamespaceInstrumentationConfig(),
			logger,
		)
	}

	logger.Debug("routing admission request to handler", "group", group, "kind", kind, "version", version)
	return routes.routeFor(group, kind, version)(
		h,
		ctx,
		request,
		gvkLabel,
		dash0MonitoringResource.GetNamespaceInstrumentationConfig(),
		logger,
	)
}

// consumeIgnoreOnceLabel skips the admission request and removes the dash0.com/webhook-ignore-once label if the workload
// has it. This runs before all other checks, so the label is consumed by the update it was added for, regardless of the
// state of the monitoring resource.
func consumeIgnoreOnceLabel(request admission.Request) (admission.Response, bool) {
	workload := &unstructured.Unstructured{}
	if err := workload.UnmarshalJSON(request.Object.Raw); err != nil {
		// The request is then processed as if the label was absent.
		return admission.Response{}, false
	}
	labels := workload.GetLabels()
	if !util.RemoveWebhookIgnoreOnceLabel(labels) {
		return admission.Response{}, false
	}
	workload.SetLabels(labels)

	logger := newRequestLogger(request)
	marshalled, err := workload.MarshalJSON()
	if err != nil {
		return logErrorAndReturnAllowed(
			fmt.Errorf("error when marshalling the workload after removing the ignore-once label: %w", err),
			logger,
		), true
	}
	// deliberately not queueing an event for this case
	logger.Info(workloads.NoModificationReasonIgnoreOnceLabel(actor))
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled), true
}

func (h *InstrumentationWebhookHandler) handleCronJob(
	_ context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	cronJob := &batchv1.CronJob{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, cronJob, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(
		&cronJob.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if util.WasInstrumentedButHasOptedOutNow(
		&cronJob.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).RevertCronJob(cronJob)
		return h.postProcessUninstrumentation(request, cronJob, cronJob, modificationResult, logger)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyCronJob(cronJob)
		return h.postProcessInstrumentation(request, cronJob, cronJob, modificationResult, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleDaemonSet(
	_ context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	daemonSet := &appsv1.DaemonSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, daemonSet, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(
		&daemonSet.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if util.WasInstrumentedButHasOptedOutNow(
		&daemonSet.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).RevertDaemonSet(daemonSet)
		return h.postProcessUninstrumentation(request, daemonSet, daemonSet, modificationResult, logger)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyDaemonSet(daemonSet)
		return h.postProcessInstrumentation(request, daemonSet, daemonSet, modificationResult, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleDeployment(
	_ context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	deployment := &appsv1.Deployment{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, deployment, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(
		&deployment.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if util.WasInstrumentedButHasOptedOutNow(
		&deployment.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).RevertDeployment(deployment)
		return h.postProcessUninstrumentation(request, deployment, deployment, modificationResult, logger)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyDeployment(deployment)
		return h.postProcessInstrumentation(request, deployment, deployment, modificationResult, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleJob(
	ctx context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	job := &batchv1.Job{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, job, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	controllingCronJob, err := workloads.ReadControllingCronJob(ctx, h.APIReader, request.Namespace, job)
	if err != nil {
		wrappedErr := fmt.Errorf(
			"not instrumenting the job, since the cron job controlling it could not be read to check whether it has "+
				"opted out of instrumentation: %w",
			err,
		)
		util.QueueFailedInstrumentationEvent(h.Recorder, job, actor, wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger)
	}
	hasOptedOut := workloads.JobHasOptedOutOfInstrumentation(
		job,
		controllingCronJob,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	)
	if hasOptedOut && !util.HasBeenInstrumentedSuccessfully(&job.ObjectMeta) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if hasOptedOut {
		// This should not happen, since it can only happen for an admission request with operation=UPDATE, and we are
		// not listening to updates for jobs. We cannot uninstrument jobs if the user adds an opt-out label after the
		// job has been already instrumented, since jobs are immutable.
		return h.postProcessUninstrumentation(
			request,
			job,
			job,
			workloads.NewNotModifiedImmutableWorkloadCannotBeRevertedResult(),
			logger,
		)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyJob(job)
		return h.postProcessInstrumentation(request, job, job, modificationResult, logger)
	}
}

func (h *InstrumentationWebhookHandler) handlePod(
	ctx context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	pod := &corev1.Pod{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, pod, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	var owners workloads.PodOwners
	if namespaceInstrumentationConfig.InstrumentPods {
		var err error
		if owners, err = h.PodOwnerResolver.Resolve(ctx, pod, logger); err != nil {
			wrappedErr := fmt.Errorf(
				"not instrumenting the pod, since its owners could not be read to check whether they have opted out "+
					"of instrumentation: %w",
				err,
			)
			util.QueueFailedInstrumentationEvent(h.Recorder, podEventTarget(pod, nil), actor, wrappedErr)
			return logErrorAndReturnAllowed(wrappedErr, logger)
		}
	}
	eventTarget := podEventTarget(pod, owners)
	hasOptedOut := workloads.PodHasOptedOutOfInstrumentation(
		pod,
		owners,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	)
	if hasOptedOut && !util.HasBeenInstrumentedSuccessfully(&pod.ObjectMeta) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if hasOptedOut && namespaceInstrumentationConfig.InstrumentPods && request.Operation == admissionv1.Create {
		// The pod has inherited the instrumentation from the pod template of a workload that has been instrumented
		// before the namespace was switched to instrumentWorkloads.mode=pods.
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).RevertPod(pod)
		return h.postProcessUninstrumentation(request, pod, eventTarget, modificationResult, logger)
	} else if hasOptedOut {
		// This should not happen, since it can only happen for an admission request with operation=UPDATE, and we are
		// not listening to updates for pods. We cannot uninstrument ownerless pods if the user adds an opt-out label
		// after the pod has been already instrumented, since we cannot restart ownerless pods, which makes them
		// effectively immutable.
		return h.postProcessUninstrumentation(
			request,
			pod,
			eventTarget,
			workloads.NewNotModifiedImmutableWorkloadCannotBeRevertedResult(),
			logger,
		)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyPod(pod, owners)
		return h.postProcessInstrumentation(request, pod, eventTarget, modificationResult, logger)
	}
}

// podLevelWorkloadType describes how a type of workload is handled in namespaces with instrumentWorkloads.mode=pods.
type podLevelWorkloadType struct {
	newObject   func() client.Object
	podTemplate func(client.Object) *corev1.PodTemplateSpec
	// replacesPodsOnTemplateChange is false for workloads whose pods are not replaced when the pod template changes.
	replacesPodsOnTemplateChange func(client.Object) bool
	revert                       func(*workloads.ResourceModifier, client.Object) workloads.ModificationResult
}

var podLevelWorkloadTypes = map[string]podLevelWorkloadType{
	util.K8sTypeMetaCronJob.Kind: {
		newObject: func() client.Object { return &batchv1.CronJob{} },
		podTemplate: func(o client.Object) *corev1.PodTemplateSpec {
			return &o.(*batchv1.CronJob).Spec.JobTemplate.Spec.Template
		},
		replacesPodsOnTemplateChange: func(client.Object) bool { return false },
		revert: func(m *workloads.ResourceModifier, o client.Object) workloads.ModificationResult {
			return m.RevertCronJob(o.(*batchv1.CronJob))
		},
	},
	util.K8sTypeMetaDaemonSet.Kind: {
		newObject:   func() client.Object { return &appsv1.DaemonSet{} },
		podTemplate: func(o client.Object) *corev1.PodTemplateSpec { return &o.(*appsv1.DaemonSet).Spec.Template },
		replacesPodsOnTemplateChange: func(o client.Object) bool {
			return o.(*appsv1.DaemonSet).Spec.UpdateStrategy.Type != appsv1.OnDeleteDaemonSetStrategyType
		},
		revert: func(m *workloads.ResourceModifier, o client.Object) workloads.ModificationResult {
			return m.RevertDaemonSet(o.(*appsv1.DaemonSet))
		},
	},
	util.K8sTypeMetaDeployment.Kind: {
		newObject:                    func() client.Object { return &appsv1.Deployment{} },
		podTemplate:                  func(o client.Object) *corev1.PodTemplateSpec { return &o.(*appsv1.Deployment).Spec.Template },
		replacesPodsOnTemplateChange: func(client.Object) bool { return true },
		revert: func(m *workloads.ResourceModifier, o client.Object) workloads.ModificationResult {
			return m.RevertDeployment(o.(*appsv1.Deployment))
		},
	},
	util.K8sTypeMetaReplicaSet.Kind: {
		newObject:                    func() client.Object { return &appsv1.ReplicaSet{} },
		podTemplate:                  func(o client.Object) *corev1.PodTemplateSpec { return &o.(*appsv1.ReplicaSet).Spec.Template },
		replacesPodsOnTemplateChange: func(client.Object) bool { return false },
		revert: func(m *workloads.ResourceModifier, o client.Object) workloads.ModificationResult {
			return m.RevertReplicaSet(o.(*appsv1.ReplicaSet))
		},
	},
	util.K8sTypeMetaStatefulSet.Kind: {
		newObject:   func() client.Object { return &appsv1.StatefulSet{} },
		podTemplate: func(o client.Object) *corev1.PodTemplateSpec { return &o.(*appsv1.StatefulSet).Spec.Template },
		replacesPodsOnTemplateChange: func(o client.Object) bool {
			return o.(*appsv1.StatefulSet).Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType
		},
		revert: func(m *workloads.ResourceModifier, o client.Object) workloads.ModificationResult {
			return m.RevertStatefulSet(o.(*appsv1.StatefulSet))
		},
	},
}

/*
handleWorkloadWithPodLevelInstrumentation handles admission requests for higher-order workloads in namespaces with
instrumentWorkloads.mode=pods. Their pods are instrumented when they are created, so the workloads are not
instrumented.

A workload that has been instrumented before the namespace was switched to instrumentWorkloads.mode=pods still has the
instrumentation in its pod template. Neither kubectl apply (client-side or server-side) nor helm upgrade remove fields
they have not set themselves, so it would stay there indefinitely. It is removed as soon as that does not restart any
pods that would not be restarted anyway: when the workload is created, when the request changes the pod template
anyway, and for workloads that do not replace their pods when the pod template changes, like cron jobs and replica sets.
Jobs are immutable, and replica sets controlled by a deployment are left to the deployment.
*/
func (h *InstrumentationWebhookHandler) handleWorkloadWithPodLevelInstrumentation(
	request admission.Request,
	targetNamespace string,
	gvkLabel string,
	actionPartial string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	unmodified := admission.Allowed(
		fmt.Sprintf(
			"The instrumentation mode in namespace %s is %s, the %s %s will not be modified, its pods will be "+
				"instrumented when they are created.",
			targetNamespace,
			dash0common.InstrumentWorkloadsModePods,
			actionPartial,
			gvkLabel,
		),
	)
	workloadType, isSupported := podLevelWorkloadTypes[request.Kind.Kind]
	if !isSupported {
		return unmodified
	}
	workload := workloadType.newObject()
	if _, _, err := decoder.Decode(request.Object.Raw, nil, workload); err != nil {
		return logErrorAndReturnAllowed(fmt.Errorf("cannot parse resource into a %s: %w", gvkLabel, err), logger)
	}
	if _, hasInstrumentationLabel := workload.GetLabels()[util.InstrumentedLabelKey]; !hasInstrumentationLabel {
		return unmodified
	}
	if request.Kind.Kind == util.K8sTypeMetaReplicaSet.Kind &&
		slices.ContainsFunc(workload.GetOwnerReferences(), func(ownerReference metav1.OwnerReference) bool {
			return ownerReference.APIVersion == util.K8sTypeMetaDeployment.APIVersion &&
				ownerReference.Kind == util.K8sTypeMetaDeployment.Kind
		}) {
		return unmodified
	}
	if request.Operation == admissionv1.Update && workloadType.replacesPodsOnTemplateChange(workload) {
		oldWorkload := workloadType.newObject()
		if _, _, err := decoder.Decode(request.OldObject.Raw, nil, oldWorkload); err != nil {
			return logErrorAndReturnAllowed(fmt.Errorf("cannot parse the previous %s: %w", gvkLabel, err), logger)
		}
		if equality.Semantic.DeepEqual(workloadType.podTemplate(oldWorkload), workloadType.podTemplate(workload)) {
			return admission.Allowed(
				fmt.Sprintf(
					"The instrumentation mode in namespace %s is %s, the %s %s will not be modified. Its pod "+
						"template still has the instrumentation from before the namespace was switched to %s, which "+
						"will be removed the next time the pod template is changed.",
					targetNamespace,
					dash0common.InstrumentWorkloadsModePods,
					actionPartial,
					gvkLabel,
					dash0common.InstrumentWorkloadsModePods,
				),
			)
		}
	}
	modificationResult := workloadType.revert(h.newWorkloadModifier(namespaceInstrumentationConfig, logger), workload)
	return h.postProcessUninstrumentation(request, workload, workload, modificationResult, logger)
}

/*
podEventTarget returns the object that events about instrumenting the pod are recorded for. A pod created via
generateName, that is, every pod created by a higher-order workload, has neither a name nor a UID yet when the webhook
processes it. Events recorded for the pod itself would not be attached to any object, hence events for an owned pod are
recorded for its top-level owner instead, or for its direct owner if no owner could be resolved.
*/
func podEventTarget(pod *corev1.Pod, owners workloads.PodOwners) runtime.Object {
	if topLevelOwner := owners.TopLevel(); topLevelOwner != nil {
		return topLevelOwner
	}
	if ownerReference := metav1.GetControllerOfNoCopy(pod); ownerReference != nil {
		owner := workloads.NewPartialObjectMetadata(
			metav1.TypeMeta{APIVersion: ownerReference.APIVersion, Kind: ownerReference.Kind},
		)
		owner.Namespace = pod.Namespace
		owner.Name = ownerReference.Name
		owner.UID = ownerReference.UID
		return owner
	}
	return pod
}

func (h *InstrumentationWebhookHandler) handleReplicaSet(
	_ context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	replicaSet := &appsv1.ReplicaSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, replicaSet, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(
		&replicaSet.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if util.WasInstrumentedButHasOptedOutNow(
		&replicaSet.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).RevertReplicaSet(replicaSet)
		return h.postProcessUninstrumentation(request, replicaSet, replicaSet, modificationResult, logger)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyReplicaSet(replicaSet)
		return h.postProcessInstrumentation(request, replicaSet, replicaSet, modificationResult, logger)
	}
}

func (h *InstrumentationWebhookHandler) handleStatefulSet(
	_ context.Context,
	request admission.Request,
	gvkLabel string,
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) admission.Response {
	statefulSet := &appsv1.StatefulSet{}
	responseIfFailed, failed := h.preProcess(request, gvkLabel, statefulSet, logger)
	if failed {
		// if h.preProcess returns failed=true, it will already have logged the error
		return responseIfFailed
	}
	if util.HasOptedOutOfInstrumentationAndIsUninstrumented(
		&statefulSet.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		return logAndReturnAllowed(
			fmt.Sprintf(
				optOutAdmissionAllowedMessage, namespaceInstrumentationConfig.InstrumentationLabelSelector),
			logger,
		)
	} else if util.WasInstrumentedButHasOptedOutNow(
		&statefulSet.ObjectMeta,
		namespaceInstrumentationConfig.InstrumentationLabelSelector,
	) {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).RevertStatefulSet(statefulSet)
		return h.postProcessUninstrumentation(request, statefulSet, statefulSet, modificationResult, logger)
	} else {
		modificationResult := h.newWorkloadModifier(namespaceInstrumentationConfig, logger).ModifyStatefulSet(statefulSet)
		return h.postProcessInstrumentation(request, statefulSet, statefulSet, modificationResult, logger)
	}
}

func (h *InstrumentationWebhookHandler) preProcess(
	request admission.Request,
	gvkLabel string,
	resource runtime.Object,
	logger logd.Logger,
) (admission.Response, bool) {
	if _, _, err := decoder.Decode(request.Object.Raw, nil, resource); err != nil {
		wrappedErr := fmt.Errorf("cannot parse resource into a %s: %w", gvkLabel, err)
		util.QueueFailedInstrumentationEvent(h.Recorder, resource, actor, wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger), true
	}
	return admission.Response{}, false
}

// postProcessInstrumentation creates the patch for the modified resource, and records events for eventTarget, which is
// the resource itself except for pods.
func (h *InstrumentationWebhookHandler) postProcessInstrumentation(
	request admission.Request,
	resource runtime.Object,
	eventTarget runtime.Object,
	modificationResult workloads.ModificationResult,
	logger logd.Logger,
) admission.Response {
	if !modificationResult.HasBeenModified {
		msg := modificationResult.RenderReasonMessage(actor)
		if !modificationResult.LogAtDebugOnly {
			logger.Info(msg)
		}
		if _, isPod := resource.(*corev1.Pod); !isPod {
			util.QueueNoInstrumentationNecessaryEvent(h.Recorder, eventTarget, msg)
		}
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(resource)
	if err != nil {
		wrappedErr := fmt.Errorf("error when marshalling modfied resource to JSON: %w", err)
		util.QueueFailedInstrumentationEvent(h.Recorder, eventTarget, actor, wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger)
	}

	logger.Debug("patching workload to instrument it")
	util.HandlePotentiallySuccessfulInstrumentationEvent(
		h.Recorder,
		eventTarget,
		actor,
		modificationResult.ContainersTotal,
		modificationResult.InstrumentationIssuesPerContainer,
		logger,
	)
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

// postProcessUninstrumentation creates the patch for the reverted resource, and records events for eventTarget, which
// is the resource itself except for pods.
func (h *InstrumentationWebhookHandler) postProcessUninstrumentation(
	request admission.Request,
	resource runtime.Object,
	eventTarget runtime.Object,
	modificationResult workloads.ModificationResult,
	logger logd.Logger,
) admission.Response {
	if modificationResult.ImmutableWorkload {
		err := errors.New(modificationResult.RenderReasonMessage(actor))
		util.QueueFailedUninstrumentationEvent(h.Recorder, eventTarget, actor, err)
		logger.Info(err.Error())
		return admission.Allowed(err.Error())
	}

	if !modificationResult.HasBeenModified {
		if !modificationResult.LogAtDebugOnly {
			logger.Info(modificationResult.RenderReasonMessage(actor))
		} else {
			logger.Debug(modificationResult.RenderReasonMessage(actor))
		}
		util.QueueNoUninstrumentationNecessaryEvent(h.Recorder, eventTarget, actor)
		return admission.Allowed("no changes")
	}

	marshalled, err := json.Marshal(resource)
	if err != nil {
		wrappedErr := fmt.Errorf("error when marshalling modfied resource to JSON: %w", err)
		util.QueueFailedUninstrumentationEvent(h.Recorder, eventTarget, actor, wrappedErr)
		return logErrorAndReturnAllowed(wrappedErr, logger)
	}

	logger.Info("The webhook has removed the Dash0 instrumentation from the workload.")
	util.QueueSuccessfulUninstrumentationEvent(h.Recorder, eventTarget, actor)
	return admission.PatchResponseFromRaw(request.Object.Raw, marshalled)
}

func (h *InstrumentationWebhookHandler) newWorkloadModifier(
	namespaceInstrumentationConfig dash0v1beta1.NamespaceInstrumentationConfig,
	logger logd.Logger,
) *workloads.ResourceModifier {
	return workloads.NewResourceModifier(
		h.ClusterInstrumentationConfig,
		namespaceInstrumentationConfig,
		actor,
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

func logAndReturnAllowed(message string, logger logd.Logger) admission.Response {
	logger.Info(message)
	return admission.Allowed(message)
}

func logErrorAndReturnAllowed(err error, logger logd.Logger) admission.Response {
	logger.Error(err, "an error occurred while processing the admission request")

	// Note: We never return admission.Errored or admission.Denied, even in case an error happens, because we do not
	// want to block the deployment of workloads. If instrumenting a new workload fails it is not great, but having
	// the webhook actually getting in the way of deploying a workload would be much worse.
	return admission.Allowed(err.Error())
}
