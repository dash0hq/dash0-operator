// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

type MatchAssertionError struct {
	Message string
}

func newMatchAssertionError(format string, a ...any) *MatchAssertionError {
	return &MatchAssertionError{
		Message: fmt.Sprintf(format, a...),
	}
}

func (m *MatchAssertionError) Error() string {
	return m.Message
}
