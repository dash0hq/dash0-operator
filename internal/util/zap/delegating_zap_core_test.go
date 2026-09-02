// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"runtime"

	"go.uber.org/zap/zapcore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Delegating Zap Core", func() {

	It("Enabled without delegate and default level", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())
	})

	It("Enabled without delegate and custom level", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegatingCore.SetBufferingLevel(zapcore.WarnLevel)
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())
	})

	It("Enabled with a delegate (default level)", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		e := &oddEvenEnabler{}
		delegatingCore.SetDelegate(zapcore.NewCore(nil, nil, e))
		// DebugLevel (-1) is below the default level (InfoLevel = 0), so Enabled short-circuits
		// without consulting the delegate.
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())
		// DebugLevel was filtered by dc.level before reaching the delegate
		Expect(e.calledWith).To(HaveLen(3))
		Expect(e.calledWith[0]).To(Equal(zapcore.InfoLevel))
		Expect(e.calledWith[1]).To(Equal(zapcore.WarnLevel))
		Expect(e.calledWith[2]).To(Equal(zapcore.ErrorLevel))
	})

	It("Enabled with a delegate (debug level)", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegatingCore.SetBufferingLevel(zapcore.DebugLevel)
		e := &oddEvenEnabler{}
		delegatingCore.SetDelegate(zapcore.NewCore(nil, nil, e))
		// With DebugLevel as the configured level, all levels pass through to the delegate.
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse()) // delegate returns false (odd)
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeTrue())   // delegate returns true (even)
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeFalse())  // delegate returns false (odd)
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())  // delegate returns true (even)
		Expect(e.calledWith).To(HaveLen(4))
		Expect(e.calledWith[0]).To(Equal(zapcore.DebugLevel))
		Expect(e.calledWith[1]).To(Equal(zapcore.InfoLevel))
		Expect(e.calledWith[2]).To(Equal(zapcore.WarnLevel))
		Expect(e.calledWith[3]).To(Equal(zapcore.ErrorLevel))
	})

	It("Check is delegated when there is a delegate", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		entry := zapcore.Entry{Level: zapcore.InfoLevel}
		checkedEntry := &zapcore.CheckedEntry{}
		delegatingCore.Check(entry, checkedEntry)
		Expect(delegate.checkCalls).To(Equal(1))
	})

	It("Write without a delegate, hitting the limit", func() {
		logMessageBuffer := NewMru[*ZapEntryWithFields](3)
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		entry1 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "1"}
		entry2 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "2"}
		entry3 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "3"}
		entry4 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "4"}
		field := zapcore.Field{Key: "key", String: "value"}
		fields := []zapcore.Field{field}
		Expect(delegatingCore.Write(entry1, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry2, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry3, fields)).To(Succeed())

		Expect(logMessageBuffer.Len()).To(Equal(3))

		Expect(delegatingCore.Write(entry4, fields)).To(Succeed())

		Expect(logMessageBuffer.Len()).To(Equal(3))
		Expect(*logMessageBuffer.elements[0]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry2,
				Fields: fields,
			}),
		)
		Expect(*logMessageBuffer.elements[1]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry3,
				Fields: fields,
			}),
		)
		Expect(*logMessageBuffer.elements[2]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry4,
				Fields: fields,
			}),
		)
	})

	It("Write with a delegate", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		entry := zapcore.Entry{Level: zapcore.InfoLevel}
		field := zapcore.Field{Key: "key", String: "value"}
		fields := []zapcore.Field{field}
		Expect(delegatingCore.Write(entry, fields)).To(Succeed())
		Expect(delegatingCore.logMessageBuffer.IsEmpty()).To(BeTrue())
		Expect(delegate.writtenEntries).To(HaveLen(1))
		Expect(delegate.writtenEntries[0]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry,
				Fields: fields,
			}),
		)
	})

	It("Sync without a delegate", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		Expect(delegatingCore.Sync()).To(Succeed())
	})

	It("Sync with a delegate", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		Expect(delegatingCore.Sync()).To(Succeed())
		Expect(delegate.syncCalls).To(Equal(1))
	})

	It("buffered messages survive SetDelegate and can be spooled in order", func() {
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		entry1 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "1"}
		entry2 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "2"}
		entry3 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "3"}
		field := zapcore.Field{Key: "key", String: "value"}
		fields := []zapcore.Field{field}
		Expect(delegatingCore.Write(entry1, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry2, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry3, fields)).To(Succeed())
		Expect(logMessageBuffer.Len()).To(Equal(3))
		delegate := &mockDelegate{}

		delegatingCore.SetDelegate(delegate)

		Expect(delegate.writtenEntries).To(BeEmpty())
		Expect(logMessageBuffer.Len()).To(Equal(3))

		logMessageBuffer.ForAllAndClear(func(entry *ZapEntryWithFields) {
			Expect(delegate.Write(entry.Entry, entry.Fields)).To(Succeed())
		})

		Expect(delegate.writtenEntries).To(HaveLen(3))
		Expect(delegate.writtenEntries[0]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry1,
				Fields: fields,
			}),
		)
		Expect(delegate.writtenEntries[1]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry2,
				Fields: fields,
			}),
		)
		Expect(delegate.writtenEntries[2]).To(Equal(
			ZapEntryWithFields{
				Entry:  entry3,
				Fields: fields,
			}),
		)
		Expect(logMessageBuffer.IsEmpty()).To(BeTrue())
	})

	It("With without a delegate returns a derived core that accumulates fields", func() {
		logMessageBuffer := NewMru[*ZapEntryWithFields](13)
		originalDelegatingCore := NewDelegatingZapCore(logMessageBuffer)
		originalDelegatingCore.SetBufferingLevel(zapcore.DebugLevel)
		entry1 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "1"}
		entry2 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "2"}
		entry3 := zapcore.Entry{Level: zapcore.InfoLevel, Message: "3"}
		writeFields := []zapcore.Field{{Key: "key", String: "value"}}
		// write some log records to the original core
		Expect(originalDelegatingCore.Write(entry1, writeFields)).To(Succeed())
		Expect(originalDelegatingCore.Write(entry2, writeFields)).To(Succeed())
		Expect(logMessageBuffer.Len()).To(Equal(2))

		// create a derived core via With
		withFields1 := []zapcore.Field{
			{Key: "with1", String: "value1"},
			{Key: "with2", String: "value2"},
		}
		dc2Raw := originalDelegatingCore.With(withFields1)

		// verify With actually returned a new core, but with the same properties
		dc2, ok := dc2Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		Expect(dc2 == originalDelegatingCore).To(BeFalse())
		Expect(dc2.level).To(Equal(zapcore.DebugLevel))
		Expect(dc2.fields).To(HaveLen(2))
		Expect(dc2.fields[0].Key).To(Equal("with1"))
		Expect(dc2.fields[0].String).To(Equal("value1"))
		Expect(dc2.fields[1].Key).To(Equal("with2"))
		Expect(dc2.fields[1].String).To(Equal("value2"))

		// write some log records to the derived core
		Expect(dc2.Write(entry1, writeFields)).To(Succeed())
		Expect(dc2.Write(entry2, writeFields)).To(Succeed())
		Expect(dc2.Write(entry3, writeFields)).To(Succeed())
		// the derived core shares the buffer with the original core
		Expect(dc2.logMessageBuffer == originalDelegatingCore.logMessageBuffer).To(BeTrue())
		Expect(logMessageBuffer.Len()).To(Equal(5))
		// the derived core's own fields are prepended to the fields of the log record
		Expect(logMessageBuffer.elements[4].Fields).To(HaveLen(3))
		Expect(logMessageBuffer.elements[4].Fields[0].Key).To(Equal("with1"))
		Expect(logMessageBuffer.elements[4].Fields[1].Key).To(Equal("with2"))
		Expect(logMessageBuffer.elements[4].Fields[2].Key).To(Equal("key"))

		// derive once more, from the derived core
		withFields2 := []zapcore.Field{
			{Key: "with3", String: "value3"},
			{Key: "with4", String: "value4"},
			{Key: "with5", String: "value5"},
		}
		dc3Raw := dc2.With(withFields2)

		dc3, ok := dc3Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		Expect(dc3 == originalDelegatingCore).To(BeFalse())
		Expect(dc3 == dc2).To(BeFalse())
		Expect(dc3.level).To(Equal(zapcore.DebugLevel))
		// the clones share the log message buffer of the original core, buffered messages are not copied
		Expect(dc3.logMessageBuffer == originalDelegatingCore.logMessageBuffer).To(BeTrue())
		Expect(dc3.fields).To(HaveLen(5))
		Expect(dc3.fields[0].Key).To(Equal("with1"))
		Expect(dc3.fields[0].String).To(Equal("value1"))
		Expect(dc3.fields[1].Key).To(Equal("with2"))
		Expect(dc3.fields[1].String).To(Equal("value2"))
		Expect(dc3.fields[2].Key).To(Equal("with3"))
		Expect(dc3.fields[2].String).To(Equal("value3"))
		Expect(dc3.fields[3].Key).To(Equal("with4"))
		Expect(dc3.fields[3].String).To(Equal("value4"))
		Expect(dc3.fields[4].Key).To(Equal("with5"))
		Expect(dc3.fields[4].String).To(Equal("value5"))
	})

	It("With applies the accumulated fields to the delegate lazily, then memoizes the result", func() {
		logMessageBuffer := NewMru[*ZapEntryWithFields](13)
		delegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegatingCore.SetBufferingLevel(zapcore.DebugLevel)
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		expectedDelegateOfDerivedCore := &mockDelegate{}
		delegate.setWithReturnValue(expectedDelegateOfDerivedCore)

		withFields1 := []zapcore.Field{
			{Key: "with1", String: "value1"},
			{Key: "with2", String: "value2"},
		}
		dc2Raw := delegatingCore.With(withFields1)
		dc2, ok := dc2Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())

		// With does not touch the delegate, the fields are applied on first use
		Expect(delegate.withFields).To(BeEmpty())

		Expect(dc2.delegate() == expectedDelegateOfDerivedCore).To(BeTrue())
		Expect(delegate.withFields).To(HaveLen(2))
		Expect(delegate.withFields[0].Key).To(Equal("with1"))
		Expect(delegate.withFields[0].String).To(Equal("value1"))
		Expect(delegate.withFields[1].Key).To(Equal("with2"))
		Expect(delegate.withFields[1].String).To(Equal("value2"))

		// the derived delegate is memoized, a second use does not derive it again
		delegate.withFields = nil
		Expect(dc2.delegate() == expectedDelegateOfDerivedCore).To(BeTrue())
		Expect(delegate.withFields).To(BeEmpty())

		// the root core has no fields, so it uses the delegate as is
		Expect(delegatingCore.delegate() == delegate).To(BeTrue())
		Expect(delegate.withFields).To(BeEmpty())
	})

	It("SetDelegate and UnsetDelegate apply to cores derived via With, before and after the call", func() {
		logMessageBuffer := NewMru[*ZapEntryWithFields](13)
		originalDelegatingCore := NewDelegatingZapCore(logMessageBuffer)
		originalDelegatingCore.SetBufferingLevel(zapcore.DebugLevel)

		// derive two cores from originalDelegatingCore via With, before there is a delegate
		dc2, ok := originalDelegatingCore.With([]zapcore.Field{
			{Key: "with1", String: "value1"},
		}).(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		dc3, ok := dc2.With([]zapcore.Field{
			{Key: "with2", String: "value2"},
		}).(*DelegatingZapCore)
		Expect(ok).To(BeTrue())

		Expect(originalDelegatingCore.delegate()).To(BeNil())
		Expect(dc2.delegate()).To(BeNil())
		Expect(dc3.delegate()).To(BeNil())

		delegate := &mockDelegate{}
		delegateOfDerivedCores := &mockDelegate{}
		delegate.setWithReturnValue(delegateOfDerivedCores)

		originalDelegatingCore.SetDelegate(delegate)

		// the cores derived before the SetDelegate call pick up the new delegate
		Expect(originalDelegatingCore.delegate() == delegate).To(BeTrue())
		Expect(dc2.delegate() == delegateOfDerivedCores).To(BeTrue())
		Expect(dc3.delegate() == delegateOfDerivedCores).To(BeTrue())

		// a core derived after the SetDelegate call picks it up as well
		dc4, ok := originalDelegatingCore.With([]zapcore.Field{
			{Key: "with3", String: "value3"},
		}).(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		Expect(dc4.delegate() == delegateOfDerivedCores).To(BeTrue())

		originalDelegatingCore.UnsetDelegate()

		// the UnsetDelegate call applies to all derived cores, including their memoized delegates
		Expect(originalDelegatingCore.delegate()).To(BeNil())
		Expect(dc2.delegate()).To(BeNil())
		Expect(dc3.delegate()).To(BeNil())
		Expect(dc4.delegate()).To(BeNil())
	})

	It("does not retain cores derived via With", func() {
		// Regression test for OPE-535: the root core used to keep a reference to every core derived from it via With,
		// which retained one core plus its delegate per admission request and per reconcile for the lifetime of the
		// process.
		logMessageBuffer := NewMruWithDefaultSizeLimit[*ZapEntryWithFields]()
		originalDelegatingCore := NewDelegatingZapCore(logMessageBuffer)
		delegate := &mockDelegate{}
		delegate.setWithReturnValue(&mockDelegate{})
		originalDelegatingCore.SetDelegate(delegate)

		collected := make(chan struct{}, 1)
		deriveAndDrop(originalDelegatingCore, collected)

		Eventually(func() bool {
			runtime.GC()
			select {
			case <-collected:
				return true
			default:
				return false
			}
		}, "5s", "10ms").Should(BeTrue(), "the core derived via With was not garbage collected")

		// The root core lives for the lifetime of the process in production, so it has to stay reachable here as well.
		// Without this, the whole tree would be collected and the assertion above would pass even if the root core
		// retained every derived core.
		runtime.KeepAlive(originalDelegatingCore)
	})
})

