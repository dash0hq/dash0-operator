# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys
from importlib.metadata import version

# The Dash0 instrumentation requires a recent urllib3 (pinned to <2.7.1, which resolves to a 2.7.x release). This
# application pins an older, conflicting urllib3 version. The Dash0 auto-instrumentation must detect this dependency
# version conflict and deactivate itself.

expected_urllib3_version = '1.26.20'
actual_urllib3_version = version('urllib3')
if actual_urllib3_version != expected_urllib3_version:
    print(f"Unexpected urllib3 version in {__file__}: {actual_urllib3_version}", file=sys.stderr)
    sys.exit(1)

# Because of the dependency version conflict, the Dash0 auto-instrumentation must have deactivated itself and must not
# have loaded the OpenTelemetry auto-instrumentation.
if 'opentelemetry.instrumentation.auto_instrumentation' in sys.modules:
    print(
        "Expected the OpenTelemetry auto-instrumentation to be deactivated due to a dependency version conflict, but "
        "it was loaded.",
        file=sys.stderr
    )
    sys.exit(1)
