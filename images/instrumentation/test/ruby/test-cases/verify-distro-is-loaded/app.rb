# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# The distribution is required via RUBYOPT before this file runs, so an initialized SDK tracer provider means it booted.
# On an unsupported Ruby the distribution stands down before requiring anything from OpenTelemetry, hence the check for
# the SDK constant rather than for the distribution's own module (which its version file defines either way).
distribution_has_been_loaded =
  defined?(OpenTelemetry::SDK) &&
  OpenTelemetry.tracer_provider.is_a?(OpenTelemetry::SDK::Trace::TracerProvider)

minimum_supported_ruby_version = Gem::Version.new('3.3.0')

if Gem::Version.new(RUBY_VERSION) < minimum_supported_ruby_version
  if distribution_has_been_loaded
    warn "ERROR: For Ruby #{RUBY_VERSION}, the Dash0 OpenTelemetry distribution should not be active, but it is."
    exit 1
  end
elsif !distribution_has_been_loaded
  warn "ERROR: For Ruby #{RUBY_VERSION}, the Dash0 OpenTelemetry distribution should be active, but it is not."
  exit 1
end
