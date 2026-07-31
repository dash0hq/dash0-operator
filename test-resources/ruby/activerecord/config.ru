# frozen_string_literal: true

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# A Rails application that uses ActiveRecord with a file-backed SQLite database: exercises
# ActiveSupport instrumentation for database queries in addition to the Rack/ActionPack layer.
# SQL queries via ActiveRecord fire ActiveSupport::Notifications events that the Dash0 Ruby
# distribution records as spans.

require 'rails'
require 'action_controller/railtie'
require 'active_record'

DB_PATH = '/tmp/dash0-demo.sqlite3'

class Event < ActiveRecord::Base; end

class TestApp < Rails::Application
  config.root = __dir__
  config.eager_load = false
  config.secret_key_base = 'dash0-operator-activerecord-demo'
  config.logger = Logger.new($stdout)
  config.log_level = :info
  config.hosts.clear

  config.after_initialize do
    ActiveRecord::Base.establish_connection(adapter: 'sqlite3', database: DB_PATH)
    ActiveRecord::Base.connection.create_table(:events, if_not_exists: true) do |t|
      t.string :path, null: false
      t.string :request_id
      t.timestamps
    end
    Event.reset_column_information
  end

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
    Event.create!(path: request.path, request_id: request_id)
    count = Event.count
    render json: {
      message: 'We make Observability easy for every developer.',
      requests_recorded: count
    }
  end
end

TestApp.initialize!
run TestApp
