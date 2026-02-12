# Trigger the load of the OpenTelemetry distribution for Python. This is enabled by prepending a directory with this
# script to the PYTHONPATH environment variable via the OpenTelemetry injector.

# IMPORTANT: This file must be valid Python 2.7+.

from __future__ import print_function
import os
from os.path import dirname
import sys
from sys import path, version, version_info, stderr

# Note: We might need to tweak the list of offending opentelemetry-* packages.
double_instrumentation_check_packages = [
    "opentelemetry-api",
    "opentelemetry-distro",
    "opentelemetry-exporter-otlp",
    "opentelemetry-exporter-otlp-proto-common",
    "opentelemetry-exporter-otlp-proto-grpc",
    "opentelemetry-exporter-otlp-proto-http",
    "opentelemetry-exporter-otlp-proto-http",
    "opentelemetry-exporter-prometheus",
    "opentelemetry-instrumentation",
    "opentelemetry-sdk",
    "opentelemetry-proto",
]

debug_enabled = os.environ.get("OTEL_INJECTOR_LOG_LEVEL") == "debug"


def _print_to_stderr(message):
    message = "[dash0] " + message
    print(message, file=stderr)


def _print_debug_msg(message):
    if debug_enabled:
        _print_to_stderr(message)


_print_debug_msg("running usercustomize.py")
_print_debug_msg("PYTHONPATH: {}".format(os.environ.get("PYTHONPATH")))


def _print_cannot_auto_instrument_message(reason):
    if hasattr(sys, "argv"):
        # If sys.argv is available, add the full command line (" ".join(sys.argv)) to the log message, so users know
        # which Python process this is about.
        _print_to_stderr("warning: cannot auto-instrument Python process: {} [{}]".format(reason, " ".join(sys.argv)))
    else:
        _print_to_stderr("warning: cannot auto-instrument Python process: {}".format(reason))


def _self_deactivate(current_site):
    # Starting child processes is quite common in Python (e.g. gunicorn etc.), and in particular, the OpenTelemetry
    # instrumentation wrapper (e.g. opentelemetry-instrument python app.py, see
    # https://opentelemetry.io/docs/zero-code/python) does this. When self-deactivating, we need to make sure that we
    # do not only self-deactivate for the current process, but also directly deactivate the OpenTelemetry injector's
    # auto-instrumentation for Python for child processes.
    # Failing to do so, in particular when the application is already instrumented with the opentelemetry-instrument
    # wrapper, might crash the child process in case of conflicting opentelemetry-* dependency versions. The reason is
    # that the child process started by opentelemetry-instrument will run e.g.
    # https://github.com/open-telemetry/opentelemetry-python-contrib/blob/v0.53b0/opentelemetry-instrumentation/src/opentelemetry/instrumentation/auto_instrumentation/_load.py
    # in the version brought in by the application, but _load.py then loads other opentelemetry-* dependencies from
    # /__otel_auto_instrumentation/agents/python/glibc/opentelemetry/instrumentation/auto_instrumentation/_load.py,
    # that is, from the packages that we provide. This happens _before_ this usercustomize.py script runs in the child
    # process. Hence, we cannot rely on usercustomize.py to self-deactivate in the child process, but must enforce
    # self-deactivation via environment variables.

    # Remove this site from PYTHONPATH so child processes do not attempt to load packages from us.
    current_pythonpath = os.environ.get("PYTHONPATH", "")
    pythonpath_entries = [entry for entry in current_pythonpath.split(",") if entry != current_site]
    new_pythonpath = ",".join(pythonpath_entries)
    _print_debug_msg("setting PYTHONPATH in _self_deactivate: \"{}\"".format(new_pythonpath))
    os.environ["PYTHONPATH"] = new_pythonpath

    # The OpenTelemetry injector will also run for child processes, and it would bring back the PYTHONPATH modification
    # which we have just removed. Instruct it to not do that by disabling Python auto-instrumentation.
    _print_debug_msg("clearing PYTHON_AUTO_INSTRUMENTATION_AGENT_PATH_PREFIX in _self_deactivate")
    os.environ["PYTHON_AUTO_INSTRUMENTATION_AGENT_PATH_PREFIX"] = ""

    if current_site in path:
        # Remove this site from the _current_ Python process, so our packages do not interfere with the application's
        # dependencies.
        path.remove(current_site)


def _check_for_double_instrumentation(current_site):
    import importlib.metadata
    offending_packages = []
    for dist in importlib.metadata.distributions():
        name = dist.metadata["Name"]
        if name is not None and name in double_instrumentation_check_packages:
            offending_packages.append(str(dist._path))
    if offending_packages:
        _self_deactivate(current_site)
        _print_cannot_auto_instrument_message(
            "The application has OpenTelemetry dependencies which indicate that it is already instrumented. The " +
            "following problematic dependencies have been found: {}. ".format(", ".join(offending_packages)) +
            "Skipping the Dash0 Python auto-instrumentation to avoid double instrumentation. Remove the mentioned "
            "dependencies and make sure the opentelemetry-instrument wrapper executable is not used if you want to " +
            "use Dash0's Python auto-instrumentation")
        return True
    return False


