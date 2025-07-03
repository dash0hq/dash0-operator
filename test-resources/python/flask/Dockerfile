# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0
FROM python:3.13-bookworm

WORKDIR /usr/src/app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY *.py .

CMD [ "python", "app.py"]
