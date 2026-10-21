// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"context"
	"fmt"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	bootstrapDeadline = 15 * time.Second
	resyncInterval    = time.Hour
	minRetryDelay     = time.Second
	maxRetryDelay     = 5 * time.Minute
	controllerName    = "dash0-webhook-certificates"
)

// singleRequest is the only request the controller works on; every event leads to a full pass.
var singleRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: controllerName}}

// Bootstrap runs one pass with a hard deadline. It is meant to be called once at startup, before the controller-runtime
// manager is started, so that the material is available for the webhook server and for components that need the CA
// bundle during their setup. If the deadline is exceeded or the pass fails, the controller started by StartController
// completes the work later.
func (m *Manager) Bootstrap(ctx context.Context, logger logd.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, bootstrapDeadline)
	defer cancel()
	return m.runPass(ctx, logger)
}

// StartController starts a controller in a separate goroutine that runs a pass whenever the Secret or one of the
// labelled targets changes, every resyncInterval, and with an exponential backoff (capped at maxRetryDelay) after
// errors. It uses its own cache that only holds the Secret and the labelled targets, and it is independent of the
// controller-runtime manager and of leader election. Errors never terminate the process; the controller is restarted
// after a delay instead.
func (m *Manager) StartController(ctx context.Context, restConfig *rest.Config, scheme *runtime.Scheme, logger logd.Logger) {
	go func() {
		delay := minRetryDelay
		for {
			startedAt := time.Now()
			err := m.runController(ctx, restConfig, scheme, logger)
			if ctx.Err() != nil {
				return
			}
			if time.Since(startedAt) > maxRetryDelay {
				delay = minRetryDelay
			}
			logger.Error(err, "The webhook certificates controller has stopped unexpectedly, restarting it.", "delay", delay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			delay = min(delay*2, maxRetryDelay)
		}
	}()
}

func (m *Manager) runController(ctx context.Context, restConfig *rest.Config, scheme *runtime.Scheme, logger logd.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	targetSelector := labels.SelectorFromSet(labels.Set{InjectionTargetLabel: m.config.OperatorNamespace})
	targetCache, err := cache.New(restConfig, cache.Options{
		Scheme: scheme,
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}: {
				Namespaces: map[string]cache.Config{m.config.OperatorNamespace: {}},
				Field:      fields.OneTermEqualSelector("metadata.name", m.config.SecretName),
			},
			&admissionregistrationv1.MutatingWebhookConfiguration{}:   {Label: targetSelector},
			&admissionregistrationv1.ValidatingWebhookConfiguration{}: {Label: targetSelector},
			&apiextensionsv1.CustomResourceDefinition{}:               {Label: targetSelector},
		},
	})
	if err != nil {
		return fmt.Errorf("cannot create the cache: %w", err)
	}
	cacheErrors := make(chan error, 1)
	go func() {
		cacheErrors <- targetCache.Start(ctx)
	}()

	ctrl, err := controller.NewTypedUnmanaged(controllerName, controller.TypedOptions[reconcile.Request]{
		Reconciler: reconcile.Func(func(reconcileCtx context.Context, _ reconcile.Request) (reconcile.Result, error) {
			if err := m.runPass(reconcileCtx, logger); err != nil {
				return reconcile.Result{}, err
			}
			return reconcile.Result{RequeueAfter: resyncInterval}, nil
		}),
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](minRetryDelay, maxRetryDelay),
		// The controller is recreated after an unexpected stop, but controller-runtime keeps the names of stopped
		// controllers.
		SkipNameValidation: new(true),
		Logger:             logger.Logger,
	})
	if err != nil {
		return fmt.Errorf("cannot create the controller: %w", err)
	}

	sources := []source.TypedSource[reconcile.Request]{
		source.TypedFunc[reconcile.Request](
			func(_ context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
				queue.Add(singleRequest)
				return nil
			},
		),
		watchFor(targetCache, &corev1.Secret{}),
		watchFor(targetCache, &admissionregistrationv1.MutatingWebhookConfiguration{}),
		watchFor(targetCache, &admissionregistrationv1.ValidatingWebhookConfiguration{}),
		watchFor(targetCache, &apiextensionsv1.CustomResourceDefinition{}),
	}
	for _, s := range sources {
		if err = ctrl.Watch(s); err != nil {
			return fmt.Errorf("cannot set up a watch: %w", err)
		}
	}

	controllerErrors := make(chan error, 1)
	go func() {
		controllerErrors <- ctrl.Start(ctx)
	}()
	select {
	case err = <-cacheErrors:
		return fmt.Errorf("the cache has stopped: %w", err)
	case err = <-controllerErrors:
		return fmt.Errorf("the controller has stopped: %w", err)
	}
}

func watchFor[object client.Object](targetCache cache.Cache, obj object) source.TypedSource[reconcile.Request] {
	return source.TypedKind(
		targetCache,
		obj,
		handler.TypedEnqueueRequestsFromMapFunc(
			func(context.Context, object) []reconcile.Request {
				return []reconcile.Request{singleRequest}
			},
		),
	)
}