def _read_all_dependencies():
    """Read all flattened dependencies from all-dependencies.txt. Returns list of requirement strings or None on error."""
    dependencies_file = os.path.join(dirname(__file__), "all-dependencies.txt")
    requirements_to_check = []
    try:
        with open(dependencies_file, "r") as f:
            for line in f:
                line = line.strip()
                # Skip empty lines and comments
                if not line or line.startswith("#"):
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

    if req.name == "pip":
        # A lot of applications depend on pip implicitly, without actually importing it, ignore pip for the dependency
        # conflict check.
        return

    try:
        installed_distribution = importlib.metadata.distribution(req.name)
        installed_version = Version(installed_distribution.version)
        _print_debug_msg("installed_version: {}".format(installed_version))

        # Check if installed version satisfies the requirement
        if req.specifier and installed_version not in req.specifier:
            _print_debug_msg("adding version conflict for {}".format(req.name))
            version_conflicts[req.name] = {
                "version_required": str(req.specifier),
                "version_found": str(installed_version),
            }
    except importlib.metadata.PackageNotFoundError:
        _print_debug_msg("adding version error for {}".format(req.name))
        version_conflicts[req.name] = {"error": "required package not found"}


def import_distro():
    _print_debug_msg("checking Python version")
    current_site = dirname(__file__)

    # We cannot use `sys.version_info.major` or other named attributes, as they only got introduced only in Python 3.1.
    if version_info[0] != 3 or version_info[1] < 9:
        _self_deactivate(current_site)
        _print_cannot_auto_instrument_message("unsupported Python version: {}".format(version))
        return
    _print_debug_msg("found eligible Python version: {}".format(version_info))
    _print_debug_msg("checking OTEL_EXPORTER_OTLP_PROTOCOL")

    otlp_protocol = os.environ.get("OTEL_EXPORTER_OTLP_PROTOCOL")
    if otlp_protocol is None:
        # If OTEL_EXPORTER_OTLP_PROTOCOL is not set, opentelemetry-distro defaults to the grpc export, but we do not
        # include the package for that exporter, this would lead to
        # "RuntimeError: Requested component 'otlp_proto_grpc' not found in entry point 'opentelemetry_traces_exporter'"
        _self_deactivate(current_site)
        _print_cannot_auto_instrument_message(
            "OTEL_EXPORTER_OTLP_PROTOCOL is not set. (The container likely has OTEL_EXPORTER_OTLP_ENDPOINT set, which " +
            "prevented the Dash0 operator from setting its own values for OTEL_EXPORTER_OTLP_ENDPOINT/PROTOCOL, " +
            "please remove OTEL_EXPORTER_OTLP_ENDPOINT from the container if you want to use Python " +
            "auto-instrumentation.)"
        )
        return
    if otlp_protocol == "grpc":
        # If OTEL_EXPORTER_OTLP_PROTOCOL is set to grpc explicitly, we need to stand down for the same reason, we do not
        # include the package for that exporter, this would lead to
        # "RuntimeError: Requested component 'otlp_proto_grpc' not found in entry point 'opentelemetry_traces_exporter'"
        _self_deactivate(current_site)
        _print_cannot_auto_instrument_message(
            "OTEL_EXPORTER_OTLP_PROTOCOL=grpc is not supported. (The container has OTEL_EXPORTER_OTLP_PROTOCOL set " +
            "which prevented the Dash0 operator from setting its own values for OTEL_EXPORTER_OTLP_ENDPOINT/PROTOCOL, " +
            "please remove OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_PROTOCOL from the container if you want " +
            "to use Python auto-instrumentation.)"
        )
        return
    _print_debug_msg("found eligible OTEL_EXPORTER_OTLP_PROTOCOL value: {}".format(otlp_protocol))
    _print_debug_msg("checking for double instrumentation")

    # Temporarily remove this site to be able to check for problematic dependencies in all other available sites.
    # Without the current site it is easier to differentiate between "our" dependencies and dependencies brought in by
    # the application under monitoring.
    # Maintenance note: After checking for double instrumentation scenarios, we add back the current site, via
    # path.append(current_site). The remove here together with the append also deliberately reorders sys.path to put
    # this site last, before evaluating conflicting dependency versions. See below for more details on that.
    path.remove(current_site)

    if _check_for_double_instrumentation(current_site):
        return

    _print_debug_msg("no double instrumentation detected")
    _print_debug_msg("checking for dependency conflicts")

    # This "path.append(current_site)" together with "path.remove(current_site)" executed a couple of lines above
    # effectively reorders sys.path to put this site last. This is necessary to evaluate potential conflicting
    # dependency versions.
    #
    # We will leave this reordering in effect when importing and initializing opentelemetry.instrumentation. Since we
    # have already ruled out dependency conflicts, the order should not matter, but with this re-ordering we make sure
    # the application's package versions will win over the package versions we bring (in case there are overlapping
    # dependencies).
    path.append(current_site)

    version_conflicts = {}
    requirements_to_check = _read_all_dependencies()
    if requirements_to_check is None:
        _self_deactivate(current_site)
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
            _self_deactivate(current_site)
            _print_cannot_auto_instrument_message(
                "error when importing/initializing the Python OpenTelemtry auto-instrumentation: {}: {}".format(
                    type(e).__name__, e))
    else:
        # Remove this site for good, we do not want to trigger dependency conflict issues.
        _self_deactivate(current_site)
        _print_cannot_auto_instrument_message("dependency conflicts: {}".format(version_conflicts))


import_distro()
