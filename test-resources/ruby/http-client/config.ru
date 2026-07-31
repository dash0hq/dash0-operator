# frozen_string_literal: true

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A Rails application that makes outbound HTTP calls via Net::HTTP: exercises both the
# server-side Rack/ActionPack instrumentation and the client-side Net::HTTP instrumentation.
# The resulting traces span two services (this app and the upstream it calls), demonstrating
# distributed trace context propagation.
#
# Set UPSTREAM_URL to the URL this app should call on each request.

require 'rails'
require 'action_controller/railtie'
require 'net/http'
require 'uri'
require 'json'

UPSTREAM_URL = ENV.fetch('UPSTREAM_URL', 'http://ruby-rails-demo-svc:8080/dash0-k8s-operator-test')

class TestApp < Rails::Application
  config.root = __dir__
  config.eager_load = false
  config.secret_key_base = 'dash0-operator-http-client-demo'
  config.logger = Logger.new($stdout)
  config.log_level = :info
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

    upstream_uri = URI(UPSTREAM_URL)
    upstream_uri.query = URI.encode_www_form({ id: request_id }.compact)

    upstream_response = Net::HTTP.get_response(upstream_uri)
    upstream_body = JSON.parse(upstream_response.body)

    render json: {
      message: 'We make Observability easy for every developer.',
      upstream_url: upstream_uri.to_s,
      upstream_status: upstream_response.code.to_i,
      upstream_response: upstream_body
    }
  rescue => e
    Rails.logger.error("upstream call failed: #{e.message}")
    render json: {
      message: 'We make Observability easy for every developer.',
      upstream_url: UPSTREAM_URL,
      upstream_error: e.message
    }
  end
end

TestApp.initialize!
run TestApp
