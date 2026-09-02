# frozen_string_literal: true

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A Rails application in a single file: enough of Rails to exercise the parts the Dash0 Ruby
# distribution instruments (Rack, ActionPack routing and controllers, ActiveSupport
# notifications), without the file tree a generated application would bring.

require 'rails'
require 'action_controller/railtie'

class TestApp < Rails::Application
  config.root = __dir__
  config.eager_load = false
  config.secret_key_base = 'dash0-operator-test-app'
  config.logger = Logger.new($stdout)
  config.log_level = :info
  # The requests arrive via an ingress, so the Host header is not known up front.
  config.hosts.clear

  routes.append do
    get '/ready', to: 'test#ready'
    get '/dash0-k8s-operator-test', to: 'test#test'
  end
end

class TestController < ActionController::Base
  def ready
    head :no_content
  end

  def test
    request_id = params[:id]
    Rails.logger.info(request_id ? "processing request #{request_id}" : 'processing request')
    render json: { message: 'We make Observability easy for every developer.' }
  end
end

TestApp.initialize!
run TestApp
