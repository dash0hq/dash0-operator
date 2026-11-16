# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys
from importlib.metadata import version
from pathlib import Path

dependency_name = 'opentelemetry-api'


def read_expected_version():
    requirements_file = Path(__file__).parent / 'requirements.txt'
    for line in requirements_file.read_text().splitlines():
        requirement = line.split('#', 1)[0].strip()
        name, separator, pinned_version = requirement.partition('==')
        if separator and name.strip() == dependency_name:
            return pinned_version.strip()
    print(f"No pinned {dependency_name} version (\"{dependency_name}==x.y.z\") in {requirements_file}", file=sys.stderr)
    sys.exit(1)


expected_version = read_expected_version()
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
