// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"go.uber.org/zap/zapcore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Delegating Zap Core", func() {

	It("Enabled without delegate and default level", func() {
		delegatingCore := NewDelegatingZapCore()
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())
	})

	It("Enabled without delegate and custom level", func() {
		delegatingCore := NewDelegatingZapCore()
		delegatingCore.SetBufferingLevel(zapcore.WarnLevel)
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())
	})

	It("Enabled with a delegate", func() {
		delegatingCore := NewDelegatingZapCore()
		e := &oddEvenEnabler{}
		delegatingCore.SetDelegate(zapcore.NewCore(nil, nil, e))
		Expect(delegatingCore.Enabled(zapcore.DebugLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.InfoLevel)).To(BeTrue())
		Expect(delegatingCore.Enabled(zapcore.WarnLevel)).To(BeFalse())
		Expect(delegatingCore.Enabled(zapcore.ErrorLevel)).To(BeTrue())
		Expect(e.calledWith).To(HaveLen(4))
		Expect(e.calledWith[0]).To(Equal(zapcore.DebugLevel))
		Expect(e.calledWith[1]).To(Equal(zapcore.InfoLevel))
		Expect(e.calledWith[2]).To(Equal(zapcore.WarnLevel))
		Expect(e.calledWith[3]).To(Equal(zapcore.ErrorLevel))
	})

	It("Enabled with a delegate", func() {
		delegatingCore := NewDelegatingZapCore()
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		entry := zapcore.Entry{Level: zapcore.InfoLevel}
		checkedEntry := &zapcore.CheckedEntry{}
		delegatingCore.Check(entry, checkedEntry)
		Expect(delegate.checkCalls).To(Equal(1))
	})

	It("Write without a delegate, hitting the limit", func() {
		delegatingCore := NewDelegatingZapCoreWithBufferSize(3)
		entry1 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry2 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry3 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry4 := zapcore.Entry{Level: zapcore.InfoLevel}
		field := zapcore.Field{Key: "key", String: "value"}
		fields := []zapcore.Field{field}
		Expect(delegatingCore.Write(entry1, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry2, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry3, fields)).To(Succeed())

		Expect(delegatingCore.bufferedLogRecords.Len()).To(Equal(3))

		Expect(delegatingCore.Write(entry4, fields)).To(Succeed())

		Expect(delegatingCore.bufferedLogRecords.Len()).To(Equal(3))
		Expect(delegatingCore.bufferedLogRecords.elements[0]).To(Equal(
			entryWithFields{
				entry:  entry2,
				fields: fields,
			}),
		)
		Expect(delegatingCore.bufferedLogRecords.elements[1]).To(Equal(
			entryWithFields{
				entry:  entry3,
				fields: fields,
			}),
		)
		Expect(delegatingCore.bufferedLogRecords.elements[2]).To(Equal(
			entryWithFields{
				entry:  entry4,
				fields: fields,
			}),
		)
	})

	It("Write with a delegate", func() {
		delegatingCore := NewDelegatingZapCore()
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		entry := zapcore.Entry{Level: zapcore.InfoLevel}
		field := zapcore.Field{Key: "key", String: "value"}
		fields := []zapcore.Field{field}
		Expect(delegatingCore.Write(entry, fields)).To(Succeed())
		Expect(delegatingCore.bufferedLogRecords.IsEmpty()).To(BeTrue())
		Expect(delegate.writtenEntries).To(HaveLen(1))
		Expect(delegate.writtenEntries[0]).To(Equal(
			entryWithFields{
				entry:  entry,
				fields: fields,
			}),
		)
	})

	It("Sync without a delegate", func() {
		delegatingCore := NewDelegatingZapCore()
		Expect(delegatingCore.Sync()).To(Succeed())
	})

	It("Sync with a delegate", func() {
		delegatingCore := NewDelegatingZapCore()
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		Expect(delegatingCore.Sync()).To(Succeed())
		Expect(delegate.syncCalls).To(Equal(1))
	})

	It("SetDelegate spools buffered messages to new delegate in order", func() {
		delegatingCore := NewDelegatingZapCore()
		entry1 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry2 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry3 := zapcore.Entry{Level: zapcore.InfoLevel}
		field := zapcore.Field{Key: "key", String: "value"}
		fields := []zapcore.Field{field}
		Expect(delegatingCore.Write(entry1, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry2, fields)).To(Succeed())
		Expect(delegatingCore.Write(entry3, fields)).To(Succeed())
		Expect(delegatingCore.bufferedLogRecords.Len()).To(Equal(3))
		delegate := &mockDelegate{}
		Expect(delegate.writtenEntries).To(BeEmpty())

		delegatingCore.SetDelegate(delegate)

		Expect(delegate.writtenEntries).To(HaveLen(3))
		Expect(delegate.writtenEntries[0]).To(Equal(
			entryWithFields{
				entry:  entry1,
				fields: fields,
			}),
		)
		Expect(delegate.writtenEntries[1]).To(Equal(
			entryWithFields{
				entry:  entry2,
				fields: fields,
			}),
		)
		Expect(delegate.writtenEntries[2]).To(Equal(
			entryWithFields{
				entry:  entry3,
				fields: fields,
			}),
		)
		Expect(delegatingCore.bufferedLogRecords.IsEmpty()).To(BeTrue())
	})

	It("With without a delegate returns a clone", func() {
		originalDelegatingCore := NewDelegatingZapCoreWithBufferSize(13)
		originalDelegatingCore.SetBufferingLevel(zapcore.DebugLevel)
		entry1 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry2 := zapcore.Entry{Level: zapcore.InfoLevel}
		entry3 := zapcore.Entry{Level: zapcore.InfoLevel}
		writeFields := []zapcore.Field{{Key: "key", String: "value"}}
		// write some log records to the original core
		Expect(originalDelegatingCore.Write(entry1, writeFields)).To(Succeed())
		Expect(originalDelegatingCore.Write(entry2, writeFields)).To(Succeed())
		Expect(originalDelegatingCore.bufferedLogRecords.Len()).To(Equal(2))

		// create a clone via With
		withFields1 := []zapcore.Field{
			{Key: "with1", String: "value1"},
			{Key: "with2", String: "value2"},
		}
		dc2Raw := originalDelegatingCore.With(withFields1)

		// verify With actually returned a clone, but with the same properties (except for buffered messages)
		dc2, ok := dc2Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		Expect(dc2 == originalDelegatingCore).To(BeFalse())
		Expect(dc2.bufferSize).To(Equal(13))
		// we do not copy buffered messages when cloning via With
		Expect(dc2.bufferedLogRecords.IsEmpty()).To(BeTrue())
		Expect(dc2.level).To(Equal(zapcore.DebugLevel))
		Expect(dc2.fields).To(HaveLen(2))
		Expect(dc2.fields[0].Key).To(Equal("with1"))
		Expect(dc2.fields[0].String).To(Equal("value1"))
		Expect(dc2.fields[1].Key).To(Equal("with2"))
		Expect(dc2.fields[1].String).To(Equal("value2"))

		// write some log records to the first clone
		Expect(dc2.Write(entry1, writeFields)).To(Succeed())
		Expect(dc2.Write(entry2, writeFields)).To(Succeed())
		Expect(dc2.Write(entry3, writeFields)).To(Succeed())
		// verify they have been written to the clone
		Expect(dc2.bufferedLogRecords.Len()).To(Equal(3))
		// verify they have been not been written to original
		Expect(originalDelegatingCore.bufferedLogRecords.Len()).To(Equal(2))

		// create a clone of the clone
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
		Expect(dc3.bufferSize).To(Equal(13))
		Expect(dc3.bufferedLogRecords.IsEmpty()).To(BeTrue())
		Expect(dc3.level).To(Equal(zapcore.DebugLevel))
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

	It("With delegates the call to the delegate when there is one", func() {
		delegatingCore := NewDelegatingZapCoreWithBufferSize(13)
		delegatingCore.SetBufferingLevel(zapcore.DebugLevel)
		delegate := &mockDelegate{}
		delegatingCore.SetDelegate(delegate)
		expectedDelegateOfClone := &mockDelegate{}
		delegate.setWithReturnValue(expectedDelegateOfClone)

		// create a clone via With
		withFields1 := []zapcore.Field{
			{Key: "with1", String: "value1"},
			{Key: "with2", String: "value2"},
		}
		dc2Raw := delegatingCore.With(withFields1)

		// verify With actually returned a clone, but with the same properties
		dc2, ok := dc2Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		delegateOfCloneRaw := dc2.delegate.Load()
		Expect(delegateOfCloneRaw).ToNot(BeNil())
		delegateOfClone, ok := (*delegateOfCloneRaw).(*mockDelegate)
		Expect(ok).To(BeTrue())
		Expect(delegateOfClone == expectedDelegateOfClone).To(BeTrue())

		Expect(delegate.withFields).To(HaveLen(2))
		Expect(delegate.withFields[0].Key).To(Equal("with1"))
		Expect(delegate.withFields[0].String).To(Equal("value1"))
		Expect(delegate.withFields[1].Key).To(Equal("with2"))
		Expect(delegate.withFields[1].String).To(Equal("value2"))
	})

	It("With keeps track of all clones, SetDelegate and UnsetDelegate are propagated to clones", func() {
		originalDelegatingCore := NewDelegatingZapCoreWithBufferSize(13)
		originalDelegatingCore.SetBufferingLevel(zapcore.DebugLevel)

		// create two clones of originalDelegatingCore via With
		withFields1 := []zapcore.Field{
			{Key: "with1", String: "value1"},
			{Key: "with2", String: "value2"},
		}
		dc2Raw := originalDelegatingCore.With(withFields1)
		dc2, ok := dc2Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())
		withFields2 := []zapcore.Field{
			{Key: "with3", String: "value4"},
			{Key: "with4", String: "value4"},
		}
		dc3Raw := originalDelegatingCore.With(withFields2)
		dc3, ok := dc3Raw.(*DelegatingZapCore)
		Expect(ok).To(BeTrue())

		// verify we are keeping track of clones
		Expect(originalDelegatingCore.clones).To(HaveLen(2))
		Expect(originalDelegatingCore.clones[0] == dc2).To(BeTrue())
		Expect(originalDelegatingCore.clones[1] == dc3).To(BeTrue())

		// set a delegate on the original, this delegate is supposed to be propagated to all clones
		delegate := &mockDelegate{}
		originalDelegatingCore.SetDelegate(delegate)

		// verify the SetDelegate call has been propagated to all clones
		delegateOfOriginalRaw := originalDelegatingCore.delegate.Load()
		Expect(delegateOfOriginalRaw).ToNot(BeNil())
		delegateOfOriginal, ok := (*delegateOfOriginalRaw).(*mockDelegate)
		Expect(ok).To(BeTrue())

		delegateOfFirstCloneRaw := dc2.delegate.Load()
		Expect(delegateOfFirstCloneRaw).ToNot(BeNil())
		delegateOfFirstClone, ok := (*delegateOfFirstCloneRaw).(*mockDelegate)
		Expect(ok).To(BeTrue())

		delegateOfSecondCloneRaw := dc2.delegate.Load()
		Expect(delegateOfSecondCloneRaw).ToNot(BeNil())
		delegateOfSecondClone, ok := (*delegateOfSecondCloneRaw).(*mockDelegate)
		Expect(ok).To(BeTrue())

		Expect(delegateOfFirstClone == delegateOfOriginal).To(BeTrue())
		Expect(delegateOfSecondClone == delegateOfOriginal).To(BeTrue())

		// unset the delegate on the original, this is supposed to be propagated to all clones as well
		originalDelegatingCore.UnsetDelegate()

		// verify the UnsetDelegate call has been propagated to all clones
		Expect(originalDelegatingCore.delegate.Load()).To(BeNil())
		Expect(dc2.delegate.Load()).To(BeNil())
		Expect(dc3.delegate.Load()).To(BeNil())
	})
})

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

	writtenEntries []entryWithFields

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
	dd.writtenEntries = append(dd.writtenEntries, entryWithFields{entry: entry, fields: fields})
	return nil
}

// Sync instructs the delegate to flush buffered logs, if there is a delegate. Otherwise, the call is ignored.
func (dd *mockDelegate) Sync() error {
	dd.syncCalls++
	return nil
}
