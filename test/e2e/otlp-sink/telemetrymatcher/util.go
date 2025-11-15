// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

func verifyResourceAttributeEquals[T any](
	resourceAttributes pcommon.Map,
	key string,
	expectedValue string,
	matchResult *ResourceMatchResult[T],
) {
	actualValue, hasValue := resourceAttributes.Get(key)
	if hasValue {
		if actualValue.Str() == expectedValue {
			matchResult.addPassedAssertion(key)
		} else {
			matchResult.addFailedAssertion(
				key,
				fmt.Sprintf("expected %s but it was %s", expectedValue, actualValue.Str()),
			)
		}
	} else {
		matchResult.addFailedAssertion(
			key,
			fmt.Sprintf("expected %s but the object had no such resource attribute", expectedValue),
		)
	}
}

func verifyResourceAttributeStartsWith[T any](
	resourceAttributes pcommon.Map,
	key string,
	expectedPrefix string,
	matchResult *ResourceMatchResult[T],
) {
	actualValue, hasValue := resourceAttributes.Get(key)
	if hasValue {
		if strings.HasPrefix(actualValue.Str(), expectedPrefix) {
			matchResult.addPassedAssertion(key)
		} else {
			matchResult.addFailedAssertion(
				key,
				fmt.Sprintf("expected a value starting with %s but it was %s", expectedPrefix, actualValue.Str()),
			)
		}
	} else {
		matchResult.addFailedAssertion(
			key,
			fmt.Sprintf(
				"expected a values starting with %s but the object had no such resource attribute",
				expectedPrefix,
			),
		)
	}
}

func verifyResourceAttributeExists[T any](
	resourceAttributes pcommon.Map,
	key string,
	matchResult *ResourceMatchResult[T],
) {
	_, hasValue := resourceAttributes.Get(key)
	if hasValue {
		matchResult.addPassedAssertion(key)
	} else {
		matchResult.addFailedAssertion(
			key,
			"expected any value but the object had no such resource attribute",
		)
	}
}

func workloadName(runtimeWorkloadName string, workloadType string) string {
	return fmt.Sprintf("%s-%s", runtimeWorkloadName, workloadType)
}
