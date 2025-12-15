// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
)

//nolint:lll
var (
	ExpectedTargetAllocatorConfigMapName  = fmt.Sprintf("%s-opentelemetry-target-allocator-cm", TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorDeploymentName = fmt.Sprintf("%s-opentelemetry-target-allocator-deployment", TargetAllocatorPrefixTest)
	ExpectedTargetAllocatorServiceName    = fmt.Sprintf("%s-opentelemetry-target-allocator-service", TargetAllocatorPrefixTest)
)
