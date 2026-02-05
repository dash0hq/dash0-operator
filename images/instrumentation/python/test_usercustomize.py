# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Unit tests for usercustomize.py
# This file tests the OpenTelemetry Python instrumentation initialization logic.

from __future__ import print_function

import os
import sys
import unittest
import importlib.util
import importlib.metadata
from io import StringIO
from unittest.mock import Mock, MagicMock, patch
from packaging.requirements import Requirement # noqa: F401
from packaging.version import Version # noqa: F401

# Determine the absolute path to the directory containing this test file
TEST_DIR = os.path.dirname(os.path.abspath(__file__))
USERCUSTOMIZE_PATH = os.path.join(TEST_DIR, 'usercustomize.py')


def load_usercustomize_module():
    """Helper to load the usercustomize module."""
    spec = importlib.util.spec_from_file_location(
        "usercustomize_for_test",
        USERCUSTOMIZE_PATH
    )
    module = importlib.util.module_from_spec(spec)
    return module, spec


def create_dirname_side_effect(mock_site):
    """Create a side_effect function for os.path.dirname that returns mock_site on first call, but delegate to the
    actual dirname function for subsequent calls."""
    from os.path import dirname as original_dirname
    first_call = [True]

    def dirname_side_effect(path):
        if first_call[0]:
            first_call[0] = False
            return mock_site
        return original_dirname(path)

    return dirname_side_effect


