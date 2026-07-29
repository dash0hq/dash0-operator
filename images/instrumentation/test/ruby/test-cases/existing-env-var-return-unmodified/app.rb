# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

if ENV['AN_ENVIRONMENT_VARIABLE'] != 'value'
  warn "Unexpected value for AN_ENVIRONMENT_VARIABLE: #{ENV['AN_ENVIRONMENT_VARIABLE']}"
  exit 1
end
