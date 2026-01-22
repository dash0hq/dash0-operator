# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys

otel_auto_instrumentation_has_been_loaded = 'opentelemetry.instrumentation.auto_instrumentation' in sys.modules

python_version = sys.version_info
if python_version[0] < 3 or (python_version[0] == 3 and python_version[1] < 9):
    # For Python 3.8 and below, we expect the module NOT to be loaded
    if otel_auto_instrumentation_has_been_loaded:
        print(f"ERROR: For Python {python_version[0]}.{python_version[1]}, the opentelemetry.instrumentation.auto_instrumentation should NOT be loaded, but it is.", file=sys.stderr)
        sys.exit(1)
else:
    # For Python 3.9+, we expect the module to be loaded
    if not otel_auto_instrumentation_has_been_loaded:
        print(f"ERROR: For Python {python_version[0]}.{python_version[1]}, the opentelemetry.instrumentation.auto_instrumentation module should be loaded, but it isn't.", file=sys.stderr)
        sys.exit(1)
