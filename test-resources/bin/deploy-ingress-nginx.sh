#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

project_root="$(dirname "${BASH_SOURCE[0]}")"/../..
scripts_lib="test-resources/bin/lib"

cd "$project_root"

# shellcheck source=./lib/constants
source "$scripts_lib/constants"

# shellcheck source=./lib/util
source "$scripts_lib/util"

load_env_file

# The main reason for this even being a script instead of just using `kubectl apply -k test-resources/nginx` directly is
# verify_kubectx, which offers protection against accidentally making changes in production clusters.
verify_kubectx

install_nginx_ingress
