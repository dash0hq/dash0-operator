// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConflictOnFirstUpdateClient rejects the first Update with a conflict error and delegates everything else, to
// exercise the retry of the resource managers' createOrUpdateResource.
type ConflictOnFirstUpdateClient struct {
	client.Client
	UpdateCalls int
}

func (c *ConflictOnFirstUpdateClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	c.UpdateCalls++
	if c.UpdateCalls == 1 {
		return apierrors.NewConflict(
			schema.GroupResource{Resource: "configmaps"},
			obj.GetName(),
			errors.New("the object has been modified; please apply your changes to the latest version and try again"),
		)
	}
	return c.Client.Update(ctx, obj, opts...)
}
