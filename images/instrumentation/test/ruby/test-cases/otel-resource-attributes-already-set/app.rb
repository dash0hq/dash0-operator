# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

env_var_name = 'OTEL_RESOURCE_ATTRIBUTES'
expected_value = 'key1=value1,key2=value2,k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name'

if ENV[env_var_name] != expected_value
  warn "Unexpected value for #{env_var_name}: expected: '#{expected_value}'; actual: '#{ENV[env_var_name]}'"
  exit 1
end
