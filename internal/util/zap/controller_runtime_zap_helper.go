/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This file contains helper functions that are modelled after the controller-runtime/pkg/log/zap package, but where we
// need to access certain internals of that package. Some small snippets have been copied over from the
// controller-runtime source code, hence the different copyright notice.
//
// Copyright notice for modifications with respect to the original source:
// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package zap

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func ConvertOptions(opts []crzap.Opts) *crzap.Options {
	o := &crzap.Options{}
	for _, opt := range opts {
		opt(o)
	}
	addDefaults(o)
	return o
}

func newConsoleEncoder(opts ...crzap.EncoderConfigOption) zapcore.Encoder {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	for _, opt := range opts {
		opt(&encoderConfig)
	}
	return zapcore.NewConsoleEncoder(encoderConfig)
}

func newJSONEncoder(opts ...crzap.EncoderConfigOption) zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	for _, opt := range opts {
		opt(&encoderConfig)
	}
	return zapcore.NewJSONEncoder(encoderConfig)
}

// addDefaults adds defaults to the Options.
func addDefaults(o *crzap.Options) {
	if o.DestWriter == nil {
		o.DestWriter = os.Stderr
	}

	if o.Development {
		if o.NewEncoder == nil {
			o.NewEncoder = newConsoleEncoder
		}
		if o.Level == nil {
			lvl := zap.NewAtomicLevelAt(zap.DebugLevel)
			o.Level = &lvl
		}
		if o.StacktraceLevel == nil {
			lvl := zap.NewAtomicLevelAt(zap.WarnLevel)
			o.StacktraceLevel = &lvl
		}
		o.ZapOpts = append(o.ZapOpts, zap.Development())
	} else {
		if o.NewEncoder == nil {
			o.NewEncoder = newJSONEncoder
		}
		if o.Level == nil {
			lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
			o.Level = &lvl
		}
		if o.StacktraceLevel == nil {
			lvl := zap.NewAtomicLevelAt(zap.ErrorLevel)
			o.StacktraceLevel = &lvl
		}
		// Disable sampling for increased Debug levels. Otherwise, this will
		// cause index out of bounds errors in the sampling code.
		if !o.Level.Enabled(zapcore.Level(-2)) {
			o.ZapOpts = append(o.ZapOpts,
				zap.WrapCore(func(core zapcore.Core) zapcore.Core {
					return zapcore.NewSamplerWithOptions(core, time.Second, 100, 100)
				}))
		}
	}

	if o.TimeEncoder == nil {
		o.TimeEncoder = zapcore.RFC3339TimeEncoder
	}
	f := func(ecfg *zapcore.EncoderConfig) {
		ecfg.EncodeTime = o.TimeEncoder
	}
	// prepend instead of append it in case someone adds a time encoder option in it
	o.EncoderConfigOptions = append([]crzap.EncoderConfigOption{f}, o.EncoderConfigOptions...)

	if o.Encoder == nil {
		o.Encoder = o.NewEncoder(o.EncoderConfigOptions...)
	}
	o.ZapOpts = append(o.ZapOpts, zap.AddStacktrace(o.StacktraceLevel))
}

func NewRawFromCore(o *crzap.Options, zapCoreLogger zapcore.Core) *zap.Logger {
	log := zap.New(zapCoreLogger)
	log = log.WithOptions(o.ZapOpts...)
	return log
}
