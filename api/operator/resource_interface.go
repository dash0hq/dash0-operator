// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Dash0Resource interface {
	GetNaturalLanguageResourceTypeName() string
	Get() client.Object
	GetName() string
	GetUID() types.UID
	GetCreationTimestamp() metav1.Time
	GetReceiver() client.Object
	GetListReceiver() client.ObjectList
	IsClusterResource() bool
	RequestToName(ctrl.Request) string

	IsAvailable() bool
	IsDegraded() bool
	SetAvailableConditionToUnknown()
	EnsureResourceIsMarkedAsAvailable()
	EnsureResourceIsMarkedAsDegraded(string, string)
	EnsureResourceIsMarkedAsAboutToBeDeleted()
	IsMarkedForDeletion() bool

	// Items returns all items from the given ObjectList as an array of client.Object objects.
	Items(client.ObjectList) []client.Object
	// All returns all items from the given ObjectList as an array of Dash0Resource objects.
	All(client.ObjectList) []Dash0Resource
	// At returns the item at the given index as a Dash0Resource object.
	At(client.ObjectList, int) Dash0Resource
}
