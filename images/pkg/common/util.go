// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package common // import "github.com/dash0hq/dash0-operator/images/pkg/common"

const (
	DefaultErrorMetricAttributeLength = 80
)

func TruncateError(err error) string {
	if err == nil {
		return "nil"
	}
	return TruncateString(err.Error(), DefaultErrorMetricAttributeLength)
}

func TruncateString(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:max(0, length)]
}
