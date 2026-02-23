package util

import (
	"log/slog"

	"github.com/go-logr/logr"

	. "github.com/onsi/gomega"
)

func NewCapturingLogger() (logr.Logger, *CapturingLogSink) {
	logSink := &CapturingLogSink{
		runtimeInfo: logr.RuntimeInfo{
			CallDepth: 1,
		},
	}
	return logr.New(logSink), logSink
}

type CapturingLogSink struct {
	messages    []string
	runtimeInfo logr.RuntimeInfo
}

func (s *CapturingLogSink) doLog(_ int, msg string, _ ...any) {
	s.messages = append(s.messages, msg)
}

func (s *CapturingLogSink) Init(info logr.RuntimeInfo) {
	s.runtimeInfo = info
}

func (s *CapturingLogSink) Enabled(_ int) bool {
	return true
}

func (s *CapturingLogSink) Info(level int, msg string, keysAndValues ...any) {
	s.doLog(level, msg, keysAndValues...)
}

func (s *CapturingLogSink) Error(err error, msg string, keysAndValues ...any) {
	s.doLog(int(slog.LevelError), msg, append(keysAndValues, err)...)
}

func (s *CapturingLogSink) WithValues(_ ...any) logr.LogSink {
	return s
}

func (s *CapturingLogSink) WithName(_ string) logr.LogSink {
	return s
}

func (s *CapturingLogSink) Reset() {
	s.messages = nil
}

func (s *CapturingLogSink) HasLogMessage(g Gomega, message string) {
	g.Expect(s.messages).To(ContainElement(message))
}

func (s *CapturingLogSink) HasNoLogMessages(g Gomega) {
	g.Expect(s.messages).To(BeEmpty())
}
