// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type ApiClient interface {
	SetApiEndpointAndDataset(*ApiConfig, *logr.Logger)
	RemoveApiEndpointAndDataset()
}

type ApiConfig struct {
	Endpoint string
	Dataset  string
}

type validationResult struct {
	namespace string
	name      string
	url       string
	origin    string
	authToken string
}

var (
	retrySettings = wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.5,
		Steps:    3,
	}
)

func makeFilterPredicate(group string, kind string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isMatchingCrd(group, kind, e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// We are not interested in updates, but we still need to define a filter predicate for it, otherwise _all_
			// update events for CRDs would be passed to our event handler. We always return false to ignore update
			// events entirely. Same for generic events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isMatchingCrd(group, kind, e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

func isMatchingCrd(group string, kind string, crd client.Object) bool {
	if crdCasted, ok := crd.(*apiextensionsv1.CustomResourceDefinition); ok {
		return crdCasted.Spec.Group == group &&
			crdCasted.Spec.Names.Kind == kind
	} else {
		return false
	}
}

func isValidApiConfig(apiConfig *ApiConfig) bool {
	return apiConfig != nil && apiConfig.Endpoint != ""
}

func readNamespaceAndName(
	resource *unstructured.Unstructured,
	resourceLabel string,
	logger *logr.Logger,
) (string, string, bool) {
	metadataRaw := resource.Object["metadata"]
	if metadataRaw == nil {
		logger.Info(
			fmt.Sprintf("%[1]s payload has no metadata section, the %[1]s will not be updated in Dash0.",
				resourceLabel,
			))
		return "", "", false
	}
	metadata, ok := metadataRaw.(map[string]interface{})
	if !ok {
		logger.Info(
			fmt.Sprintf("%[1]s payload metadata section is not a map, the %[1]s will not be updated in Dash0.",
				resourceLabel,
			))
		return "", "", false
	}
	namespace, ok := readStringAttribute(metadata, "namespace", resourceLabel, logger)
	if !ok {
		return "", "", false
	}
	name, ok := readStringAttribute(metadata, "name", resourceLabel, logger)
	if !ok {
		return "", "", false
	}
	return namespace, name, true
}

func readStringAttribute(
	metadata map[string]interface{},
	attributeName string,
	resourceLabel string,
	logger *logr.Logger,
) (string, bool) {
	valueRaw := metadata[attributeName]
	if valueRaw == nil {
		logger.Info(
			fmt.Sprintf("%[1]s has no attribute metadata.%[2]s, the %[1]s will not be updated in Dash0.",
				resourceLabel,
				attributeName))
		return "", false
	}
	value, ok := valueRaw.(string)
	if !ok {
		logger.Info(
			fmt.Sprintf("%[1]s metadata.%[2]s is not a string, the %[1]s will not be updated in Dash0.",
				resourceLabel,
				attributeName))
		return "", false
	}
	if value == "" {
		logger.Info(
			fmt.Sprintf("%[1]s has no attribute metadata.%[2]s, the %[1]s will not be updated in Dash0.",
				resourceLabel,
				attributeName))
		return "", false
	}
	return value, true
}
