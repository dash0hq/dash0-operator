// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"errors"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConflictOnFirstUpdateClient simulates a concurrent modification on the first Update: it modifies the stored object
// and rejects the update with a conflict error. Everything else is delegated, to exercise the retry of the resource
// managers' createOrUpdateResource.
type ConflictOnFirstUpdateClient struct {
	client.Client
	// ModifyStoredObject is the concurrent modification of the stored object. By default, an annotation is added.
	ModifyStoredObject func(storedObject client.Object)
	UpdateCalls        int
}

func (c *ConflictOnFirstUpdateClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	c.UpdateCalls++
	if c.UpdateCalls > 1 {
		return c.Client.Update(ctx, obj, opts...)
	}

	storedObject := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(obj), storedObject); err != nil {
		return err
	}
	if c.ModifyStoredObject != nil {
		c.ModifyStoredObject(storedObject)
	} else {
		annotations := storedObject.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["test.dash0.com/concurrent-modification"] = "true"
		storedObject.SetAnnotations(annotations)
	}
	if err := c.Client.Update(ctx, storedObject); err != nil {
		return err
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	return apierrors.NewConflict(
		schema.GroupResource{Group: gvk.Group, Resource: strings.ToLower(gvk.Kind)},
		obj.GetName(),
		errors.New("the object has been modified; please apply your changes to the latest version and try again"),
	)
}
