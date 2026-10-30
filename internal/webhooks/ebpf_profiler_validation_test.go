// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0common "github.com/dash0hq/dash0-operator/api/operator/common"
	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/util"
)

func TestEbpfProfilerAdmission(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := dash0v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	enabledSpec := func() dash0v1alpha1.Dash0OperatorConfigurationSpec {
		return dash0v1alpha1.Dash0OperatorConfigurationSpec{
			SelfMonitoring: dash0v1alpha1.SelfMonitoring{Enabled: new(false)},
			Exports: []dash0common.Export{{Dash0: &dash0common.Dash0Configuration{
				Endpoint: "ingress.example.com:4317",
			}}},
			Profiling: &dash0v1alpha1.Profiling{
				Enabled:      new(true),
				EbpfProfiler: &dash0v1alpha1.EbpfProfiler{Enabled: new(true)},
			},
		}
	}
	for _, tc := range []struct {
		name          string
		profilerHelm  bool
		telemetryHelm bool
		modify        func(*dash0v1alpha1.Dash0OperatorConfigurationSpec)
		oldEnabled    *bool
		invalidOld    bool
		wantMessage   string
	}{
		{name: "explicit opt-in", profilerHelm: true, telemetryHelm: true},
		{name: "missing profiler configuration", telemetryHelm: true, modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.Profiling.EbpfProfiler = nil
		}},
		{name: "missing profiler enabled defaults to false", telemetryHelm: true, modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.Profiling.EbpfProfiler.Enabled = nil
		}},
		{name: "explicitly disabled", modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.Profiling.EbpfProfiler.Enabled = new(false)
			s.Profiling.Enabled = new(false)
		}},
		{name: "Helm disabled create", telemetryHelm: true, wantMessage: ErrorMessageEbpfProfilerDisabledViaHelm},
		{name: "Helm disabled enabling update", telemetryHelm: true, oldEnabled: new(false), wantMessage: ErrorMessageEbpfProfilerDisabledViaHelm},
		{name: "Helm disabled unrelated update", telemetryHelm: true, oldEnabled: new(true)},
		{name: "malformed old object", telemetryHelm: true, invalidOld: true, wantMessage: "could not decode OldObject for the eBPF profiler validation"},
		{name: "profiling disabled", profilerHelm: true, telemetryHelm: true, modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.Profiling.Enabled = new(false)
		}, wantMessage: ErrorMessageEbpfProfilerRequiresProfiling},
		{name: "profiling omitted", profilerHelm: true, telemetryHelm: true, modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.Profiling.Enabled = nil
		}, wantMessage: ErrorMessageEbpfProfilerRequiresProfiling},
		{name: "telemetry disabled via CR", profilerHelm: true, telemetryHelm: true, modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.TelemetryCollection.Enabled = new(false)
		}, wantMessage: ErrorMessageEbpfProfilerRequiresTelemetry},
		{name: "telemetry disabled via Helm", profilerHelm: true, wantMessage: ErrorMessageEbpfProfilerRequiresTelemetry},
		{name: "disabling after Helm disabled", telemetryHelm: true, oldEnabled: new(true), modify: func(s *dash0v1alpha1.Dash0OperatorConfigurationSpec) {
			s.Profiling.EbpfProfiler.Enabled = new(false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := enabledSpec()
			if tc.modify != nil {
				tc.modify(&spec)
			}
			handler := NewOperatorConfigurationValidationWebhookHandler(
				fake.NewClientBuilder().WithScheme(scheme).Build(), tc.telemetryHelm, false, false,
				util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			handler.EbpfProfilerEnabledViaHelm = tc.profilerHelm
			request := ebpfProfilerAdmissionRequest(t, spec)
			if tc.oldEnabled != nil {
				request.Operation = admissionv1.Update
				oldSpec := enabledSpec()
				oldSpec.Profiling.EbpfProfiler.Enabled = tc.oldEnabled
				request.OldObject = ebpfProfilerAdmissionRequest(t, oldSpec).Object
			}
			if tc.invalidOld {
				request.Operation = admissionv1.Update
				request.OldObject.Raw = []byte("invalid JSON")
			}
			response := handler.Handle(context.Background(), request)
			if tc.wantMessage == "" {
				if !response.Allowed {
					t.Fatalf("expected admission to allow request: %v", response.Result)
				}
			} else if response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, tc.wantMessage) {
				t.Fatalf("expected denial containing %q, got allowed=%t, result=%v", tc.wantMessage, response.Allowed, response.Result)
			}
		})
	}

	for _, tc := range []struct {
		name         string
		dash0Exports int
		otherExports int
		profilerHelm bool
		enabled      bool
		debuginfo    *dash0v1alpha1.EbpfProfilerDebuginfo
		wantAllowed  bool
	}{
		{name: "64 Dash0 exports", dash0Exports: 64, profilerHelm: true, enabled: true, wantAllowed: true},
		{name: "65 Dash0 exports defaults uploads on", dash0Exports: 65, profilerHelm: true, enabled: true},
		{name: "65 Dash0 exports empty debuginfo defaults uploads on", dash0Exports: 65, profilerHelm: true, enabled: true,
			debuginfo: &dash0v1alpha1.EbpfProfilerDebuginfo{}},
		{name: "explicit uploads enabled", dash0Exports: 65, profilerHelm: true, enabled: true,
			debuginfo: &dash0v1alpha1.EbpfProfilerDebuginfo{UploadEnabled: new(true)}},
		{name: "uploads disabled", dash0Exports: 65, profilerHelm: true, enabled: true, wantAllowed: true,
			debuginfo: &dash0v1alpha1.EbpfProfilerDebuginfo{UploadEnabled: new(false)}},
		{name: "profiler disabled", dash0Exports: 65, profilerHelm: true, wantAllowed: true},
		{name: "Helm disabled unrelated update", dash0Exports: 65, enabled: true, wantAllowed: true},
		{name: "only Dash0 exports count", dash0Exports: 64, otherExports: 65, profilerHelm: true, enabled: true, wantAllowed: true},
		{name: "OTLP exports without Dash0", otherExports: 65, profilerHelm: true, enabled: true, wantAllowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := enabledSpec()
			spec.Profiling.EbpfProfiler.Enabled = new(tc.enabled)
			spec.Profiling.EbpfProfiler.Debuginfo = tc.debuginfo
			spec.Exports = nil
			for range tc.dash0Exports {
				spec.Exports = append(spec.Exports, enabledSpec().Exports[0])
			}
			for range tc.otherExports {
				spec.Exports = append(spec.Exports, dash0common.Export{Grpc: &dash0common.GrpcConfiguration{Endpoint: "ingress.example.com:4317"}})
			}
			handler := NewOperatorConfigurationValidationWebhookHandler(
				fake.NewClientBuilder().WithScheme(scheme).Build(), true, false, false,
				util.ExtraConfigDefaults, util.ExtraConfigDefaults)
			handler.EbpfProfilerEnabledViaHelm = tc.profilerHelm
			request := ebpfProfilerAdmissionRequest(t, spec)
			if !tc.profilerHelm && tc.enabled {
				request.Operation = admissionv1.Update
				request.OldObject = request.Object
			}
			response := handler.Handle(context.Background(), request)
			if response.Allowed != tc.wantAllowed {
				t.Fatalf("expected allowed=%t, got allowed=%t, result=%v", tc.wantAllowed, response.Allowed, response.Result)
			}
			if !tc.wantAllowed && (response.Result == nil || response.Result.Message != ErrorMessageEbpfProfilerTooManyDash0Exports) {
				t.Fatalf("expected export limit denial, got %v", response.Result)
			}
		})
	}
}

func ebpfProfilerAdmissionRequest(t *testing.T, spec dash0v1alpha1.Dash0OperatorConfigurationSpec) admission.Request {
	t.Helper()
	raw, err := json.Marshal(dash0v1alpha1.Dash0OperatorConfiguration{
		TypeMeta: metav1.TypeMeta{APIVersion: "operator.dash0.com/v1alpha1", Kind: "Dash0OperatorConfiguration"},
		Spec:     spec,
	})
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: raw},
	}}
}
