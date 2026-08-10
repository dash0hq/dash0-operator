# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys
from importlib.metadata import version

dependency_name = 'opentelemetry-api'
expected_version = '1.40.0'
actual_version = version(dependency_name)
if actual_version != expected_version:
    print(f"Unexpected {dependency_name} version in {__file__}: {actual_version}", file=sys.stderr)
    sys.exit(1)

# Because of the dependency version conflict, the Python auto-instrumentation should have deactivated itself.
if 'opentelemetry.instrumentation.auto_instrumentation' in sys.modules:
    print(
        "Expected the Python auto-instrumentation to be deactivated due to a dependency version conflict, but "
        "it was loaded.",
        file=sys.stderr
    )
    sys.exit(1)
