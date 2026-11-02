// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util"
)

const (
	podOwnerResolutionTimeout = 2 * time.Second
	cachedOwnerReadTimeout    = 500 * time.Millisecond
)

var (
	resolvableOwnerTypes = map[metav1.TypeMeta][]metav1.TypeMeta{
		util.K8sTypeMetaPod: {
			util.K8sTypeMetaDaemonSet,
			util.K8sTypeMetaJob,
			util.K8sTypeMetaReplicaSet,
			util.K8sTypeMetaStatefulSet,
		},
		util.K8sTypeMetaJob:        {util.K8sTypeMetaCronJob},
		util.K8sTypeMetaReplicaSet: {util.K8sTypeMetaDeployment},
	}

	// PodOwnerMetadataTypes are the owner types that PodOwnerResolver reads. Informers for their metadata should be
	// registered with the cache of the operator manager at startup, so that the first pod admission request does not
	// have to wait for them to sync.
	PodOwnerMetadataTypes = []metav1.TypeMeta{
		util.K8sTypeMetaCronJob,
		util.K8sTypeMetaDaemonSet,
		util.K8sTypeMetaDeployment,
		util.K8sTypeMetaJob,
		util.K8sTypeMetaReplicaSet,
		util.K8sTypeMetaStatefulSet,
	}
)

// PodOwners holds the metadata of the controlling owners of a pod, ordered from the pod's direct owner to its top-level
// owner, for example the replica set and then the deployment.
type PodOwners []*metav1.PartialObjectMetadata

// PodOwnerResolver reads the controlling owners of a pod, so that their labels and annotations can be taken into
// account when instrumenting the pod.
type PodOwnerResolver struct {
	cachedReader client.Reader
	liveReader   client.Reader
}

// NewPodOwnerResolver creates a PodOwnerResolver. Owners are read from cachedReader first. If an owner cannot be read
// from there, for example because the cache has not yet seen a replica set that has just been created, or because the
// cache is still syncing right after the operator manager has started, it is read from liveReader.
func NewPodOwnerResolver(cachedReader client.Reader, liveReader client.Reader) *PodOwnerResolver {
	return &PodOwnerResolver{
		cachedReader: cachedReader,
		liveReader:   liveReader,
	}
}

// NewPartialObjectMetadata creates an empty metadata-only object for the given type.
func NewPartialObjectMetadata(typeMeta metav1.TypeMeta) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{TypeMeta: typeMeta}
}

/*
Resolve follows the controller owner references of the pod: pod → replica set → deployment, pod → job → cron job, and
pod → daemon set or stateful set. The walk stops at an owner of any other type, since the operator neither knows nor
has permissions to read arbitrary owner types, and at an owner that does not exist (anymore). Neither is an error, the
returned owners then only contain the levels that could be resolved.

An error is returned if an owner could not be read for any other reason, including the walk taking longer than
podOwnerResolutionTimeout. Resolve is called from the instrumentation webhook, which needs to answer well within its
timeout.
*/
func (r *PodOwnerResolver) Resolve(ctx context.Context, pod *corev1.Pod) (PodOwners, error) {
	ctx, cancel := context.WithTimeout(ctx, podOwnerResolutionTimeout)
	defer cancel()

	var owners PodOwners
	ownedTypeMeta := util.K8sTypeMetaPod
	ownerReference := metav1.GetControllerOfNoCopy(pod)
	for ownerReference != nil {
		ownerTypeMeta := metav1.TypeMeta{APIVersion: ownerReference.APIVersion, Kind: ownerReference.Kind}
		if !slices.Contains(resolvableOwnerTypes[ownedTypeMeta], ownerTypeMeta) {
			break
		}
		owner, err := r.readOwner(ctx, ownerTypeMeta, pod.Namespace, ownerReference)
		if err != nil {
			return nil, err
		}
		if owner == nil {
			break
		}
		owners = append(owners, owner)
		ownedTypeMeta = ownerTypeMeta
		ownerReference = metav1.GetControllerOfNoCopy(owner)
	}
	return owners, nil
}

// readOwner reads the metadata of the owner, and returns nil if there is no object of this type with the name and UID
// from the owner reference.
func (r *PodOwnerResolver) readOwner(
	ctx context.Context,
	typeMeta metav1.TypeMeta,
	namespace string,
	ownerReference *metav1.OwnerReference,
) (*metav1.PartialObjectMetadata, error) {
	key := client.ObjectKey{Namespace: namespace, Name: ownerReference.Name}
	owner := NewPartialObjectMetadata(typeMeta)
	cachedReadCtx, cancel := context.WithTimeout(ctx, cachedOwnerReadTimeout)
	err := r.cachedReader.Get(cachedReadCtx, key, owner)
	cancel()
	if err != nil || owner.UID != ownerReference.UID {
		owner = NewPartialObjectMetadata(typeMeta)
		err = r.liveReader.Get(ctx, key, owner)
	}
	if apierrors.IsNotFound(err) || (err == nil && owner.UID != ownerReference.UID) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the %s %s/%s owning the pod: %w", typeMeta.Kind, namespace, key.Name, err)
	}
	owner.TypeMeta = typeMeta
	return owner, nil
}

// TopLevel returns the outermost resolved owner, for example the deployment of a pod managed by a replica set, or nil
// if no owner has been resolved.
func (o PodOwners) TopLevel() *metav1.PartialObjectMetadata {
	if len(o) == 0 {
		return nil
	}
	return o[len(o)-1]
}

// WorkloadMeta returns the metadata of the top-level owner, which corresponds to the workload metadata when modifying
// the pod template of a higher-order workload, or the metadata of the pod itself if it has no resolved owner.
func (o PodOwners) WorkloadMeta(pod *corev1.Pod) *metav1.ObjectMeta {
	if topLevel := o.TopLevel(); topLevel != nil {
		return &topLevel.ObjectMeta
	}
	return &pod.ObjectMeta
}

// mergedLabels returns the labels of the pod and its owners. If the same label key is present on several levels, the
// value from the level closest to the pod wins.
func (o PodOwners) mergedLabels(pod *corev1.Pod) map[string]string {
	merged := make(map[string]string)
	for _, owner := range slices.Backward(o) {
		maps.Copy(merged, owner.Labels)
	}
	maps.Copy(merged, pod.Labels)
	return merged
}

// PodHasOptedOutOfInstrumentation evaluates the instrumentation label selector against the labels of the pod merged
// with the labels of its owners. This way, opting out (or in, for a selector like dash0.com/enable=true) works on the
// workload's own metadata, as without pod-level instrumentation, as well as on the pod template.
func PodHasOptedOutOfInstrumentation(pod *corev1.Pod, owners PodOwners, instrumentationLabelSelector string) bool {
	return util.HasOptedOutOfInstrumentation(
		&metav1.ObjectMeta{Labels: owners.mergedLabels(pod)},
		instrumentationLabelSelector,
	)
}
