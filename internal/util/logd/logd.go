// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package logd

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	telemetryCollectionIssueMarker = "dash0.monitoring.telemetry_collection_issue"
)

// Logger is a thin wrapper around logr.Logger that adds
//   - a Debug method using V(1), which maps to zapcore.DebugLevel (-1) via the zapr bridge
//     (logr V level n maps to zap level -n).
//   - a Warn method to log warnings
//
// In production mode, the zap logger is configured at info level (0), so Debug messages are filtered out.
type Logger struct {
	logr.Logger
}

// NewLogger wraps a logr.Logger.
func NewLogger(logger logr.Logger) Logger {
	return Logger{Logger: logger}
}

// LoggerFromContext extracts a logr.Logger from the context and wraps it.
func FromContext(ctx context.Context) Logger {
	return NewLogger(log.FromContext(ctx))
}

// Debug logs a message at V(1) level (zapcore.DebugLevel).
func (l Logger) Debug(msg string, keysAndValues ...any) {
	l.Logger.WithCallDepth(1).V(1).Info(msg, keysAndValues...)
}

// Warn logs a message at zapcore.WarnLevel by accessing the underlying *zap.Logger via zapr.Underlier.
// Falls back to Info level if the sink does not implement zapr.Underlier (e.g. in tests).
func (l Logger) Warn(msg string, keysAndValues ...any) {
	if underlier, ok := l.GetSink().(zapr.Underlier); ok {
		underlier.GetUnderlying().WithOptions(zap.AddCallerSkip(1)).Sugar().Warnw(msg, keysAndValues...)
		return
	}
	l.Logger.WithCallDepth(1).Info(msg, keysAndValues...)
}

// ErrorAsWarn logs an error at level at zapcore.WarnLevel.
func (l Logger) ErrorAsWarn(err error, msg string, keysAndValues ...any) {
	l.Warn(msg, append([]any{"error", err}, keysAndValues...)...)
}

// WarnTelemetryCollectionIssue logs a telemetry collection issue at level at zapcore.WarnLevel.
func (l Logger) WarnTelemetryCollectionIssue(msg string, keysAndValues ...any) {
	l.Warn(msg, append([]any{telemetryCollectionIssueMarker, true}, keysAndValues...)...)
}

// ErrorAsWarnTelemetryCollectionIssue logs an error as a telemetry collection issue at level at zapcore.WarnLevel.
func (l Logger) ErrorAsWarnTelemetryCollectionIssue(err error, msg string, keysAndValues ...any) {
	l.ErrorAsWarn(err, msg, append([]any{telemetryCollectionIssueMarker, true}, keysAndValues...)...)
}

// ErrorTelemetryCollectionIssue logs an error as a telemetry collection issue at level at zapcore.ErrorLevel.
func (l Logger) ErrorTelemetryCollectionIssue(err error, msg string, keysAndValues ...any) {
	l.ErrorAsWarn(err, msg, append([]any{telemetryCollectionIssueMarker, true}, keysAndValues...)...)
}

// WithValues returns a new Logger with additional key-value pairs.
func (l Logger) WithValues(keysAndValues ...any) Logger {
	return NewLogger(l.Logger.WithValues(keysAndValues...))
}

// WithName returns a new Logger with the specified name appended.
func (l Logger) WithName(name string) Logger {
	return NewLogger(l.Logger.WithName(name))
}

func Discard() Logger {
	return NewLogger(logr.Discard())
}
