# frozen_string_literal: true

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A minimal pure-Rack application: exercises Rack instrumentation at its lowest level, with no
# additional framework. The Dash0 Ruby distribution wraps the outermost Rack middleware layer, so
# this app verifies that instrumentation works without Rails or Sinatra present.

require 'rack'
require 'json'
require 'logger'

logger = Logger.new($stdout)

app = lambda do |env|
  req = Rack::Request.new(env)
  case req.path_info
  when '/ready'
    [204, {}, []]
  when '/dash0-k8s-operator-test'
    request_id = req.params['id']
    logger.info(request_id ? "processing request #{request_id}" : 'processing request')
    body = JSON.generate({ message: 'We make Observability easy for every developer.' })
    [200, { 'content-type' => 'application/json' }, [body]]
  else
    [404, { 'content-type' => 'text/plain' }, ['Not Found']]
  end
end

run app
