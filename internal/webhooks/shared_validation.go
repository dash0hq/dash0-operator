// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0
package webhooks

import (
	"github.com/dash0hq/dash0-operator/api/operator/common"
	"github.com/dash0hq/dash0-operator/internal/util"
)

type valid = bool

// ValidateGrpcExportInsecureFlags validates that the GRPC export config doesn't have both `insecure`
// and `insecureSkipVerify` enabled at the same time.
func validateGrpcExportInsecureFlags(export *common.Export) valid {
	if export == nil || export.Grpc == nil {
		return true
	} else {
		return !util.ReadBoolPointerWithDefault(export.Grpc.Insecure, false) ||
			!util.ReadBoolPointerWithDefault(export.Grpc.InsecureSkipVerify, false)
	}
}

// validateDash0ApiEndpoint validates that the Dash0 export's apiEndpoint (if set) points to a host on the hard-coded
// allowlist of permitted Dash0 API hosts. Returns nil if the export does not configure API access or if the endpoint is
// acceptable, or an error describing why the endpoint was rejected.
func validateDash0ApiEndpoint(export *common.Export) error {
	if export == nil || export.Dash0 == nil || export.Dash0.ApiEndpoint == "" {
		return nil
	}
	return common.ValidateDash0ApiEndpoint(export.Dash0.ApiEndpoint)
}
