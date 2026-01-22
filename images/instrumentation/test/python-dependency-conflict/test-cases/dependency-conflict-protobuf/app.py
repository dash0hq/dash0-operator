# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

import sys
import google.protobuf

protobuf_version = google.protobuf.__version__

if protobuf_version!= '4.25.8':
    print(f"Unexpected protobuf version in {__file__}: {protobuf_version}", file=sys.stderr)
    sys.exit(1)
