# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

env_var_name = 'OTEL_RESOURCE_ATTRIBUTES'
expected_value = nil

if ENV[env_var_name] != expected_value
  warn "Unexpected value for #{env_var_name}: expected: '#{expected_value}'; actual: '#{ENV[env_var_name]}'"
  exit 1
end
