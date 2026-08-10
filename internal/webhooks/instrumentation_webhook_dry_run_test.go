// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dash0v1beta1 "github.com/dash0hq/dash0-operator/api/operator/v1beta1"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/cluster"
	testutil "github.com/dash0hq/dash0-operator/test/util"
)

func TestInstrumentationWebhookDryRunReturnsPatchWithoutRecordingEvent(t *testing.T) {
	testScheme := runtime.NewScheme()
	if err := dash0v1beta1.AddToScheme(testScheme); err != nil {
		t.Fatal(err)
	}

	monitoringResource := testutil.DefaultMonitoringResource()
	monitoringResource.EnsureResourceIsMarkedAsAvailable()
	recorder := events.NewFakeRecorder(1)
	handler := NewInstrumentationWebhookHandler(
		fake.NewClientBuilder().WithScheme(testScheme).WithObjects(monitoringResource).Build(),
		recorder,
		util.NewClusterInstrumentationConfig(
			testutil.TestImages,
			testutil.PossibleCollectorUrlsTest,
			testutil.OTelCollectorNodeLocalBaseUrlTest,
			util.ExtraConfigDefaults,
			cluster.ResolvedInstrumentationDeliveryInitContainer,
			nil,
			false,
			false,
			false,
		),
	)

	deployment := testutil.BasicDeployment(testutil.TestNamespaceName, "dry-run-deployment")
	rawDeployment, err := json.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	dryRun := true
	response := handler.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		DryRun:    &dryRun,
		Kind: metav1.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		},
		Namespace: testutil.TestNamespaceName,
		Name:      deployment.Name,
		Object:    runtime.RawExtension{Raw: rawDeployment},
	}})

	if !response.Allowed {
		t.Fatalf("expected dry-run admission to be allowed: %v", response.Result)
	}
	if len(response.Patches) == 0 {
		t.Fatal("expected dry-run admission to return instrumentation patch")
	}
	select {
	case event := <-recorder.Events:
		t.Fatalf("expected dry-run admission to record no event, got %q", event)
	default:
	}
}
