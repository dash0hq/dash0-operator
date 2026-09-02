// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"slices"
	"sync/atomic"

	"go.uber.org/zap/zapcore"
)

// DelegatingZapCore is a Zap core that keeps messages in a size-limited buffer until an actual zapcore.Core is added as
// a delegate via SetDelegate. The messages that are already buffered can then be spooled to the new core via
// DelegatingZapCoreWrapper.LogMessageBuffer.ForAllAndClear. After SetDelegate, DelegatingZapCore will pass all messages
// to the delegate.
//
// DelegatingZapCores conceptually form a tree structure, just like zap loggers do; without representing an actual tree
// in memory. You start with a root DelegatingZapCore, and then derive child loggers (by calling `With`) that add
// additional fields (which will be included in every log record written by that child logger). There are no references
// from parent to child or vice versa.
type DelegatingZapCore struct {
	sharedDelegate   *sharedDelegate
	derivedDelegate  atomic.Pointer[derivedDelegate]
	logMessageBuffer *Mru[*ZapEntryWithFields]
	level            zapcore.Level
	fields           []zapcore.Field
}

// sharedDelegate holds the single delegate for an entire DelegatingZapCore tree. It always starts out without an
// actual Zap core. The intended usage is that some time later an actual zapcore.Core is provided via
// DelegatingZapCore#SetDelegate. The zapcore.Core is then stored in the sharedDelegate.
//
// The root core of a tree and every core derived from it via `With` need to have access to the delegate. For that
// purpose, all cores of the tree hold a pointer to the same sharedDelegate instance. They can look up the current core
// from the sharedDelegate reference, and then write log messages to it.
//
// Note that the references only point from the derived DelegatingZapCores to the sharedDelegate, *not* the other way
// around, or from a DelegatingZapCore to its child cores. This makes a DelegatingZapCore derived via `With` eligible
// for garbage collection as soon as the logger holding it goes out of scope. Tracking the derived DelegatingZapCores
// in their parent would create a reference chain from the root DelegatingZapCore to every derived DelegatingZapCore,
// and retain every logger ever derived for the lifetime of the root DelegatingZapCore object - i.e. it would be a
// memory leak.
//
// The generation counter is used for caching cores together with their applied Zap fields. The generation is
// incremented on every SetDelegate/UnsetDelegate call. This lets a derived core detect that the delegate it has
// memoized is stale. See also the `derivedDelegate` struct, which holds the counterpart for the generation comparison,
// and DelegatingZapCore#delegate() which handles the memoization.
//
// The two fields (delegate and generation) are written separately, without a lock. That is safe because a derived
// DelegatingZapCoreDelegatingZapCore only uses its memoized delegate while the memoized generation still equals the current one, and
// because writers always store the delegate before they increment the generation: observing a generation means the
// delegate for that generation has already been stored, so a reader either memoizes the matching delegate or memoizes
// one tagged with a generation that is already superseded, which makes it derive again on the next call. Do not replace
// the two fields with a single atomic pointer to a combined struct unless the update uses compare-and-swap: a plain
// load-modify-store can drop a concurrent increment, and two writers would then be able to settle on a generation whose
// delegate is not the one that was stored last.
type sharedDelegate struct {
	// delegate is the reference to the current zapcore.Core for the whole tree of DelegatingZapCore objects. If a new
	// delegate is set (removed) via SetDelegate (UnsetDelegate), the generation counter is incremented as well.
	delegate   atomic.Pointer[zapcore.Core]
	generation atomic.Uint64
}

// derivedDelegate is the delegate of a single DelegatingZapCore, that is, it is that tree node's delegate with the
// core's fields applied, together with the generation it has been derived from.
//
// A DelegatingZapCore derived via `With` cannot use sharedDelegate.delegate directly to write log messages, the fields
// provided via With would be missing. Naively, it would need to call zapcore.Core#With(dc.fields) for every single log
// message, to apply the Zap fields. The otelzap conversion is somewhat expensive though. Thus, each DelegatingZapCore
// in the tree keeps its own copy of the zapcore.Core with all fields already applied. This is what the derivedDelegate
// is for. Its generation counter is used to compare with sharedDelegate.generation, to determine if we are still
// logging to the same zapcore.Core, or if it has been replaced via SetDelegate/UnsetDelegate since memoizing it.
//
// The first log call after a delegate change pays for the otelzap conversion once. Every call after that is two atomic
// loads and a comparison.
//
// A nil delegate means the DelegatingZapCore tree had no delegate at that generation.
type derivedDelegate struct {
	// delegate is the zapcore.Core with applied fields
	delegate zapcore.Core
	// generation is the generation of the sharedDelegate that was active when this derivedDelegate was created. It is
	// never updated, if a derivedDelegate's generation is checked in delegate() and does not match, a new derivedDelegate
	// is created.
	generation uint64
}

// ZapEntryWithFields represents a single log entry, i.e. a log message with additional fields.
type ZapEntryWithFields struct {
	Entry  zapcore.Entry
	Fields []zapcore.Field
}

// NewDelegatingZapCore creates a DelegatingZapCore.
func NewDelegatingZapCore(logMessageBuffer *Mru[*ZapEntryWithFields]) *DelegatingZapCore {
	return &DelegatingZapCore{
		sharedDelegate:   &sharedDelegate{},
		logMessageBuffer: logMessageBuffer,
		level:            zapcore.InfoLevel,
	}
}

