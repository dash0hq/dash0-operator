// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
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

// isReachabilityCheck reports whether the request is one of the dry-run requests with which the Helm chart's
// post-install hook checks that the webhooks are reachable. These requests are allowed right away, without validation
// or logging; since they are dry-run requests, nothing is persisted.
func isReachabilityCheck(request admission.Request) bool {
	return request.DryRun != nil &&
		*request.DryRun &&
		request.Operation == admissionv1.Create &&
		request.Name == util.WebhookReachabilityCheckResourceName
}

// componentSettingsAreUnchanged reports whether the request is an UPDATE that leaves spec.components unchanged, which
// is not checked again: a setting stored before the validation became stricter must not block updates of other fields.
// Returns an error response if the OldObject cannot be decoded.
func componentSettingsAreUnchanged[T client.Object, C any](
	request admission.Request,
	oldResource T,
	componentsOf func(T) C,
	components C,
	logger logd.Logger,
) (bool, *admission.Response) {
	if request.Operation != admissionv1.Update || request.OldObject.Raw == nil {
		return false, nil
	}
	if _, _, err := decoder.Decode(request.OldObject.Raw, nil, oldResource); err != nil {
		msg := "could not decode OldObject for the validation of spec.components"
		logger.Error(err, msg)
		errResponse := admission.Errored(http.StatusBadRequest, fmt.Errorf("%s: %w", msg, err))
		return false, &errResponse
	}
	return apiequality.Semantic.DeepEqual(componentsOf(oldResource), components), nil
}

// componentSettingsCheckResponse denies the request if a setting in spec.components is invalid, and otherwise allows it
// with one admission warning per setting that overrides a different value provided via the Helm chart.
func componentSettingsCheckResponse(
	resourceDescription string,
	check util.ComponentSettingsCheck,
	logger logd.Logger,
) admission.Response {
	if len(check.Invalid) > 0 {
		msg := fmt.Sprintf("The provided %s has invalid component settings: %s.",
			resourceDescription, strings.Join(check.Invalid, "; "))
		logger.Warn(msg)
		return admission.Denied(msg)
	}
	warnings := make([]string, 0, len(check.Conflicts))
	for _, conflict := range check.Conflicts {
		warnings = append(warnings, conflict.String())
	}
	return admission.Allowed("").WithWarnings(warnings...)
}
