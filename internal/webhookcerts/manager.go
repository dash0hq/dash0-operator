// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package webhookcerts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	callTimeout        = 5 * time.Second
	maxConflictRetries = 3

	eventAction                    = "WebhookCertificates"
	reasonSecretMissing            = "WebhookCertificatesSecretMissing"
	reasonSecretUpdateFailed       = "WebhookCertificatesSecretUpdateFailed"
	reasonInjectionFailed          = "WebhookCaBundleInjectionFailed"
	reasonRotationBlocked          = "WebhookCaRotationBlocked"
	reasonRotationForced           = "WebhookCaRotationForced"
	reasonRetentionOverdue         = "WebhookCertificateRetentionOverdue"
	reasonServingCertificateExpiry = "WebhookServingCertificateExpiring"
)

// Manager maintains the operator-managed webhook certificates: it creates and renews the CA and the serving
// certificate, stores them in a Secret, loads them into a Store, and injects the CA bundle into the configured targets.
// It runs on every replica of the operator manager, without leader election.
type Manager struct {
	config   Config
	client   client.Client
	store    *Store
	now      func() time.Time
	dnsNames []string

	// passLock serializes passes; the bootstrap and the controller never run a pass concurrently.
	passLock      sync.Mutex
	remembered    *rememberedMaterial
	lastInjected  map[Target][]byte
	missingLogged map[Target]bool

	eventLock      sync.RWMutex
	eventRecorder  events.EventRecorder
	eventRegarding runtime.Object
}

// NewManager creates a manager. The client must not be backed by a cache, since every pass needs to see the current
// state of the Secret and the targets.
func NewManager(config Config, uncachedClient client.Client, store *Store) *Manager {
	return &Manager{
		config:        config,
		client:        uncachedClient,
		store:         store,
		now:           time.Now,
		dnsNames:      servingDnsNames(config.WebhookServiceName, config.OperatorNamespace),
		lastInjected:  map[Target][]byte{},
		missingLogged: map[Target]bool{},
	}
}

// Store returns the store into which the manager loads the material.
func (m *Manager) Store() *Store {
	return m.store
}

// SetEventRecorder sets the recorder for Kubernetes events, and the object the events are attached to (the operator
// manager deployment). Until it has been set, problems are only logged.
func (m *Manager) SetEventRecorder(recorder events.EventRecorder, regarding runtime.Object) {
	m.eventLock.Lock()
	defer m.eventLock.Unlock()
	m.eventRecorder = recorder
	m.eventRegarding = regarding
}

func (m *Manager) warningEvent(reason string, note string) {
	m.eventLock.RLock()
	defer m.eventLock.RUnlock()
	if m.eventRecorder == nil || m.eventRegarding == nil {
		return
	}
	m.eventRecorder.Eventf(m.eventRegarding, nil, corev1.EventTypeWarning, reason, eventAction, "%s", note)
}

// targetOutcome is the result of injecting the bundle into one target.
type targetOutcome struct {
	target Target
	// injected is true if the target carries the current bundle.
	injected bool
	// ignored is true if the target does not exist or does not carry the injection label.
	ignored   bool
	permanent bool
	err       error
}

// runPass ensures that valid material is stored and loaded, injects the bundle into all targets, completes a pending
// CA rotation if possible, and sets the readiness latch. It returns an error if anything needs to be retried.
func (m *Manager) runPass(ctx context.Context, logger logd.Logger) error {
	m.passLock.Lock()
	defer m.passLock.Unlock()

	defer m.warnAboutServingCertificateExpiry(logger)

	var current *material
	var secret *corev1.Secret
	var err error
	for attempt := 0; ; attempt++ {
		current, secret, err = m.ensureMaterial(ctx, logger)
		if err == nil {
			break
		}
		if apierrors.IsConflict(err) && attempt < maxConflictRetries {
			logger.Debug("the webhook certificates secret has been changed concurrently, reading it again")
			continue
		}
		return err
	}
	if err = m.store.load(current); err != nil {
		return err
	}

	bundle := current.bundle()
	outcomes := m.injectAll(ctx, bundle, logger)
	persesCrdCarriesBundle := m.checkPersesCrdBundle(ctx, bundle, logger)

	if err = m.completeRotationIfPossible(ctx, current, secret, outcomes, persesCrdCarriesBundle, logger); err != nil {
		return err
	}

	return m.evaluateOutcomes(outcomes, logger)
}