// deriveAndDrop derives a core from the given core, uses it so that it memoizes its delegate, and registers a cleanup
// that signals on the given channel once the derived core is garbage collected. The derived core is unreachable when
// this function returns.
func deriveAndDrop(dc *DelegatingZapCore, collected chan struct{}) {
	derived, ok := dc.With([]zapcore.Field{{Key: "key", String: "value"}}).(*DelegatingZapCore)
	Expect(ok).To(BeTrue())
	// use the derived core, so that it memoizes its delegate
	Expect(derived.delegate()).ToNot(BeNil())
	runtime.AddCleanup(derived, func(ch chan struct{}) { ch <- struct{}{} }, collected)
}

type oddEvenEnabler struct {
	calledWith []zapcore.Level
}

func (e *oddEvenEnabler) Enabled(level zapcore.Level) bool {
	e.calledWith = append(e.calledWith, level)
	return level%2 == 0
}

type mockDelegate struct {
	checkCalls int
	syncCalls  int

	writtenEntries []ZapEntryWithFields

	withReturnValue zapcore.Core
	withFields      []zapcore.Field
}

func (dd *mockDelegate) setWithReturnValue(core zapcore.Core) {
	dd.withReturnValue = core
}

func (dd *mockDelegate) With(fields []zapcore.Field) zapcore.Core {
	dd.withFields = fields
	return dd.withReturnValue
}

func (dd *mockDelegate) Enabled(_ zapcore.Level) bool {
	return false
}

func (dd *mockDelegate) Check(_ zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	dd.checkCalls++
	return ce
}

func (dd *mockDelegate) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	dd.writtenEntries = append(dd.writtenEntries, ZapEntryWithFields{Entry: entry, Fields: fields})
	return nil
}

// Sync instructs the delegate to flush buffered logs, if there is a delegate. Otherwise, the call is ignored.
func (dd *mockDelegate) Sync() error {
	dd.syncCalls++
	return nil
}
