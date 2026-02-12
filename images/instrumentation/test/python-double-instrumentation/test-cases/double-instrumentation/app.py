# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys
from opentelemetry.instrumentation import auto_instrumentation

if auto_instrumentation.__version__ != "0.59b0":
    print(f"ERROR: Unexpected opentelemetry.instrumentation.auto_instrumentation version: " + auto_instrumentation.__version__, file=sys.stderr)
    sys.exit(1)
