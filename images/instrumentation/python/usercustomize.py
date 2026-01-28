# Trigger the load of the OpenTelemetry distribution for Python. This is enabled by prepending a directory with this
# script to the PYTHONPATH environment variable via the OpenTelemetry injector.

# IMPORTANT: This file must be valid Python 2.7+

import os

from os.path import dirname
from sys import path, version, version_info

def import_distro():
    print("[usercustomize] import_distro")
    # print("[usercustomize] PYTHONPATH: {}".format(os.environ['PYTHONPATH']))
    current_site = dirname(__file__)
    print("[usercustomize] current_site: {}".format(current_site))
    print("[usercustomize] version_info: {}".format(version_info))
    print("[usercustomize] version_info: {}".format(version_info))
    # We cannot use `sys.version_info.major` or other named attributes, as they only  got introduced only in Python 3.1.
    if version_info[0] != 3 or version_info[1] < 9:
        path.remove(current_site)
        print("Cannot import 'opentelemetry-distro' due unsupported runtime version: {}".format(version))
        return

    # Reorder sys.path to put this site last and evaluate potential conflicts
    path.remove(current_site)
    path.append(current_site)

    print("[usercustomize] path: {}".format(path))

    import importlib.metadata
    from packaging.requirements import Requirement
    from packaging.version import Version

    def _check_dependency_version_conflicts(package_name):
        try:
            distro = importlib.metadata.distribution(package_name)
        except importlib.metadata.PackageNotFoundError:
            return {package_name: {'error': 'package not found'}}

        version_conflicts = {}
        requires = distro.requires
        if requires:
            for req_string in requires:
                req = Requirement(req_string)
                # Skip extras/markers for simplicity in conflict detection
                if req.marker and not req.marker.evaluate():
                    continue

                try:
                    installed_distro = importlib.metadata.distribution(req.name)
                    installed_version = Version(installed_distro.version)

                    # Check if installed version satisfies the requirement
                    if req.specifier and installed_version not in req.specifier:
                        version_conflicts[req.name] = {
                            'version_required': str(req.specifier),
                            'version_found': str(installed_version),
                        }
                except importlib.metadata.PackageNotFoundError:
                    version_conflicts[req.name] = {'error': 'required package not found'}

                # Recursively check dependencies
                version_conflicts.update(
                    _check_dependency_version_conflicts(req.name)
                )

        return version_conflicts

    version_conflicts = _check_dependency_version_conflicts("opentelemetry-distro")

    if not version_conflicts:
        from opentelemetry.instrumentation import auto_instrumentation
        auto_instrumentation.initialize()
    else:
        # Remove this site for good, we do not want to trigger issues
        path.remove(current_site)
        print("Cannot import 'opentelemetry-distro' due to dependency conflicts: {}".format(version_conflicts))

import_distro()