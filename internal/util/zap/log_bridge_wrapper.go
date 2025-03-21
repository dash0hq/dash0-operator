// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

// DelegatingZapCoreWrapper is a wrapper around the root instance of the DelegatingZapCore tree and the global buffer
// for log messages.
type DelegatingZapCoreWrapper struct {
	RootDelegatingZapCore *DelegatingZapCore
	LogMessageBuffer      *Mru[*ZapEntryWithFields]
}

func NewDelegatingZapCoreWrapper() *DelegatingZapCoreWrapper {
	logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
	delegatingZapCore := NewDelegatingZapCore(logMessageBuffer)
	return &DelegatingZapCoreWrapper{
		RootDelegatingZapCore: delegatingZapCore,
		LogMessageBuffer:      logMessageBuffer,
	}
}
