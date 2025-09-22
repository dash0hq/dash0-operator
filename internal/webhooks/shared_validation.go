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
		return !(util.ReadBoolPointerWithDefault(export.Grpc.Insecure, false) &&
			util.ReadBoolPointerWithDefault(export.Grpc.InsecureSkipVerify, false))
	}
}
