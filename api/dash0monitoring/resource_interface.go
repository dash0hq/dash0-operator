// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0monitoring

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Dash0Resource interface {
	GetResourceTypeName() string
	GetNaturalLanguageResourceTypeName() string
	Get() client.Object
	GetName() string
	GetUid() types.UID
	GetCreationTimestamp() metav1.Time
	GetReceiver() client.Object
	GetListReceiver() client.ObjectList
	IsClusterResource() bool
	RequestToName(ctrl.Request) string

	IsAvailable() bool
	SetAvailableConditionToUnknown()
	EnsureResourceIsMarkedAsAvailable()
	EnsureResourceIsMarkedAsDegraded(string, string)
	EnsureResourceIsMarkedAsAboutToBeDeleted()
	IsMarkedForDeletion() bool

	Items(client.ObjectList) []client.Object
	At(client.ObjectList, int) Dash0Resource
}
