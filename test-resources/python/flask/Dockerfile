# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0
FROM python:3.14.3-trixie

WORKDIR /usr/src/app

COPY requirements.txt .
COPY *.py .
RUN pip install --no-cache-dir -r requirements.txt

CMD [ "python", "app.py"]