// SetBufferingLevel sets the level to be used in Enabled() when no delegate is set, that is, the provided level
// determines whether a message is buffered or not. In particular, it is used in Check() in case no delegate is set.
func (dc *DelegatingZapCore) SetBufferingLevel(lvl zapcore.Level) {
	dc.level = lvl
}

// With adds structured context to the Core. All fields are stored, independent of whether a delegate is set or not.
// They are applied to the delegate lazily, when the returned core logs for the first time.
func (dc *DelegatingZapCore) With(fields []zapcore.Field) zapcore.Core {
	return &DelegatingZapCore{
		sharedDelegate: dc.sharedDelegate,
		// We deliberately do not clone the buffer, since the original DelegatingZapCore still has the buffered
		// messages, cloning the buffer as well might lead to emitting log records twice
		logMessageBuffer: dc.logMessageBuffer,
		level:            dc.level,
		fields:           slices.Concat(dc.fields, fields),
	}
}

// delegate returns the tree's delegate with this core's fields applied, or nil if the tree has no delegate. The result
// is memoized until the next SetDelegate or UnsetDelegate call.
func (dc *DelegatingZapCore) delegate() zapcore.Core {
	generation := dc.sharedDelegate.generation.Load()
	if memoized := dc.derivedDelegate.Load(); memoized != nil && memoized.generation == generation {
		// The cached derivedDelegate with its applied fields is still valid, there has been no SetDelegate/UnsetDelegate
		// call since it has been memoized.
		return memoized.delegate
	}

	// Either we have never memoized a core for this DelegatingZapCore, or the memoized core has been invalidated by
	// a SetDelegate/UnsetDelegate call. Create a new derivedDelegate and memoize it.
	var delegate zapcore.Core
	if treeDelegate := dc.sharedDelegate.delegate.Load(); treeDelegate != nil {
		delegate = *treeDelegate
		if len(dc.fields) > 0 {
			// Apply the Zap fields.
			delegate = delegate.With(dc.fields)
		}
	}

	// Two goroutines can derive concurrently. Also, a concurrent SetDelegate can make the value we are about to store
	// stale before we store it. Both are benign: the stored generation then no longer matches, and the next call
	// derives again.
	dc.derivedDelegate.Store(&derivedDelegate{delegate: delegate, generation: generation})
	return delegate
}

func (dc *DelegatingZapCore) Enabled(lvl zapcore.Level) bool {
	if delegate := dc.delegate(); delegate != nil {
		return lvl >= dc.level && delegate.Enabled(lvl)
	}

	return lvl >= dc.level
}

// Check determines whether the supplied Entry should be logged. If a delegate is set, the call will simply be delegated
// to the delegate's Check method, otherwise it will be checked against the level set in SetBufferingLevel.
func (dc *DelegatingZapCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if delegate := dc.delegate(); delegate != nil {
		// Respect the configured level so that debug messages are not forwarded to the OTel pipeline when not in
		// development mode.
		if entry.Level < dc.level {
			return ce
		}
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
		return delegate.Check(entry, ce)
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
	if delegate := dc.delegate(); delegate != nil {
		// Note: This branch is actually never executed. When a delegate is present, the Write method is no longer
		// called for the delegating zap core, since the implementation of Check delegates the Check call to the
		// delegate already. Within delegate.Check, the delegate adds itself via AddCore to the checked entry. Write is
		// only called for cores that have been added to a checked entry.
		return delegate.Write(entry, fields)
	}

	finalFields := slices.Concat(dc.fields, fields)
	dc.logMessageBuffer.Put(&ZapEntryWithFields{Entry: entry, Fields: finalFields})
	return nil
}

// Sync instructs the delegate to flush buffered logs, if there is a delegate. Otherwise, the call is ignored.
func (dc *DelegatingZapCore) Sync() error {
	if delegate := dc.delegate(); delegate != nil {
		return delegate.Sync()
	}

	// ignore sync calls if no delegate is set
	return nil
}

// SetDelegate sets the delegate core to which all messages will be forwarded. This also applies to all cores that have
// been derived from this core via With, before or after this call. The messages that are already buffered can then be
// spooled to the new core via DelegatingZapCoreWrapper.LogMessageBuffer.ForAllAndClear.
func (dc *DelegatingZapCore) SetDelegate(delegate zapcore.Core) {
	// Store the delegate before incrementing the generation, never the other way around. That way, if delegate() memoizes
	// a new derivedDelegate, it would err on the side of storing a too-new delegate with an older generation counter, and
	// memoize again on the next log call. That is, the error is a wasted With call.
	//
	// If instead we did the increment first, a reader can observe the new generation together with an outdated delegate.
	// That is much worse, it will memoize that pair and the stale delegate is potentially trusted for the rest of the
	// process lifetime.
	dc.sharedDelegate.delegate.Store(&delegate)
	dc.sharedDelegate.generation.Add(1)
}

// UnsetDelegate will remove the current delegate (if any). This also applies to all cores that have been derived from
// this core via With, before or after this call.
func (dc *DelegatingZapCore) UnsetDelegate() {
	// Clear the delegate before incrementing the generation, for the same reason as in SetDelegate.
	dc.sharedDelegate.delegate.Store(nil)
	dc.sharedDelegate.generation.Add(1)
}

func (dc *DelegatingZapCore) ForTestOnlyHasDelegate() bool {
	return dc.delegate() != nil
}
