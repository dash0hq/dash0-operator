// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateFunc compares the existing object with the desired object and updates it if necessary. It reports whether the
// object has been changed, and it may modify the desired object it is given.
type UpdateFunc func(existingResource client.Object, desiredResource client.Object) (bool, error)

// UpdateWithRetryOnConflict calls update with a fresh read of the existing object and an untouched copy of
// desiredResource, and retries when the update is rejected with a conflict. Conflicts are expected, since the read is
// served by the controller-runtime cache, which lags behind the API server.
func UpdateWithRetryOnConflict(
	ctx context.Context,
	k8sClient client.Client,
	desiredResource client.Object,
	update UpdateFunc,
) (bool, error) {
	pristineDesiredResource := desiredResource.DeepCopyObject().(client.Object)
	hasChanged := false
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		existingResource, receiverErr := CreateEmptyReceiverFor(pristineDesiredResource)
		if receiverErr != nil {
			return receiverErr
		}
		if getErr := k8sClient.Get(
			ctx,
			client.ObjectKeyFromObject(pristineDesiredResource),
			existingResource,
		); getErr != nil {
			return getErr
		}
		var updateErr error
		hasChanged, updateErr = update(existingResource, pristineDesiredResource.DeepCopyObject().(client.Object))
		return updateErr
	})
	return hasChanged, err
}
