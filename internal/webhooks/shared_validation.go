// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util/pointers"
)

type valid = bool

// ValidateGrpcExportInsecureFlags validates that the GRPC export config doesn't have both `insecure`
// and `insecureSkipVerify` enabled at the same time.
func validateGrpcExportInsecureFlags(export *common.Export) valid {
	if export == nil || export.Grpc == nil {
		return true
	} else {
		return !pointers.ReadBoolPointerWithDefault(export.Grpc.Insecure, false) ||
			!pointers.ReadBoolPointerWithDefault(export.Grpc.InsecureSkipVerify, false)
	}
}

// validateExportSecretRefsExist checks that all Kubernetes secrets (and the keys within them) referenced by the given
// export -- the Dash0 authorization secretRef as well as gRPC/HTTP header values sourced from secrets -- exist in the
// namespace of the Dash0 operator. A dangling secret reference would be rolled out as a required secretKeyRef
// environment variable on the collector pods, which the kubelet answers with CreateContainerConfigError, so admission
// requests with dangling secret references are rejected. Returns nil if all referenced secrets exist.
func validateExportSecretRefsExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	export *common.Export,
) error {
	if export == nil {
		return nil
	}
	if export.Dash0 != nil {
		authorization := export.Dash0.Authorization
		// A token literal takes precedence over the secret ref, a dangling secret ref is irrelevant then.
		if (authorization.Token == nil || *authorization.Token == "") && authorization.SecretRef != nil &&
			authorization.SecretRef.Name != "" && authorization.SecretRef.Key != "" {
			if err := validateSecretAndKeyExist(
				ctx, k8sClient, operatorNamespace, "the Dash0 authorization token",
				authorization.SecretRef.Name, authorization.SecretRef.Key); err != nil {
				return err
			}
		}
	}
	if export.Grpc != nil {
		if err := validateHeaderSecretRefsExist(
			ctx, k8sClient, operatorNamespace, "gRPC", export.Grpc.Headers); err != nil {
			return err
		}
	}
	if export.Http != nil {
		if err := validateHeaderSecretRefsExist(
			ctx, k8sClient, operatorNamespace, "HTTP", export.Http.Headers); err != nil {
			return err
		}
	}
	return nil
}

func validateHeaderSecretRefsExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	protocol string,
	headers []common.Header,
) error {
	for _, header := range headers {
		if header.ValueFrom == nil || header.ValueFrom.SecretKeyRef == nil {
			continue
		}
		secretKeyRef := header.ValueFrom.SecretKeyRef
		if secretKeyRef.Name == "" || secretKeyRef.Key == "" {
			continue
		}
		if err := validateSecretAndKeyExist(
			ctx, k8sClient, operatorNamespace,
			fmt.Sprintf("the value of the header \"%s\" of the %s export", header.Name, protocol),
			secretKeyRef.Name, secretKeyRef.Key); err != nil {
			return err
		}
	}
	return nil
}

func validateSecretAndKeyExist(
	ctx context.Context,
	k8sClient client.Client,
	operatorNamespace string,
	refPurpose string,
	secretName string,
	secretKey string,
) error {
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: secretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf(
				"the Kubernetes secret \"%s\", referenced for %s, does not exist in the namespace of the Dash0 "+
					"operator (%s); note that referenced secrets need to be created in the operator's namespace",
				secretName, refPurpose, operatorNamespace)
		}
		// Reading the secret failed for another reason (e.g. a transient API server issue). Do not block the admission
		// request in this case; the authoritative validation happens at reconcile time, when the collector resources
		// are created or updated.
		return nil
	}
	if _, hasKey := secret.Data[secretKey]; !hasKey {
		return fmt.Errorf(
			"the Kubernetes secret \"%s/%s\", referenced for %s, does not contain the key \"%s\"",
			operatorNamespace, secretName, refPurpose, secretKey)
	}
	return nil
}
