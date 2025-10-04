# SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import os
import sys

if os.environ.get('AN_ENVIRONMENT_VARIABLE') != 'value':
    print(f"Unexpected value for AN_ENVIRONMENT_VARIABLE: {os.environ.get('AN_ENVIRONMENT_VARIABLE')}", file=sys.stderr)
    sys.exit(1)
