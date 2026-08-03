# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0
FROM ruby:3.4.10-slim-trixie

WORKDIR /usr/src/app

RUN apt-get update && \
  apt-get install -y --no-install-recommends build-essential && \
  rm -rf /var/lib/apt/lists/*

COPY Gemfile Gemfile.lock ./
RUN bundle install

COPY config.ru .

CMD ["sh", "-c", "bundle exec puma --port ${PORT:-1607} config.ru"]
