// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package otelcolresources

import (
	"context"
	"errors"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type OTelColResourceManager struct {
	client.Client
}

func (m *OTelColResourceManager) AssembleDesiredState(namespace string) []client.Object {
	return []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				// TODO name is configurable to some degree?
				Name:      "dash0-opentelemetry-collector-daemonset",
				Namespace: namespace,
				Labels: map[string]string{
					// TODO more labels
					"dash0.com/enable": "false",
				},
				// Annotations: TODO do we need annotations?
			},
			Data: map[string]string{
				"collector.yaml": "{}",
			},
		},
	}
}

func (m *OTelColResourceManager) CreateOrUpdateOpenTelemetryCollectorResources(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) (bool, bool, error) {
	desiredState := m.AssembleDesiredState(namespace)
	resourcesHaveBeenCreated := false
	resourcesHaveBeenUpdated := false
	for _, desiredResource := range desiredState {
		isNew, isChanged, err := m.createOrUpdateResource(
			ctx,
			desiredResource,
			logger,
		)
		if err != nil {
			return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, err
		} else if isNew {
			resourcesHaveBeenCreated = true
		} else if isChanged {
			resourcesHaveBeenUpdated = true
		}
	}

	return resourcesHaveBeenCreated, resourcesHaveBeenUpdated, nil
}

func (m *OTelColResourceManager) createOrUpdateResource(
	ctx context.Context,
	desiredObject client.Object,
	logger *logr.Logger,
) (bool, bool, error) {
	existingObject, err := m.createEmptyReceiverFor(desiredObject)
	if err != nil {
		return false, false, err
	}
	err = m.Client.Get(ctx, client.ObjectKeyFromObject(desiredObject), existingObject)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, false, err
		}
		err = m.createResource(ctx, desiredObject, logger)
		if err != nil {
			return false, false, err
		}
		return true, false, nil
	} else {
		// object needs to be updated
		var hasChanged bool
		hasChanged, err = m.updateResource(ctx, existingObject, desiredObject, logger)
		if err != nil {
			return false, false, err
		}
		return false, hasChanged, err
	}
}

func (m *OTelColResourceManager) createEmptyReceiverFor(desiredObject client.Object) (client.Object, error) {
	objectKind := desiredObject.GetObjectKind()
	gvk := schema.GroupVersionKind{
		Group:   objectKind.GroupVersionKind().Group,
		Version: objectKind.GroupVersionKind().Version,
		Kind:    objectKind.GroupVersionKind().Kind,
	}
	runtimeObject, err := scheme.Scheme.New(gvk)
	if err != nil {
		return nil, err
	}
	return runtimeObject.(client.Object), nil
}

func (m *OTelColResourceManager) createResource(
	ctx context.Context,
	desiredObject client.Object,
	logger *logr.Logger,
) error {
	logger.Info(
		"creating resource",
		"name",
		desiredObject.GetName(),
		"namespace",
		desiredObject.GetNamespace(),
		"kind",
		desiredObject.GetObjectKind().GroupVersionKind(),
	)
	err := m.Client.Create(ctx, desiredObject)
	if err != nil {
		return err
	}
	return nil
}

func (m *OTelColResourceManager) updateResource(
	ctx context.Context,
	existingObject client.Object,
	desiredObject client.Object,
	logger *logr.Logger,
) (bool, error) {
	logger.Info(
		"updating resource",
		"name",
		desiredObject.GetName(),
		"namespace",
		desiredObject.GetNamespace(),
		"kind",
		desiredObject.GetObjectKind().GroupVersionKind(),
	)
	err := m.Client.Update(ctx, desiredObject)
	if err != nil {
		return false, err
	}
	updatedObject, err := m.createEmptyReceiverFor(desiredObject)
	if err != nil {
		return false, err
	}
	err = m.Client.Get(ctx, client.ObjectKeyFromObject(desiredObject), updatedObject)
	if err != nil {
		return false, err
	}
	hasChanged := !reflect.DeepEqual(existingObject, updatedObject)
	return hasChanged, nil
}

func (m *OTelColResourceManager) DeleteResources(
	ctx context.Context,
	namespace string,
	logger *logr.Logger,
) error {
	var allErrors []error
	allObjects := m.AssembleDesiredState(namespace)
	for _, object := range allObjects {
		logger.Info(
			"deleting resource",
			"name",
			object.GetName(),
			"namespace",
			object.GetNamespace(),
			"kind",
			object.GetObjectKind().GroupVersionKind(),
		)
		err := m.Client.Delete(ctx, object)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}
	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}
	return nil
}
