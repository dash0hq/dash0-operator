# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys

if sys.version_info < (3, 10):
    # The distribution is not loaded on Python versions below 3.10, see verify-distro-is-loaded.
    sys.exit(0)

from opentelemetry import trace
from opentelemetry.propagate import inject

carrier = {}
with trace.get_tracer(__name__).start_as_current_span("test"):
    inject(carrier)

expected_present = ["traceparent"]
expected_absent = ["X-Amzn-Trace-Id"]
for header in expected_present:
    if header not in carrier:
        print(f"ERROR: expected the header {header} to be injected, injected headers: {sorted(carrier)}", file=sys.stderr)
        sys.exit(1)
for header in expected_absent:
    if header in carrier:
        print(f"ERROR: expected the header {header} not to be injected, injected headers: {sorted(carrier)}", file=sys.stderr)
        sys.exit(1)
