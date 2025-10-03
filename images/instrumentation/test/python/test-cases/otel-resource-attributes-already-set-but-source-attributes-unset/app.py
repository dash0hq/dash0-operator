# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import os
import sys

env_var_name = 'OTEL_RESOURCE_ATTRIBUTES'
expected_value = 'key1=value1,key2=value2'

if os.environ.get(env_var_name) != expected_value:
    print(
        f"Unexpected value for {env_var_name}: expected: '{expected_value}'; actual: '{os.environ.get(env_var_name)}'",
        file=sys.stderr
    )
    sys.exit(1)
