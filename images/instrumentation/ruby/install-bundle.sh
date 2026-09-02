#!/usr/bin/env sh

# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

# Installs the Dash0 OpenTelemetry Ruby distribution and its dependency closure into a plain GEM_HOME, laid out the way
# the OpenTelemetry injector expects: the injector requires <target>/opentelemetry-auto-instrumentation.rb via RUBYOPT
# and passes <target> as OTEL_RUBY_ADDITIONAL_GEM_PATH so the distribution can find its own dependencies.
#
# Usage: install-bundle.sh <distribution source tree> <target directory>

set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <distribution source tree> <target directory>" >&2
  exit 1
fi

distribution_dir="$1"
target_dir="$2"

# gem build resolves the gemspec's file globs relative to the working directory, so it has to run inside the tree.
cd "${distribution_dir}"
gem build dash0-opentelemetry.gemspec -o /tmp/dash0-opentelemetry.gem

# Install the exact versions from the distribution's packaging lock file instead of resolving the closure fresh, so the
# installed bundle is reproducible. The distribution itself is path-sourced in that lock file and therefore skipped
# here; it is installed from the gem built above. Dependencies are ignored because the lock file already provides the
# fully resolved set.
BUNDLE_GEMFILE="${distribution_dir}/packaging/Gemfile" ruby -rbundler -e '
  lockfile = Bundler::LockfileParser.new(File.read(ARGV[0]))
  pins = lockfile.specs
                 .reject { |spec| spec.source.is_a?(Bundler::Source::Path) }
                 .map { |spec| "#{spec.name}:#{spec.version}" }
                 .uniq
  File.write("/tmp/pins.txt", pins.join(" "))
' "${distribution_dir}/packaging/Gemfile.lock"

# shellcheck disable=SC2046 # deliberate word splitting: the pins file holds a space-separated list of name:version pairs
gem install --install-dir "${target_dir}" --no-document --ignore-dependencies \
  $(cat /tmp/pins.txt) /tmp/dash0-opentelemetry.gem

# The injector requires a fixed file name as the entry point. A relative symlink keeps resolving after the bundle has
# been copied into the instrumented container.
cd "${target_dir}"
set -- gems/dash0-opentelemetry-*/
if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  echo "expected exactly one dash0-opentelemetry gem directory below ${target_dir}/gems, found: $*" >&2
  exit 1
fi
ln -s "${1%/}/lib/dash0-opentelemetry.rb" opentelemetry-auto-instrumentation.rb
