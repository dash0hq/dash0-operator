// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package logd

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Logger is a thin wrapper around logr.Logger that adds a Debug method using V(1), which maps to zapcore.DebugLevel
// (-1) via the zapr bridge (logr V level n maps to zap level -n). Debug messages are only visible when the operator
// runs in development mode, where the zap logger is configured at debug level (-1).
// In production mode, the zap logger is configured at info level (0), so V(1) messages are filtered out.
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
