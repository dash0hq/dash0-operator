// SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

package enablement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dash0v1alpha1 "github.com/dash0hq/dash0-operator/api/operator/v1alpha1"
	"github.com/dash0hq/dash0-operator/internal/selfmonitoringapiaccess"
	"github.com/dash0hq/dash0-operator/internal/util"
	"github.com/dash0hq/dash0-operator/internal/util/logd"
)

const (
	// signalControlEdgeSettingsPath is the Dash0 API path that reports the organization's Signal Control entitlement.
	// It is a control-plane API route that is also reachable on the regular Dash0 API host, which proxies it to the
	// control plane. The response body is {"enabled": true|false}.
	signalControlEdgeSettingsPath = "/api/signal-control/edge/settings"

	// checkTimeout bounds a single entitlement check. The shared Dash0 API HTTP client does not set a request-level
	// timeout, so this timeout prevents a hung endpoint from blocking the reconcile loop.
	checkTimeout = 10 * time.Second
)

// ErrNotConfigured indicates that the entitlement check could not be performed because the operator configuration
// resource has no Dash0 export with an API endpoint and an auth token. This is distinct from a transport/HTTP error.
var ErrNotConfigured = errors.New("no Dash0 export with API endpoint and auth token is configured")

// Checker is the read-side interface consumed by the collector manager and the Signal Control manager to gate Signal
// Control on the organization's entitlement result.
type Checker interface {
	// EnsureAllowed reports whether the organization is entitled to use Signal Control. If the result has not been
	// determined yet (Unknown), it performs a single entitlement check. Cached Allowed/NotAllowed results are
	// returned without an HTTP call.
	EnsureAllowed(ctx context.Context, operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration, logger logd.Logger) bool

	// Result returns the last cached entitlement result without performing an HTTP call.
	Result() Result
}

// EnablementChecker queries the Dash0 API for the organization's Signal Control entitlement and caches the latest
// result in memory. A single instance is shared between the Signal Control controller (which forces a fresh check
// on every reconcile via Check) and the collector manager (which reads the cached result via EnsureAllowed).
type EnablementChecker struct {
	httpClient        *http.Client
	k8sClient         client.Client
	operatorNamespace string
	result            atomic.Int32
}

func NewEnablementChecker(
	httpClient *http.Client,
	k8sClient client.Client,
	operatorNamespace string,
) *EnablementChecker {
	return &EnablementChecker{
		httpClient:        httpClient,
		k8sClient:         k8sClient,
		operatorNamespace: operatorNamespace,
	}
}

// Result returns the last cached entitlement result.
func (c *EnablementChecker) Result() Result {
	return Result(c.result.Load())
}

// Check performs the entitlement check against the Dash0 API and updates the cached result. On success it returns
// the definitive result (Allowed/NotAllowed) and stores it. On failure (missing configuration, transport error,
// non-2xx status, or unparseable response) it returns the current cached result together with an error and leaves
// the cached result unchanged, so a transient failure never tears down a previously-confirmed deployment.
func (c *EnablementChecker) Check(
	ctx context.Context,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) (Result, error) {
	enabled, err := c.fetchEnablement(ctx, operatorConfig, logger)
	if err != nil {
		return c.Result(), err
	}
	result := ResultNotAllowed
	if enabled {
		result = ResultAllowed
	}
	c.result.Store(int32(result))
	logger.Info("Signal Control entitlement check completed", "enabled", enabled)
	return result, nil
}

// EnsureAllowed implements Checker.
func (c *EnablementChecker) EnsureAllowed(
	ctx context.Context,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) bool {
	switch c.Result() {
	case ResultAllowed:
		return true
	case ResultNotAllowed:
		return false
	default:
		result, err := c.Check(ctx, operatorConfig, logger)
		if err != nil {
			logger.Warn("the Signal Control entitlement check (collector fallback) did not complete", "error", err)
			return false
		}
		return result == ResultAllowed
	}
}

func (c *EnablementChecker) fetchEnablement(
	ctx context.Context,
	operatorConfig *dash0v1alpha1.Dash0OperatorConfiguration,
	logger logd.Logger,
) (bool, error) {
	if operatorConfig == nil {
		return false, ErrNotConfigured
	}
	dash0Exports := operatorConfig.GetDash0Exports()
	if len(dash0Exports) == 0 {
		return false, ErrNotConfigured
	}
	dash0Config := dash0Exports[0]
	apiEndpoint := strings.TrimSpace(dash0Config.ApiEndpoint)
	if apiEndpoint == "" {
		return false, ErrNotConfigured
	}
	tokenPtr, err := selfmonitoringapiaccess.GetAuthTokenForDash0Export(
		ctx,
		c.k8sClient,
		c.operatorNamespace,
		dash0Config,
		logger,
	)
	if err != nil {
		return false, fmt.Errorf("cannot resolve the Dash0 auth token for the Signal Control entitlement check: %w", err)
	}
	if tokenPtr == nil || *tokenPtr == "" {
		return false, ErrNotConfigured
	}

	url := strings.TrimSuffix(apiEndpoint, "/") + signalControlEdgeSettingsPath

	timeoutCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("cannot create the Signal Control entitlement check request: %w", err)
	}
	req.Header.Set(util.AuthorizationHeaderName, util.RenderAuthorizationHeader(*tokenPtr))

	logger.Debug("checking Signal Control entitlement", "url", url)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("the Signal Control entitlement check request to %s failed: %w", url, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(res.Body)
		return false, fmt.Errorf(
			"the Signal Control entitlement check request to %s returned unexpected status code %d: %s",
			url, res.StatusCode, string(body),
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return false, fmt.Errorf("cannot read the Signal Control entitlement check response from %s: %w", url, err)
	}
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("cannot parse the Signal Control entitlement check response from %s: %w", url, err)
	}
	return payload.Enabled, nil
}
