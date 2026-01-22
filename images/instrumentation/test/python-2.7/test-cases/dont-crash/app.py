# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys

try:
    import pkg_resources
except ImportError:
    print >> sys.stderr, "pkg_resources module not found"
    sys.exit(1)

try:
    pkg_resources.get_distribution('opentelemetry-distro')
    # If we reach here, the package is loaded, which is unexpected, as usercustomize.py should have prevented that.
    print >> sys.stderr, "error: opentelemetry-distro has been loaded in Python 2.7, although it should not have been loaded"
    sys.exit(1)
except pkg_resources.DistributionNotFound:
    # Package is not loaded - this is the expected behavior
    sys.exit(0)