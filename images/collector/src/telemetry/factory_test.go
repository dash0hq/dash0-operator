// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package dash0telemetry

import (
	"reflect"
	"testing"

	"go.opentelemetry.io/collector/service/telemetry"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
)

// TestFactoryForwardsEverythingTheWrappedFactorySets guards against silently dropping a capability of the wrapped
// factory. NewFactory rebuilds the factory from individual telemetry.FactoryOption values, and telemetry.NewFactory
// substitutes a noop default for every option that is not passed (an empty resource, a noop logger, noop providers).
// So when a future collector version adds an option that otelconftelemetry sets and NewFactory does not forward, the
// collector silently loses that capability: the type is sealed, the options are variadic, and nothing fails to
// compile.
//
// Both factories are the same unexported struct of function fields, one per capability, so a field that the wrapped
// factory populates and ours leaves nil is exactly that bug.
func TestFactoryForwardsEverythingTheWrappedFactorySets(t *testing.T) {
	wrapped := reflect.ValueOf(otelconftelemetry.NewFactory())
	ours := reflect.ValueOf(NewFactory())

	if wrapped.Type() != ours.Type() {
		t.Fatalf(
			"expected both factories to be the same concrete type, got %s and %s",
			wrapped.Type(),
			ours.Type(),
		)
	}

	wrappedStruct := wrapped.Elem()
	oursStruct := ours.Elem()
	factoryType := wrappedStruct.Type()

	checked := 0
	for i := range factoryType.NumField() {
		if factoryType.Field(i).Type.Kind() != reflect.Func {
			continue
		}
		checked++
		if wrappedStruct.Field(i).IsNil() {
			// otelconftelemetry does not provide this capability either, there is nothing to forward.
			continue
		}
		if oursStruct.Field(i).IsNil() {
			t.Errorf(
				"%s sets %s.%s but NewFactory does not forward it, so the collector falls back to the noop "+
					"default; add the corresponding telemetry.With... option in NewFactory",
				"otelconftelemetry",
				factoryType,
				factoryType.Field(i).Name,
			)
		}
	}

	if checked == 0 {
		t.Fatalf("expected %s to have function fields to compare, found none", factoryType)
	}
}

// TestFactoryCreatesDefaultConfigOfTheWrappedFactory pins the one capability that is forwarded as a plain function
// rather than an option, so the reflection check above cannot tell a real delegation from an unrelated non-nil value.
func TestFactoryCreatesDefaultConfigOfTheWrappedFactory(t *testing.T) {
	expected := otelconftelemetry.NewFactory().CreateDefaultConfig()
	actual := NewFactory().CreateDefaultConfig()

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected the default config of the wrapped factory %#v, got %#v", expected, actual)
	}
}

// telemetry.Factory is a sealed interface; this assignment fails to compile if NewFactory ever stops returning one.
var _ telemetry.Factory = NewFactory()
