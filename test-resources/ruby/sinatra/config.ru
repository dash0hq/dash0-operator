# frozen_string_literal: true

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A Sinatra application: exercises Rack instrumentation via a framework that is distinct from
# Rails, showing that the Dash0 Ruby distribution instruments at the Rack middleware layer rather
# than exclusively through Rails internals.

require 'sinatra/base'

class SinatraApp < Sinatra::Base
  configure do
    set :logger, Logger.new($stdout)
  end

  get '/ready' do
    status 204
  end

  get '/dash0-k8s-operator-test' do
    request_id = params[:id]
    logger.info(request_id ? "processing request #{request_id}" : 'processing request')
    content_type :json
    { message: 'We make Observability easy for every developer.' }.to_json
  end
end

run SinatraApp