func (m *Manager) evaluateOutcomes(outcomes []targetOutcome, logger logd.Logger) error {
	var errs []error
	allSettled := true
	anyFound := false
	for _, outcome := range outcomes {
		if !outcome.ignored {
			anyFound = true
		}
		if outcome.err == nil {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: %w", outcome.target, outcome.err))
		if outcome.permanent {
			logger.Error(
				outcome.err,
				"Cannot inject the CA bundle into a webhook target. Requests to the webhooks of this target will fail "+
					"with a certificate error.",
				"target", outcome.target.String(),
			)
			m.warningEvent(
				reasonInjectionFailed,
				fmt.Sprintf("Cannot inject the CA bundle into %s: %v", outcome.target, outcome.err),
			)
		} else {
			allSettled = false
		}
	}

	if allSettled && !m.store.IsReady() {
		if !anyFound {
			logger.Warn(
				"None of the configured webhook targets has been found; the CA bundle will be injected as soon as they " +
					"are created.",
			)
		}
		m.store.setReady()
		logger.Info("The webhook certificates have been loaded and injected, the webhook server is ready.")
	}
	return errors.Join(errs...)
}

// ensureMaterial reads the Secret and returns the material to use. If necessary, it adopts, restores, generates,
// renews or rotates the material, and persists it. Material is only returned after it has been persisted.
func (m *Manager) ensureMaterial(ctx context.Context, logger logd.Logger) (*material, *corev1.Secret, error) {
	now := m.now()
	secret := &corev1.Secret{}
	if err := m.get(ctx, client.ObjectKey{Namespace: m.config.OperatorNamespace, Name: m.config.SecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf(
				"The secret %s/%s for the webhook certificates does not exist. It is created by the Helm chart; running "+
					"pods keep serving their current certificate, new pods will not become ready until it exists.",
				m.config.OperatorNamespace,
				m.config.SecretName,
			)
			logger.Error(err, msg)
			m.warningEvent(reasonSecretMissing, msg)
			return nil, nil, fmt.Errorf("the webhook certificates secret does not exist: %w", err)
		}
		return nil, nil, fmt.Errorf("cannot read the webhook certificates secret: %w", err)
	}

	var changes []string
	current, parseErr := materialFromSecretData(secret.Data, now)
	isNew := false
	if parseErr != nil {
		if m.remembered != nil && m.remembered.secretUid == secret.UID {
			if restored, err := materialFromSecretData(m.remembered.data, now); err == nil {
				current = restored
				changes = append(changes, "restored the material from memory, since the secret data has been "+
					"removed or damaged ("+parseErr.Error()+")")
			}
		}
		if current == nil {
			candidates := m.collectRetentionCandidates(ctx, now, logger)
			generated, err := generateMaterial(m.dnsNames, candidates, now)
			if err != nil {
				return nil, nil, err
			}
			current = generated
			isNew = true
			changes = append(changes, fmt.Sprintf(
				"generated a new CA (%s), retained %d CA(s) of older chart versions and %d other foreign "+
					"certificate(s)", parseErr.Error(), len(candidates.chartCas), len(candidates.other),
			))
		}
	}
	if !isNew {
		// After a rollback to an older chart version and another upgrade, the targets and the previous secret carry a
		// CA of the older chart version that the kept material does not contain yet.
		if added := current.retainChartCas(m.collectRetentionCandidates(ctx, now, logger).chartCas); added > 0 {
			changes = append(changes, fmt.Sprintf("retained %d CA(s) of older chart versions", added))
		}
	}

	maintenance, err := current.maintain(maintainInput{
		now:      now,
		dnsNames: m.dnsNames,
		rolloutComplete: func() bool {
			return m.isRolloutComplete(ctx, logger)
		},
	})
	if err != nil {
		return nil, nil, err
	}
	changes = append(changes, maintenance.changes...)
	if maintenance.retentionOverdue {
		msg := "Foreign certificates are still retained in the webhook CA bundle, because the rollout of the operator " +
			"deployment has not completed yet. They will be removed once the rollout is complete."
		logger.Warn(msg)
		m.warningEvent(reasonRetentionOverdue, msg)
	}

	newData := current.toSecretData()
	if dataEqual(newData, secret.Data) {
		m.remember(secret, newData)
		return current, secret, nil
	}
	updated, err := m.writeSecret(ctx, secret, newData, logger)
	if err != nil {
		return nil, nil, err
	}
	logger.Info(
		"Updated the webhook certificates secret.",
		"changes", strings.Join(changes, "; "),
		"secret", fmt.Sprintf("%s/%s", m.config.OperatorNamespace, m.config.SecretName),
	)
	return current, updated, nil
}

func (m *Manager) writeSecret(
	ctx context.Context,
	secret *corev1.Secret,
	data map[string][]byte,
	logger logd.Logger,
) (*corev1.Secret, error) {
	updated := secret.DeepCopy()
	updated.Data = data
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := m.client.Update(callCtx, updated); err != nil {
		if apierrors.IsConflict(err) {
			return nil, err
		}
		msg := fmt.Sprintf(
			"Cannot update the webhook certificates secret %s/%s. The new certificate material is not used until it "+
				"has been stored; pods that have not loaded any material will not become ready.",
			m.config.OperatorNamespace,
			m.config.SecretName,
		)
		logger.Error(err, msg)
		m.warningEvent(reasonSecretUpdateFailed, fmt.Sprintf("%s Error: %v", msg, err))
		return nil, fmt.Errorf("cannot update the webhook certificates secret: %w", err)
	}
	m.remember(updated, data)
	return updated, nil
}

func (m *Manager) remember(secret *corev1.Secret, data map[string][]byte) {
	m.remembered = &rememberedMaterial{secretUid: secret.UID, data: cloneData(data)}
}

func (m *Manager) collectRetentionCandidates(ctx context.Context, now time.Time, logger logd.Logger) retentionCandidates {
	candidates := retentionCandidates{}
	for _, target := range m.config.Targets {
		bundles, err := m.readTargetBundles(ctx, target)
		if err != nil {
			logger.ErrorAsWarn(err, "cannot read the current CA bundle of a webhook target for retention", "target", target.String())
			continue
		}
		for _, bundle := range bundles {
			candidates.add(parseCertificates(bundle), false, now)
		}
	}
	if m.config.PreviousSecretName != "" {
		previousSecret := &corev1.Secret{}
		err := m.get(ctx, client.ObjectKey{Namespace: m.config.OperatorNamespace, Name: m.config.PreviousSecretName}, previousSecret)
		if err == nil {
			candidates.add(parseCertificates(previousSecret.Data[keyCaBundle]), true, now)
		} else if !apierrors.IsNotFound(err) {
			logger.ErrorAsWarn(err, "cannot read the certificates secret of the previous chart version for retention")
		}
	}
	return candidates
}

// readTargetBundles returns the CA bundles of the target's entries that point at the operator's webhook service.
func (m *Manager) readTargetBundles(ctx context.Context, target Target) ([][]byte, error) {
	object, err := m.getTarget(ctx, target)
	if err != nil || object == nil {
		return nil, err
	}
	var bundles [][]byte
	for _, clientConfig := range m.clientConfigsOf(object) {
		bundles = append(bundles, *clientConfig.caBundle)
	}
	return bundles, nil
}

func (m *Manager) isRolloutComplete(ctx context.Context, logger logd.Logger) bool {
	deployment := &appsv1.Deployment{}
	if err := m.get(ctx, client.ObjectKey{Namespace: m.config.OperatorNamespace, Name: m.config.DeploymentName}, deployment); err != nil {
		logger.ErrorAsWarn(err, "cannot read the operator deployment to check whether its rollout is complete")
		return false
	}
	return EvaluateDeploymentRollout(deployment).Complete
}

func (m *Manager) injectAll(ctx context.Context, bundle []byte, logger logd.Logger) []targetOutcome {
	outcomes := make([]targetOutcome, 0, len(m.config.Targets))
	for _, target := range m.config.Targets {
		outcome := m.injectWithRetry(ctx, target, bundle, logger)
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func (m *Manager) injectWithRetry(ctx context.Context, target Target, bundle []byte, logger logd.Logger) targetOutcome {
	var outcome targetOutcome
	for attempt := 0; attempt <= maxConflictRetries; attempt++ {
		outcome = m.inject(ctx, target, bundle, logger)
		if outcome.err == nil || !apierrors.IsConflict(outcome.err) {
			return outcome
		}
	}
	return outcome
}

func (m *Manager) inject(ctx context.Context, target Target, bundle []byte, logger logd.Logger) targetOutcome {
	outcome := targetOutcome{target: target}
	object, err := m.getTarget(ctx, target)
	if err != nil {
		outcome.err = err
		outcome.permanent = apierrors.IsForbidden(err)
		return outcome
	}
	if object == nil {
		outcome.ignored = true
		if !m.missingLogged[target] {
			logger.Info("A webhook target does not exist or does not carry the injection label yet.", "target", target.String())
			m.missingLogged[target] = true
		}
		return outcome
	}
	m.missingLogged[target] = false

	clientConfigs := m.clientConfigsOf(object)
	if len(clientConfigs) == 0 {
		outcome.err = fmt.Errorf("no webhook client configuration of this object points at the service %s/%s",
			m.config.OperatorNamespace, m.config.WebhookServiceName)
		outcome.permanent = true
		return outcome
	}

	original := object.DeepCopyObject().(client.Object)
	changed := false
	for _, clientConfig := range clientConfigs {
		if !bytes.Equal(*clientConfig.caBundle, bundle) {
			*clientConfig.caBundle = bundle
			changed = true
		}
	}
	if !changed {
		m.lastInjected[target] = bundle
		outcome.injected = true
		return outcome
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err = m.client.Patch(callCtx, object, patchFor(original)); err != nil {
		outcome.err = err
		// An admission webhook that denies the request without a status code (for example a policy engine) causes a
		// BadRequest.
		outcome.permanent = apierrors.IsForbidden(err) || apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)
		return outcome
	}
	if lastInjected, ok := m.lastInjected[target]; ok && bytes.Equal(lastInjected, bundle) {
		logger.Warn(
			"The CA bundle of a webhook target has been changed by someone else (for example a GitOps tool applying a "+
				"stale manifest); it has been injected again.",
			"target", target.String(),
		)
	} else {
		logger.Info("Injected the CA bundle into a webhook target.", "target", target.String())
	}
	m.lastInjected[target] = bundle
	outcome.injected = true
	return outcome
}

// patchFor returns a patch with an optimistic lock, so that a stale read never overwrites newer content. Webhook
// configurations use a strategic merge patch, which only contains the changed webhook entries, keyed by name, and
// therefore keeps fields this client does not know about.
func patchFor(original client.Object) client.Patch {
	switch original.(type) {
	case *admissionregistrationv1.MutatingWebhookConfiguration, *admissionregistrationv1.ValidatingWebhookConfiguration:
		return client.StrategicMergeFrom(original, client.MergeFromWithOptimisticLock{})
	default:
		return client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})
	}
}

// getTarget reads the target. It returns nil (and no error) if the target does not exist or does not carry the
// injection label for this installation.
func (m *Manager) getTarget(ctx context.Context, target Target) (client.Object, error) {
	var object client.Object
	switch target.Kind {
	case TargetKindMutatingWebhookConfiguration:
		object = &admissionregistrationv1.MutatingWebhookConfiguration{}
	case TargetKindValidatingWebhookConfiguration:
		object = &admissionregistrationv1.ValidatingWebhookConfiguration{}
	case TargetKindCustomResourceDefinition:
		object = &apiextensionsv1.CustomResourceDefinition{}
	default:
		return nil, fmt.Errorf("unsupported target kind %s", target.Kind)
	}
	if err := m.get(ctx, client.ObjectKey{Name: target.Name}, object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if object.GetLabels()[InjectionTargetLabel] != m.config.OperatorNamespace {
		return nil, nil
	}
	return object, nil
}

// clientConfigRef points at the CA bundle field of a webhook client configuration that targets the operator's
// webhook service.
type clientConfigRef struct {
	caBundle *[]byte
}

func (m *Manager) clientConfigsOf(object client.Object) []clientConfigRef {
	var refs []clientConfigRef
	switch typed := object.(type) {
	case *admissionregistrationv1.MutatingWebhookConfiguration:
		for i := range typed.Webhooks {
			if m.isOwnService(typed.Webhooks[i].ClientConfig.Service) {
				refs = append(refs, clientConfigRef{caBundle: &typed.Webhooks[i].ClientConfig.CABundle})
			}
		}
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		for i := range typed.Webhooks {
			if m.isOwnService(typed.Webhooks[i].ClientConfig.Service) {
				refs = append(refs, clientConfigRef{caBundle: &typed.Webhooks[i].ClientConfig.CABundle})
			}
		}
	case *apiextensionsv1.CustomResourceDefinition:
		conversion := typed.Spec.Conversion
		if conversion != nil && conversion.Webhook != nil && conversion.Webhook.ClientConfig != nil &&
			conversion.Webhook.ClientConfig.Service != nil &&
			conversion.Webhook.ClientConfig.Service.Name == m.config.WebhookServiceName &&
			conversion.Webhook.ClientConfig.Service.Namespace == m.config.OperatorNamespace {
			refs = append(refs, clientConfigRef{caBundle: &conversion.Webhook.ClientConfig.CABundle})
		}
	}
	return refs
}

func (m *Manager) isOwnService(service *admissionregistrationv1.ServiceReference) bool {
	return service != nil &&
		service.Name == m.config.WebhookServiceName &&
		service.Namespace == m.config.OperatorNamespace
}

// completeRotationIfPossible switches to the pending CA once every target carries it and the rotation has been going on
// long enough, or when the active CA is about to expire.
func (m *Manager) completeRotationIfPossible(
	ctx context.Context,
	current *material,
	secret *corev1.Secret,
	outcomes []targetOutcome,
	persesCrdCarriesBundle bool,
	logger logd.Logger,
) error {
	if current.pendingCa == nil {
		return nil
	}
	now := m.now()
	var notInjected []string
	for _, outcome := range outcomes {
		if !outcome.injected && !outcome.ignored {
			notInjected = append(notInjected, outcome.target.String())
		}
	}
	if !persesCrdCarriesBundle {
		notInjected = append(notInjected, fmt.Sprintf("%s/%s", TargetKindCustomResourceDefinition, m.config.PersesCrdName))
	}

	switch current.evaluateSwitch(len(notInjected) == 0, now) {
	case noRotationPending, rotationWaiting:
		return nil
	case rotationBlocked:
		msg := fmt.Sprintf(
			"The webhook CA rotation cannot complete, because these targets do not carry the new CA yet: %s. The "+
				"rotation will be forced %s before the current CA expires (%s), which will break requests to these "+
				"targets.",
			strings.Join(notInjected, ", "),
			forcedSwitchLeadTime,
			current.activeCa.cert.NotAfter.Format(time.RFC3339),
		)
		logger.Error(fmt.Errorf("CA rotation blocked"), msg)
		m.warningEvent(reasonRotationBlocked, msg)
		return nil
	case rotationForcedSwitch:
		msg := fmt.Sprintf(
			"Forcing the webhook CA rotation, since the current CA expires at %s. These targets do not carry the new "+
				"CA, requests to them will fail with a certificate error: %s.",
			current.activeCa.cert.NotAfter.Format(time.RFC3339),
			strings.Join(notInjected, ", "),
		)
		logger.Error(fmt.Errorf("CA rotation forced"), msg)
		m.warningEvent(reasonRotationForced, msg)
	case rotationSwitch:
	}

	if err := current.switchToPendingCa(m.dnsNames, now); err != nil {
		return err
	}
	if _, err := m.writeSecret(ctx, secret, current.toSecretData(), logger); err != nil {
		// The next pass reads the secret again and repeats the decision.
		return err
	}
	logger.Info("Completed the webhook CA rotation, the new CA is active now.")
	return m.store.load(current)
}

// checkPersesCrdBundle reports whether the Perses dashboard CRD carries the bundle or does not depend on it. The CRD is
// patched by the Perses dashboard CRD reconciler, which listens to bundle changes; if the CRD carries an outdated bundle
// (for example because a patch has failed), the listeners are notified again.
func (m *Manager) checkPersesCrdBundle(ctx context.Context, bundle []byte, logger logd.Logger) bool {
	if m.config.PersesCrdName == "" {
		return true
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := m.get(ctx, client.ObjectKey{Name: m.config.PersesCrdName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return true
		}
		logger.ErrorAsWarn(err, "cannot read the Perses dashboard CRD to check its CA bundle")
		return false
	}
	refs := m.clientConfigsOf(crd)
	if len(refs) == 0 {
		// The conversion webhook does not point at the operator, so the CRD does not depend on the bundle.
		return true
	}
	if bytes.Equal(*refs[0].caBundle, bundle) {
		return true
	}
	logger.Info("The conversion webhook of the Perses dashboard CRD does not carry the current CA bundle, updating it.")
	m.store.notifyListeners()
	return false
}

func (m *Manager) warnAboutServingCertificateExpiry(logger logd.Logger) {
	leaf := m.store.servingCertificate()
	if leaf == nil {
		return
	}
	if m.now().Add(expiryWarningThreshold).After(leaf.NotAfter) {
		msg := fmt.Sprintf(
			"The webhook serving certificate expires at %s and has not been renewed. Check the previous errors.",
			leaf.NotAfter.Format(time.RFC3339),
		)
		logger.Error(fmt.Errorf("serving certificate about to expire"), msg)
		m.warningEvent(reasonServingCertificateExpiry, msg)
	}
}

func (m *Manager) get(ctx context.Context, key client.ObjectKey, object client.Object) error {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return m.client.Get(callCtx, key, object)
}
