# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys

otel_auto_instrumentation_has_been_loaded = 'opentelemetry.instrumentation.auto_instrumentation' in sys.modules

if not otel_auto_instrumentation_has_been_loaded:
    print(f"ERROR: For Python {python_version[0]}.{python_version[1]}, the opentelemetry.instrumentation.auto_instrumentation module should be loaded, but it isn't.", file=sys.stderr)
    sys.exit(1)