class TestImportDistro(unittest.TestCase):
    """Test suite for the import_distro function in usercustomize.py."""

    def setUp(self):
        """Set up test fixtures before each test method."""
        # Store original environment variables
        self.original_env = os.environ.copy()
        # Store original sys.path
        self.original_sys_path = sys.path.copy()
        # Clear relevant environment variables for clean test state
        for key in ['OTEL_INJECTOR_LOG_LEVEL', 'OTEL_EXPORTER_OTLP_PROTOCOL']:
            os.environ.pop(key, None)

    def tearDown(self):
        """Clean up after each test method."""
        # Restore original environment
        os.environ.clear()
        os.environ.update(self.original_env)
        # Restore original sys.path
        sys.path = self.original_sys_path.copy()

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (2, 7, 0, 'final', 0))
    @patch('sys.version', '2.7.0')
    def test_python_2_7_too_old(self, mock_stderr):
        """Test that Python versions older than 3.9 are rejected."""
        mock_site = '/mock/site-packages'

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                module, spec = load_usercustomize_module()
                spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        self.assertIn("[dash0] warning: cannot auto-instrument Python process: unsupported Python version: 2.7.0", output)

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (3, 8, 0, 'final', 0))
    @patch('sys.version', '3.8.0')
    def test_python_3_8_too_old(self, mock_stderr):
        """Test that Python 3.8 is rejected (needs 3.9+)."""
        mock_site = '/mock/site-packages'

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                module, spec = load_usercustomize_module()
                spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        self.assertIn("[dash0] warning: cannot auto-instrument Python process: unsupported Python version: 3.8.0", output)

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (3, 9, 0, 'final', 0))
    def test_missing_otlp_protocol_env_var(self, mock_stderr):
        """Test that missing OTEL_EXPORTER_OTLP_PROTOCOL is rejected."""
        os.environ.pop('OTEL_EXPORTER_OTLP_PROTOCOL', None)
        mock_site = '/mock/site-packages'

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                module, spec = load_usercustomize_module()
                spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        self.assertIn("[dash0] warning: cannot auto-instrument Python process: OTEL_EXPORTER_OTLP_PROTOCOL is not set", output)

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (3, 9, 0, 'final', 0))
    def test_grpc_protocol_rejected(self, mock_stderr):
        """Test that OTEL_EXPORTER_OTLP_PROTOCOL=grpc is rejected."""
        os.environ['OTEL_EXPORTER_OTLP_PROTOCOL'] = 'grpc'
        mock_site = '/mock/site-packages'

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                module, spec = load_usercustomize_module()
                spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        self.assertIn("[dash0] warning: cannot auto-instrument Python process: OTEL_EXPORTER_OTLP_PROTOCOL=grpc is not supported", output)

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (3, 9, 0, 'final', 0))
    def test_dependency_conflict_detection(self, mock_stderr):
        """Test that dependency version conflicts are detected."""
        os.environ['OTEL_EXPORTER_OTLP_PROTOCOL'] = 'http/protobuf'
        mock_site = '/mock/site-packages'

        # Create mock distribution with conflicting version
        mock_packaging = Mock()
        mock_packaging.version = '19.0'  # Too old

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                # Mock _read_all_dependencies to return a requirement that will conflict
                with patch('builtins.open', unittest.mock.mock_open(read_data='packaging >=20.0\n')):
                    with patch('importlib.metadata.distribution') as mock_dist:
                        def dist_side_effect(name):
                            if name == 'packaging':
                                return mock_packaging
                            else:
                                # Return mocks for other packages
                                other_mock = Mock()
                                return other_mock

                        mock_dist.side_effect = dist_side_effect

                        module, spec = load_usercustomize_module()
                        spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        # Should report dependency conflicts
        self.assertIn("[dash0] warning: cannot auto-instrument Python process: dependency conflicts: {'packaging': {'version_required': '>=20.0', 'version_found': '19.0'}}", output)

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (3, 9, 0, 'final', 0))
    def test_successful_initialization(self, mock_stderr):
        """Test successful auto-instrumentation initialization."""
        os.environ['OTEL_EXPORTER_OTLP_PROTOCOL'] = 'http/protobuf'
        os.environ['OTEL_INJECTOR_LOG_LEVEL'] = 'debug'
        mock_site = '/mock/site-packages'

        # Create mock distribution with matching version
        mock_packaging = Mock()
        mock_packaging.version = '21.0'  # Satisfies >=20.0

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                # Mock _read_all_dependencies to return a requirement that is satisfied
                with patch('builtins.open', unittest.mock.mock_open(read_data='packaging >=20.0\n')):
                    with patch('importlib.metadata.distribution') as mock_dist:
                        mock_dist.return_value = mock_packaging
                        with patch.dict('sys.modules', {
                            'opentelemetry': MagicMock(),
                            'opentelemetry.instrumentation': MagicMock(),
                            'opentelemetry.instrumentation.auto_instrumentation': MagicMock()
                        }):
                            module, spec = load_usercustomize_module()
                            spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        self.assertIn("[dash0] importing and initializing the Python auto-instrumentation now", output)

    @patch('sys.stderr', new_callable=StringIO)
    @patch('sys.version_info', (3, 9, 0, 'final', 0))
    def test_package_not_found_in_dependency_tree(self, mock_stderr):
        """Test dependency conflict when a required package is missing."""
        os.environ['OTEL_EXPORTER_OTLP_PROTOCOL'] = 'http/protobuf'
        mock_site = '/mock/site-packages'

        with patch('os.path.dirname', side_effect=create_dirname_side_effect(mock_site)):
            with patch('sys.path', [mock_site]):
                # Mock _read_all_dependencies to return a requirement for a missing package
                with patch('builtins.open', unittest.mock.mock_open(read_data='missing-package >=1.0\n')):
                    with patch('importlib.metadata.distribution') as mock_dist:
                        def dist_side_effect(name):
                            if name == 'missing-package':
                                # This dependency is not found
                                raise importlib.metadata.PackageNotFoundError()
                            else:
                                # Other packages exist
                                mock_pkg = Mock()
                                return mock_pkg

                        mock_dist.side_effect = dist_side_effect

                        module, spec = load_usercustomize_module()
                        spec.loader.exec_module(module)

        output = mock_stderr.getvalue()
        # Should report dependency conflicts for the missing required package
        self.assertIn("[dash0] warning: cannot auto-instrument Python process: dependency conflicts", output)


if __name__ == '__main__':
    unittest.main()