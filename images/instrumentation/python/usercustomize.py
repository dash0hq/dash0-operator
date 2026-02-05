# Trigger the load of the OpenTelemetry distribution for Python. This is enabled by prepending a directory with this
# script to the PYTHONPATH environment variable via the OpenTelemetry injector.

# IMPORTANT: This file must be valid Python 2.7+

from __future__ import print_function
import os
from os.path import dirname
import sys
from sys import path, version, version_info, stderr

debug_enabled = os.environ.get('OTEL_INJECTOR_LOG_LEVEL') == 'debug'

def _print_to_stderr(message):
    message = "[dash0] " + message
    print(message, file=stderr)

def _print_debug_msg(message):
    if debug_enabled:
        _print_to_stderr(message)

_print_debug_msg("running usercustomize.py")

def _print_cannot_auto_instrument_message(reason):
    if hasattr(sys, 'argv'):
        # If sys.argv is available, add the full command line (' '.join(sys.argv)) to the log message, so users know
        # which Python process this is about.
        _print_to_stderr("warning: cannot auto-instrument Python process: {} [{}]".format(reason, ' '.join(sys.argv)))
    else:
        _print_to_stderr("warning: cannot auto-instrument Python process: {}".format(reason))

def _read_all_dependencies():
    """Read all flattened dependencies from all-dependencies.txt. Returns list of requirement strings or None on error."""
    dependencies_file = os.path.join(dirname(__file__), 'all-dependencies.txt')
    requirements_to_check = []
    try:
        with open(dependencies_file, 'r') as f:
            for line in f:
                line = line.strip()
                # Skip empty lines and comments
                if not line or line.startswith('#'):
                    continue
                requirements_to_check.append(line)
        return requirements_to_check
    except (IOError, OSError):
        return None

def _check_dependency_version_conflict(req_string, version_conflicts):
    """Check for dependency version conflict for a given requirement.

    Args:
        req_string: Requirement string (e.g., "package-name >=1.0.0")
        version_conflicts: Dictionary to accumulate version conflicts (modified in place)
    """
    import importlib.metadata
    from packaging.requirements import Requirement
    from packaging.version import Version

    _print_debug_msg("_check_dependency_version_conflict({})".format(req_string))
    req = Requirement(req_string)

    # Skip extras/markers for simplicity in conflict detection
    if req.marker and not req.marker.evaluate():
        return

    try:
        installed_distribution = importlib.metadata.distribution(req.name)
        installed_version = Version(installed_distribution.version)
        _print_debug_msg("installed_version: {}".format(installed_version))

        # Check if installed version satisfies the requirement
        if req.specifier and installed_version not in req.specifier:
            _print_debug_msg("adding version conflict for {}".format(req.name))
            version_conflicts[req.name] = {
                'version_required': str(req.specifier),
                'version_found': str(installed_version),
            }
    except importlib.metadata.PackageNotFoundError:
        _print_debug_msg("adding version error for {}".format(req.name))
        version_conflicts[req.name] = {'error': 'required package not found'}

def import_distro():
    _print_debug_msg("import_distro")
    current_site = dirname(__file__)

    # We cannot use `sys.version_info.major` or other named attributes, as they only got introduced only in Python 3.1.
    if version_info[0] != 3 or version_info[1] < 9:
        path.remove(current_site)
        _print_cannot_auto_instrument_message("unsupported Python version: {}".format(version))
        return
    _print_debug_msg("found eligible Python version: {}".format(version_info))

    otlp_protocol = os.environ.get('OTEL_EXPORTER_OTLP_PROTOCOL')
    if otlp_protocol is None:
        # If OTEL_EXPORTER_OTLP_PROTOCOL is not set, opentelemetry-distro defaults to the grpc export, but we do not
        # include the package for that exporter, this would lead to
        # "RuntimeError: Requested component 'otlp_proto_grpc' not found in entry point 'opentelemetry_traces_exporter'"
        path.remove(current_site)
        _print_cannot_auto_instrument_message(
            "OTEL_EXPORTER_OTLP_PROTOCOL is not set. (The container likely has OTEL_EXPORTER_OTLP_ENDPOINT set, which "+
            "prevented the Dash0 operator from setting its own values for OTEL_EXPORTER_OTLP_ENDPOINT/PROTOCOL, "+
            "please remove OTEL_EXPORTER_OTLP_ENDPOINT from the container if you want to use Python "+
            "auto-instrumentation.)"
        )
        return
    if otlp_protocol == 'grpc':
        # If OTEL_EXPORTER_OTLP_PROTOCOL is set to grpc explicitly, we need to stand down for the same reason, we do not
        # include the package for that exporter, this would lead to
        # "RuntimeError: Requested component 'otlp_proto_grpc' not found in entry point 'opentelemetry_traces_exporter'"
        path.remove(current_site)
        _print_cannot_auto_instrument_message(
            "OTEL_EXPORTER_OTLP_PROTOCOL=grpc is not supported. (The container has OTEL_EXPORTER_OTLP_PROTOCOL set "+
            "which prevented the Dash0 operator from setting its own values for OTEL_EXPORTER_OTLP_ENDPOINT/PROTOCOL, "+
            "please remove OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_PROTOCOL from the container if you want "+
            "to use Python auto-instrumentation.)"
        )
        return
    _print_debug_msg("found eligible OTEL_EXPORTER_OTLP_PROTOCOL value: {}".format(otlp_protocol))

    # Reorder sys.path to put this site last and evaluate potential conflicts. We will leave this reordering in effect
    # when importing and initializing opentelemetry.instrumentation. Since we ruled out dependency conflicts, the order
    # should not matter, but with this re-ordering we make sure the application's package versions will win over the
    # package versions we bring (in case there are overlapping dependencies).
    path.remove(current_site)
    path.append(current_site)

    version_conflicts = {}
    requirements_to_check = _read_all_dependencies()
    if requirements_to_check is None:
        path.remove(current_site)
        _print_cannot_auto_instrument_message("cannot read all-dependencies.txt for dependency conflict checking")
        return

    for req_string in requirements_to_check:
        _check_dependency_version_conflict(req_string, version_conflicts)
        if version_conflicts:
            break

    if not version_conflicts:
        try:
            _print_debug_msg("importing and initializing the Python auto-instrumentation now")
            from opentelemetry.instrumentation import auto_instrumentation
            auto_instrumentation.initialize()
        except Exception as e:
            path.remove(current_site)
            _print_cannot_auto_instrument_message("error when importing/initializing the Python OpenTelemtry auto-instrumentation: {}: {}".format(type(e).__name__, e))
    else:
        # Remove this site for good, we do not want to trigger dependency conflict issues.
        path.remove(current_site)
        _print_cannot_auto_instrument_message("dependency conflicts: {}".format(version_conflicts))

import_distro()
