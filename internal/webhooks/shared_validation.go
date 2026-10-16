// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"
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

// componentSettingsCheckResponse allows the request with one admission warning per setting in spec.components that
// overrides a different value provided via the Helm chart.
func componentSettingsCheckResponse(check util.ComponentSettingsCheck) admission.Response {
	warnings := make([]string, 0, len(check.Conflicts))
	for _, conflict := range check.Conflicts {
		warnings = append(warnings, conflict.String())
	}
	return admission.Allowed("").WithWarnings(warnings...)
}
