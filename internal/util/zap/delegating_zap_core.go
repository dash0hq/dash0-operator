// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"slices"
	"sync/atomic"

	"go.uber.org/zap/zapcore"
)

// DelegatingZapCore is a Zap core that keeps messages in a size-limited buffer until another core is added as a
// delegate, then spools all buffered messages to the delegate. After that, the core will pass all messages to the
// delegate.
type DelegatingZapCore struct {
	delegate         atomic.Pointer[zapcore.Core]
	logMessageBuffer *Mru[*ZapEntryWithFields]
	level            zapcore.Level
	fields           []zapcore.Field
	clones           []*DelegatingZapCore
}

type ZapEntryWithFields struct {
	Entry  zapcore.Entry
	Fields []zapcore.Field
}

// NewDelegatingZapCore creates a DelegatingZapCore.
func NewDelegatingZapCore(logMessageBuffer *Mru[*ZapEntryWithFields]) *DelegatingZapCore {
	return &DelegatingZapCore{
		logMessageBuffer: logMessageBuffer,
		level:            zapcore.InfoLevel,
	}
}

// SetBufferingLevel sets the level to be used in Enabled() when no delegate is set, that is, the provided level
// determines whether a message is buffered or not. In particular, it is used in Check() in case no delegate is set.
func (dc *DelegatingZapCore) SetBufferingLevel(lvl zapcore.Level) {
	dc.level = lvl
}

// With adds structured context to the Core. The provided fields will be forwarded to the delegate if one is set. All
// fields will also be stored, independent of whether a delegate is set or not.
func (dc *DelegatingZapCore) With(fields []zapcore.Field) zapcore.Core {
	clone := DelegatingZapCore{
		// We deliberately do not clone the buffer, since the original DelegatingZapCore still has the buffered
		// messages, cloning the buffer as well might lead to emitting log records twice
		logMessageBuffer: dc.logMessageBuffer,
		level:            dc.level,
		fields:           slices.Concat(dc.fields, fields),
	}
	dc.clones = append(dc.clones, &clone)

	// If this core has a delegate, we propagate the With call to the delegate and set the result as the delegate for
	// the cloned DelegatingZapCore.
	delegate := dc.delegate.Load()
	if delegate != nil {
		newDelegate := (*delegate).With(fields)
		clone.delegate.Store(&newDelegate)
	}

	return &clone
}

func (dc *DelegatingZapCore) Enabled(lvl zapcore.Level) bool {
	delegate := dc.delegate.Load()
	if delegate != nil {
		return (*delegate).Enabled(lvl)
	}

	return lvl >= dc.level
}

// Check determines whether the supplied Entry should be logged. If a delegate is set, the call will simply be delegated
// to the delegate's Check method, otherwise it will be checked against the level set in SetBufferingLevel.
func (dc *DelegatingZapCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	delegate := dc.delegate.Load()
	if delegate != nil {
		if !zapcore.DebugLevel.Enabled(entry.Level) { // this is equivalent to `entry.Level < -1`
			// There is some unfortunate interaction going on between controller-runtime debug logging and the zap OTel
			// bridge. When controller-runtime logs with a level below -1, like for example here:
			// https://github.com/kubernetes-sigs/controller-runtime/blob/5dfe3216fb7fd7f5afb61d6d0f8956c7bec8df62/pkg/webhook/authentication/http.go#L105
			// (basically always when somthing like logger.V(5) or similar is used, which can result in a level of -8
			// for example), the zap OTel bridge's [convertLevel function](https://github.com/open-telemetry/opentelemetry-go-contrib/blob/b84ed3a871d50d4565c3bedb4e545784cc33e4a5/bridges/otelzap/core.go#L243-L262)
			// converts that to log.SeverityUndefined. The following "Enabled" in the OTel zap bridge returns true.
			// This effectively lets controller-runtime debug logs bleed into the log records sent to the Dash0 backend
			// for self-monitoring. To prevent this, we directly filter out anything with a level < -1.
			return ce
		}
		return (*delegate).Check(entry, ce)
	}

	if dc.Enabled(entry.Level) {
		return ce.AddCore(entry, dc)
	}
	return ce
}

// Write will forward the entry and fields to the delegate if one is set, or put it into the interal buffer otherwise.
//
// If called, Write will always do one of those two things, it will not replicate the logic of Check.
func (dc *DelegatingZapCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	delegate := dc.delegate.Load()
	if delegate != nil {
		// Note: This branch is actually never executed. When a delegate is present, the Write method is no longer
		// called for the delegating zap core, since the implementation of Check delegates the Check call to the
		// delegate already. Within delegate.Check, the delegate adds itself via AddCore to the checked entry. Write is
		// only called for cores that have been added to a checked entry.
		return (*delegate).Write(entry, fields)
	}

	finalFields := slices.Concat(dc.fields, fields)
	dc.logMessageBuffer.Put(&ZapEntryWithFields{Entry: entry, Fields: finalFields})
	return nil
}

// Sync instructs the delegate to flush buffered logs, if there is a delegate. Otherwise, the call is ignored.
func (dc *DelegatingZapCore) Sync() error {
	delegate := dc.delegate.Load()
	if delegate != nil {
		return (*delegate).Sync()
	}

	// ignore sync calls if no delegate is set
	return nil
}

// SetDelegate sets the delegate core to which all messages will be forwarded. Also, all buffered messages will be
// spooled to the delegate, and then deleted from the DelegatingZapCore's buffer. Check will not be called again, the
// buffered entries will be written unconditionally. Errors occurring during spooling will be silently ignored.
// The SetDelegate call will also be propagated to all clones created via With previous to this call.
func (dc *DelegatingZapCore) SetDelegate(delegate zapcore.Core) {
	dc.delegate.Store(&delegate)
	for _, clone := range dc.clones {
		clone.SetDelegate(delegate)
	}
}

// UnsetDelegate will remove the current delegate (if any). The UnsetDelegate call will also be propagated to all clones
// created via With previous to this call.
func (dc *DelegatingZapCore) UnsetDelegate() {
	dc.delegate.Store(nil)
	for _, clone := range dc.clones {
		clone.UnsetDelegate()
	}
}

func (dc *DelegatingZapCore) ForTestOnlyHasDelegate() bool {
	return dc.delegate.Load() != nil
}
