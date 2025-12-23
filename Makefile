# Source test-resources/.env if it exists. This avoids having to repeat settings from test-resources/.env on the command
# line when calling make directly. If test-resources/.env does not exist, the -include instruction will be silently
# ignored.
-include test-resources/.env

# Use the local Helm chart sources by default, can be overridden with a chart referencing a remote repository,
# for example dash0-operator/dash0-operator. The remote repository needs to have been installed previously, e.g. via
# helm repo add dash0-operator https://dash0hq.github.io/dash0-operator && helm repo update
OPERATOR_HELM_CHART ?= helm-chart/dash0-operator

# Variables for all operator container images:

# Use "localhost:5001/" for the local registry, leave empty for local images on Docker Desktop, set to any registry
# path (e.g., "ghcr.io/dash0hq/") for remote images.
IMAGE_REPOSITORY_PREFIX ?= ""

# Use "latest" for local dev, specific tag for CI/remote registries
IMAGE_TAG ?= latest

# Use "Always" when using a local registry, use "Never" for local images on Docker Desktop, use "" (empty) to let
# Kubernetes decide the pull policy for remote images.
PULL_POLICY ?= Always

CONTROLLER_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)operator-controller
CONTROLLER_IMAGE_TAG ?= $(IMAGE_TAG)
CONTROLLER_IMAGE ?= $(CONTROLLER_IMAGE_REPOSITORY):$(CONTROLLER_IMAGE_TAG)
CONTROLLER_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

INSTRUMENTATION_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)instrumentation
INSTRUMENTATION_IMAGE_TAG ?= $(IMAGE_TAG)
INSTRUMENTATION_IMAGE ?= $(INSTRUMENTATION_IMAGE_REPOSITORY):$(INSTRUMENTATION_IMAGE_TAG)
INSTRUMENTATION_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

COLLECTOR_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)collector
COLLECTOR_IMAGE_TAG ?= $(IMAGE_TAG)
COLLECTOR_IMAGE ?= $(COLLECTOR_IMAGE_REPOSITORY):$(COLLECTOR_IMAGE_TAG)
COLLECTOR_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

CONFIGURATION_RELOADER_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)configuration-reloader
CONFIGURATION_RELOADER_IMAGE_TAG ?= $(IMAGE_TAG)
CONFIGURATION_RELOADER_IMAGE ?= $(CONFIGURATION_RELOADER_IMAGE_REPOSITORY):$(CONFIGURATION_RELOADER_IMAGE_TAG)
CONFIGURATION_RELOADER_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)filelog-offset-sync
FILELOG_OFFSET_SYNC_IMAGE_TAG ?= $(IMAGE_TAG)
FILELOG_OFFSET_SYNC_IMAGE ?= $(FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY):$(FILELOG_OFFSET_SYNC_IMAGE_TAG)
FILELOG_OFFSET_SYNC_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)filelog-offset-volume-ownership
FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG ?= $(IMAGE_TAG)
FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE ?= $(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY):$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG)
FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

TARGET_ALLOCATOR_IMAGE_REPOSITORY ?= $(IMAGE_REPOSITORY_PREFIX)target-allocator
TARGET_ALLOCATOR_IMAGE_TAG ?= $(IMAGE_TAG)
TARGET_ALLOCATOR_IMAGE ?= $(TARGET_ALLOCATOR_IMAGE_REPOSITORY):$(TARGET_ALLOCATOR_IMAGE_TAG)
TARGET_ALLOCATOR_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

# Variables for test application container images:

TEST_IMAGE_REPOSITORY_PREFIX ?= $(IMAGE_REPOSITORY_PREFIX)
TEST_IMAGE_TAG ?= latest
TEST_IMAGE_PULL_POLICY ?= $(PULL_POLICY)

TEST_APP_DOTNET_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-dotnet-test-app
TEST_APP_DOTNET_IMAGE_TAG ?= $(TEST_IMAGE_TAG)

TEST_APP_JVM_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-jvm-spring-boot-test-app
TEST_APP_JVM_IMAGE_TAG ?= $(TEST_IMAGE_TAG)

TEST_APP_NODEJS_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-nodejs-20-express-test-app
TEST_APP_NODEJS_IMAGE_TAG ?= $(TEST_IMAGE_TAG)

TEST_APP_PYTHON_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-python-flask-test-app
TEST_APP_PYTHON_IMAGE_TAG ?= $(TEST_IMAGE_TAG)

# Variables for additional container images used in end-to-end tests:

DASH0_API_MOCK_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-api-mock
DASH0_API_MOCK_IMAGE_TAG ?= $(TEST_IMAGE_TAG)

TELEMETRY_MATCHER_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)telemetry-matcher
TELEMETRY_MATCHER_IMAGE_TAG ?= $(TEST_IMAGE_TAG)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
# Maintenance note: Keep this in sync with EnvtestK8sVersion in test/util/constants.go.
ENVTEST_K8S_VERSION = 1.34.1

GINKGO_FOCUS ?=

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: go-unit-tests injector-unit-tests helm-unit-tests ## Run all unit tests (Go, Zig/injector & Helm chart unit tests).

.PHONY: go-unit-tests
go-unit-tests: common-package-unit-tests operator-manager-unit-tests ## Run the Go unit tests for all packages.

.PHONY: operator-manager-unit-tests
operator-manager-unit-tests: manifests generate fmt vet envtest ## Run the Go unit tests for the operator code.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v -e /e2e -e /vendored) -coverprofile cover.out

.PHONY: common-package-unit-tests
common-package-unit-tests: ## Run the Go unit tests for the common package (code shared between operator manager and other images, i.e. config-reloader, filelogoffsetsync).
	go test github.com/dash0hq/dash0-operator/images/pkg/common

.PHONY: injector-unit-tests
injector-unit-tests: zig-installed ## Run the Zig unit tests for the Dash0 injector binary.
	cd images/instrumentation/injector && zig build test

.PHONY: helm-unit-tests
helm-unit-tests: ## Run the Helm chart unit tests.
	cd helm-chart/dash0-operator && helm unittest -f 'tests/**/*.yaml' .

.PHONY: build-all-test-e2e
build-all-test-e2e: all-images test-e2e ## Builds (but does not push) all container images, then runs the end-to-end tests.

.PHONY: build-all-push-all-test-e2e
build-all-push-all-test-e2e: all-images push-all-images test-e2e ## Builds and pushes all container images, then runs the end-to-end tests.

# Invoking ginkgo via go run makes sure we use the version from go.mod and not a version installed globally, which
# would be used when simply running `ginkgo -v test/e2e`. An alternative would be to invoke ginkgo via go test, that
# is, `go test ./test/e2e/ -v -ginkgo.v`, but that would require us to manage go test's timeout (via the -timeout
# flag), and ginkgo's own timeout.
.PHONY: test-e2e
test-e2e: ## Run the end-to-end tests. When testing local code, container images should be built beforehand (or use target build-images-test-e2e).
ifdef GINKGO_FOCUS
	go run github.com/onsi/ginkgo/v2/ginkgo -v -focus="$(GINKGO_FOCUS)" test/e2e
else
	go run github.com/onsi/ginkgo/v2/ginkgo -v test/e2e
endif

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.7.2
golangci-lint-install:
	@[ -f $(GOLANGCI_LINT) ] || { \
	set -e ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) $(GOLANGCI_LINT_VERSION) ;\
	}

.PHONY: golangci-lint
golangci-lint: golangci-lint-install ## Run static code analysis for Go code.
	@echo "-------------------------------- (linting Go code)"
	@find . -maxdepth 5 -type f -name go.mod -print0 | xargs -0 -I{} $(SHELL) -c 'set -eo pipefail; dir=$$(dirname {}); echo $$dir; pushd $$dir > /dev/null; $(GOLANGCI_LINT) run; popd > /dev/null'

.PHONY: golangci-lint-fix
golangci-lint-fix: golangci-lint-install ## Run static code analysis for Go code and fix issues automatically.
	@find . -maxdepth 5 -type f -name go.mod -print0 | xargs -0 -I{} $(SHELL) -c 'set -eo pipefail; dir=$$(dirname {}); echo $$dir; pushd $$dir > /dev/null; $(GOLANGCI_LINT) run --fix; popd > /dev/null'

.PHONY: internal-config-map-lint
internal-config-map-lint: ## Verify config map templates for resources managed by the collector.
	@if grep -ne '[[:space:]]$$' internal/collectors/otelcolresources/daemonset.config.yaml.template; then \
	  echo "error: internal/collectors/otelcolresources/daemonset.config.yaml.template contains whitespace followed by a new line in the line printed above. Remove the offending characters."; \
	  exit 1; \
	fi
	@if grep -ne '[[:space:]]$$' internal/collectors/otelcolresources/deployment.config.yaml.template; then \
	  echo "error: internal/collectors/otelcolresources/deployment.config.yaml.template contains whitespace followed by a new line in the line printed above. Remove the offending characters."; \
	  exit 1; \
	fi

define lint_helm_chart
$(eval HELM_LINT_OUTPUT=$(shell helm lint --quiet $(1)))
if [ -n "$(HELM_LINT_OUTPUT)" ]; then \
  echo helm lint found issues: ; \
  echo "$(HELM_LINT_OUTPUT)" ; \
  exit 1; \
fi
endef

.PHONY: helm-chart-lint
helm-chart-lint: ## Run static code analysis for the Helm chart templates.
	@echo "-------------------------------- (linting Helm charts)"
	@$(call lint_helm_chart,helm-chart/dash0-operator)
	@$(call lint_helm_chart,test-resources/dotnet/helm-chart)
	@$(call lint_helm_chart,test-resources/jvm/spring-boot/helm-chart)
	@$(call lint_helm_chart,test-resources/node.js/express/helm-chart)
	@$(call lint_helm_chart,test-resources/python/flask/helm-chart)
	@$(call lint_helm_chart,test/e2e/dash0-api-mock/helm-chart)
	@$(call lint_helm_chart,test/e2e/otlp-sink/helm-chart)

.PHONY: shellcheck-check-installed
shellcheck-check-installed:
	@set +x
	@if ! shellcheck --version > /dev/null; then \
	echo "error: shellcheck is not installed. See https://github.com/koalaman/shellcheck?tab=readme-ov-file#installing for installation instructions."; \
	exit 1; \
	fi

.PHONY: shellcheck-lint
shellcheck-lint: shellcheck-check-installed ## Run static code analysis for all shell scripts.
	@echo "-------------------------------- (linting shell scripts)"
	find . -name \*.sh | xargs shellcheck -x

.PHONY: zig-installed
zig-installed:
	@set +x
	@if ! zig version > /dev/null; then \
		echo "error: zig is not installed. Run 'brew install zig' or similar."; \
		exit 1; \
	fi

.PHONY: zig-fmt-check
zig-fmt-check: zig-installed ## Run formatting checks for the Zig code.
ifeq ("${CI}","true")
	@echo "CI: skip linting Zig source files via make lint, will run as separate Github action job step"
else
	@echo "-------------------------------- (linting Zig source files)"
	zig fmt --check images/instrumentation/injector/src
endif

.PHONY: zig-fmt
zig-fmt: zig-installed ## Fix Zig code formatting.
	zig fmt images/instrumentation/injector/src

.PHONY: npm-installed
npm-installed:
	@set +x
	@if ! npm --version > /dev/null; then \
		echo "error: npm is not installed."; \
		exit 1; \
	fi

.PHONY: instrumentation-test-lint
instrumentation-test-lint: npm-installed
	@echo "-------------------------------- (linting the instrumentation tests)"
	cd images/instrumentation/test && npm ci && npm run lint

.PHONY: prometheus-crd-version-check
prometheus-crd-version-check: ## Check whether all references to the PrometheusRule CRD are in sync.
	@echo "-------------------------------- (verifying the Prometheus CRD version is in sync)"
	./test-resources/bin/prometheus-crd-version-check.sh

.PHONY: perses-crd-version-check
perses-crd-version-check: ## Check whether all references to the PersesDashboard CRD are in sync.
	@echo "-------------------------------- (verifying the Perses CRD version is in sync)"
	./test-resources/bin/perses-crd-version-check.sh

.PHONY: lint
lint: golangci-lint internal-config-map-lint helm-chart-lint shellcheck-lint zig-fmt-check instrumentation-test-lint perses-crd-version-check prometheus-crd-version-check ## Run all static code analysis checks (Go, Helm, shell scripts, Zig, etc.).

.PHONY: lint-fix
lint-fix: golangci-lint-fix zig-fmt

##@ Building/Pushing Test App and Auxiliary Images

PHONY: all-images
all-images: \
  images \
  all-auxiliary-images ## Build all production and all auxiliary images (i.e. images that are used in test scripts and/or e2e tests. If IMAGE_PLATFORMS is set, it will be passed as --platform to the build.

PHONY: all-auxiliary-images
all-auxiliary-images: \
  test-app-images \
  dash0-api-mock-image \
  telemetry-matcher-image ## Build all auxiliary images that are used in test scripts and/or e2e tests, that is, test applications, dash0-api-mock, telemetry-matcher etc. If IMAGE_PLATFORMS is set, it will be passed as --platform to the build.

PHONY: test-app-images
test-app-images: \
  test-app-image-dotnet \
  test-app-image-jvm \
  test-app-image-nodejs \
  test-app-image-python ## Build all test application container images. If IMAGE_PLATFORMS is set, it will be passed as --platform to the build.

.PHONY: test-app-image-dotnet
test-app-image-dotnet: ## Build the .NET test application.
	@$(call build_container_image,$(TEST_APP_DOTNET_IMAGE_REPOSITORY),$(TEST_APP_DOTNET_IMAGE_TAG),test-resources/dotnet)

.PHONY: test-app-image-jvm
test-app-image-jvm: ## Build the JVM test application.
	@$(call build_container_image,$(TEST_APP_JVM_IMAGE_REPOSITORY),$(TEST_APP_JVM_IMAGE_TAG),test-resources/jvm/spring-boot)

.PHONY: test-app-image-nodejs
test-app-image-nodejs: ## Build the Node.js test application.
	@$(call build_container_image,$(TEST_APP_NODEJS_IMAGE_REPOSITORY),$(TEST_APP_NODEJS_IMAGE_TAG),test-resources/node.js/express)

.PHONY: test-app-image-python
test-app-image-python: ## Build the Python test application.
	@$(call build_container_image,$(TEST_APP_PYTHON_IMAGE_REPOSITORY),$(TEST_APP_PYTHON_IMAGE_TAG),test-resources/python/flask)

.PHONY: dash0-api-mock-image
dash0-api-mock-image: ## Build the Dash0 API mock container image, which is used in end-to-end tests.
	@$(call build_container_image,$(DASH0_API_MOCK_IMAGE_REPOSITORY),$(DASH0_API_MOCK_IMAGE_TAG),test/e2e/dash0-api-mock)

.PHONY: telemetry-matcher-image
telemetry-matcher-image: ## Build the telemetry-matcher container image, which is used in end-to-end tests.
	@$(call build_container_image,$(TELEMETRY_MATCHER_IMAGE_REPOSITORY),$(TELEMETRY_MATCHER_IMAGE_TAG),test/e2e,test/e2e/otlp-sink/telemetrymatcher/Dockerfile)

PHONY: push-all-images
push-all-images: \
  push-images \
  push-all-auxiliary-images ## Push all production and all auxiliary images (i.e. images that are used in test scripts and/or e2e tests.

PHONY: push-all-auxiliary-images
push-all-auxiliary-images: \
  push-test-app-images \
  push-dash0-api-mock-image \
  push-telemetry-matcher-image ## Push all auxiliary images that are used in test scripts and/or e2e tests, that is, test applications, dash0-api-mock, telemetry-matcher etc.

PHONY: push-test-app-images
push-test-app-images: \
  push-test-app-image-dotnet \
  push-test-app-image-jvm \
  push-test-app-image-nodejs \
  push-test-app-image-python ## Push all test application container images.

.PHONY: push-test-app-image-dotnet
push-test-app-image-dotnet: ## Push the .NET test app image.
	@$(call push_container_image,$(TEST_APP_DOTNET_IMAGE_REPOSITORY),$(TEST_APP_DOTNET_IMAGE_TAG))

.PHONY: push-test-app-image-jvm
push-test-app-image-jvm: ## Push the JVM test app image.
	@$(call push_container_image,$(TEST_APP_JVM_IMAGE_REPOSITORY),$(TEST_APP_JVM_IMAGE_TAG))

.PHONY: push-test-app-image-nodejs
push-test-app-image-nodejs: ## Push the Node.js test app image.
	@$(call push_container_image,$(TEST_APP_NODEJS_IMAGE_REPOSITORY),$(TEST_APP_NODEJS_IMAGE_TAG))

.PHONY: push-test-app-image-python
push-test-app-image-python: ## Push the Python test app image.
	@$(call push_container_image,$(TEST_APP_PYTHON_IMAGE_REPOSITORY),$(TEST_APP_PYTHON_IMAGE_TAG))

.PHONY: push-dash0-api-mock-image
push-dash0-api-mock-image: ## Push the Dash0 API mock container image.
	@$(call push_container_image,$(DASH0_API_MOCK_IMAGE_REPOSITORY),$(DASH0_API_MOCK_IMAGE_TAG))

.PHONY: push-telemetry-matcher-image
push-telemetry-matcher-image: ## Push the telemetry-matcher container image.
	@$(call push_container_image,$(TELEMETRY_MATCHER_IMAGE_REPOSITORY),$(TELEMETRY_MATCHER_IMAGE_TAG))

##@ Build Production Code and Production Images

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: images
images: \
  image-controller \
  image-instrumentation \
  image-collector \
  image-config-reloader \
  image-filelog-offset-sync \
  image-filelog-offset-volume-ownership \
  image-target-allocator ## Build all container images used by the operator. If IMAGE_PLATFORMS is set, it will be passed as --platform to the build.

define build_container_image
$(eval $@_IMAGE_REPOSITORY = $(1))
$(eval $@_IMAGE_TAG = $(2))
$(eval $@_CONTEXT = $(3))
$(eval $@_DOCKERFILE = $(4))
dockerfile=$($@_DOCKERFILE);                                                                     \
if [[ -z $$dockerfile ]]; then                                                                   \
  dockerfile=$($@_CONTEXT)/Dockerfile;                                                           \
fi;                                                                                              \
build_cmd="$(CONTAINER_TOOL) build";                                                             \
if [[ -n "$(IMAGE_PLATFORMS)" ]]; then                                                           \
  build_cmd="$$build_cmd --platform $(IMAGE_PLATFORMS)";                                         \
fi;                                                                                              \
build_cmd="$$build_cmd -t $($@_IMAGE_REPOSITORY):$($@_IMAGE_TAG) -f $$dockerfile $($@_CONTEXT)"; \
echo "$$build_cmd";                                                                              \
$$build_cmd;
endef

.PHONY: image-controller
image-controller: ## Build the manager container image.
	@$(call build_container_image,$(CONTROLLER_IMAGE_REPOSITORY),$(CONTROLLER_IMAGE_TAG),.)

.PHONY: image-instrumentation
image-instrumentation: ## Build the instrumentation image.
	@$(call build_container_image,$(INSTRUMENTATION_IMAGE_REPOSITORY),$(INSTRUMENTATION_IMAGE_TAG),images/instrumentation)

.PHONY: image-collector
image-collector: ## Build the OpenTelemetry collector container image.
	@$(call build_container_image,$(COLLECTOR_IMAGE_REPOSITORY),$(COLLECTOR_IMAGE_TAG),images/collector)

.PHONY: image-config-reloader
image-config-reloader: ## Build the config reloader container image.
	@$(call build_container_image,$(CONFIGURATION_RELOADER_IMAGE_REPOSITORY),$(CONFIGURATION_RELOADER_IMAGE_TAG),images,images/configreloader/Dockerfile)

.PHONY: image-filelog-offset-sync
image-filelog-offset-sync: ## Build the filelog offset sync container image.
	@$(call build_container_image,$(FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY),$(FILELOG_OFFSET_SYNC_IMAGE_TAG),images,images/filelogoffsetsync/Dockerfile)

.PHONY: image-filelog-offset-volume-ownership
image-filelog-offset-volume-ownership: ## Build the filelog offset volume ownership container image.
	@$(call build_container_image,$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY),$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG),images,images/filelogoffsetvolumeownership/Dockerfile)

.PHONY: image-target-allocator
image-target-allocator: ## Build the OpenTelemetry target-allocator container image.
	@$(call build_container_image,$(TARGET_ALLOCATOR_IMAGE_REPOSITORY),$(TARGET_ALLOCATOR_IMAGE_TAG),images/target-allocator)

.PHONY: push-images
push-images: \
	push-image-controller \
	push-image-instrumentation \
	push-image-collector \
	push-image-config-reloader \
	push-image-filelog-offset-sync \
	push-image-filelog-offset-volume-ownership \
	push-image-target-allocator ## Push all container images using the full image reference.

define push_container_image
$(eval $@_IMAGE_REPOSITORY = $(1))
$(eval $@_IMAGE_TAG = $(2))
push_cmd="$(CONTAINER_TOOL) push";                                                              \
push_cmd="$$push_cmd $($@_IMAGE_REPOSITORY):$($@_IMAGE_TAG)";                                   \
echo "$$push_cmd";                                                                              \
$$push_cmd;
endef

.PHONY: push-image-controller
push-image-controller: ## Push the manager container image.
	@$(call push_container_image,$(CONTROLLER_IMAGE_REPOSITORY),$(CONTROLLER_IMAGE_TAG))

.PHONY: push-image-instrumentation
push-image-instrumentation: ## Push the instrumentation image.
	@$(call push_container_image,$(INSTRUMENTATION_IMAGE_REPOSITORY),$(INSTRUMENTATION_IMAGE_TAG))

.PHONY: push-image-collector
push-image-collector: ## Push the OpenTelemetry collector container image.
	@$(call push_container_image,$(COLLECTOR_IMAGE_REPOSITORY),$(COLLECTOR_IMAGE_TAG))

.PHONY: push-image-config-reloader
push-image-config-reloader: ## Push the config reloader container image.
	@$(call push_container_image,$(CONFIGURATION_RELOADER_IMAGE_REPOSITORY),$(CONFIGURATION_RELOADER_IMAGE_TAG))

.PHONY: push-image-filelog-offset-sync
push-image-filelog-offset-sync: ## Push the filelog offset sync container image.
	@$(call push_container_image,$(FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY),$(FILELOG_OFFSET_SYNC_IMAGE_TAG))

.PHONY: push-image-filelog-offset-volume-ownership
push-image-filelog-offset-volume-ownership: ## Push the filelog offset volume ownership container image.
	@$(call push_container_image,$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY),$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG))

.PHONY: push-image-target-allocator
push-image-target-allocator: ## Push the OpenTelemetry target-allocator container image.
	@$(call push_container_image,$(TARGET_ALLOCATOR_IMAGE_REPOSITORY),$(TARGET_ALLOCATOR_IMAGE_TAG))

.PHONY: deploy
deploy: ## Deploy the controller via helm to the current kubectl context.
	test-resources/bin/render-templates.sh
	helm upgrade --install \
		--namespace dash0-system \
		--create-namespace \
		--set operator.image.repository=$(CONTROLLER_IMAGE_REPOSITORY) \
		--set operator.image.tag=$(CONTROLLER_IMAGE_TAG) \
		--set operator.image.pullPolicy=$(CONTROLLER_IMAGE_PULL_POLICY) \
		--set operator.initContainerImage.repository=$(INSTRUMENTATION_IMAGE_REPOSITORY) \
		--set operator.initContainerImage.tag=$(INSTRUMENTATION_IMAGE_TAG) \
		--set operator.initContainerImage.pullPolicy=$(INSTRUMENTATION_IMAGE_PULL_POLICY) \
		--set operator.collectorImage.repository=$(COLLECTOR_IMAGE_REPOSITORY) \
		--set operator.collectorImage.tag=$(COLLECTOR_IMAGE_TAG) \
		--set operator.collectorImage.pullPolicy=$(COLLECTOR_IMAGE_PULL_POLICY) \
		--set operator.configurationReloaderImage.repository=$(CONFIGURATION_RELOADER_IMAGE_REPOSITORY) \
		--set operator.configurationReloaderImage.tag=$(CONFIGURATION_RELOADER_IMAGE_TAG) \
		--set operator.configurationReloaderImage.pullPolicy=$(CONFIGURATION_RELOADER_IMAGE_PULL_POLICY) \
		--set operator.filelogOffsetSyncImage.repository=$(FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY) \
		--set operator.filelogOffsetSyncImage.tag=$(FILELOG_OFFSET_SYNC_IMAGE_TAG) \
		--set operator.filelogOffsetSyncImage.pullPolicy=$(FILELOG_OFFSET_SYNC_IMAGE_PULL_POLICY) \
		--set operator.filelogOffsetVolumeOwnershipImage.repository=$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY) \
		--set operator.filelogOffsetVolumeOwnershipImage.tag=$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG) \
		--set operator.filelogOffsetVolumeOwnershipImage.pullPolicy=$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_PULL_POLICY) \
		--set operator.targetAllocatorImage.repository=$(TARGET_ALLOCATOR_IMAGE_REPOSITORY) \
		--set operator.targetAllocatorImage.tag=$(TARGET_ALLOCATOR_IMAGE_TAG) \
		--set operator.targetAllocatorImage.pullPolicy=$(TARGET_ALLOCATOR_IMAGE_PULL_POLICY) \
		--set operator.developmentMode=true \
		dash0-operator \
		$(OPERATOR_HELM_CHART)

.PHONY: undeploy
undeploy: ## Undeploy the controller via helm from the current kubectl context.
	helm uninstall --namespace dash0-system dash0-operator --timeout 30s
	$(KUBECTL) delete ns dash0-system

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
HUSKY ?= $(LOCALBIN)/husky

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.1
CONTROLLER_TOOLS_VERSION ?= v0.19.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: husky-install
husky-install:
ifeq ("${CI}","true")
	@echo "CI: skipping husky-install in CI"
else
	(test -d .git && (test -s $(HUSKY) || GOBIN=$(LOCALBIN) go install github.com/automation-co/husky@latest)) || true
endif

# Install git hooks, but only if .git actually exists (this helps with git worktrees, where .git does not exist).
.PHONY: husky-setup-hooks
husky-setup-hooks: husky-install
ifeq ("${CI}","true")
	@echo "CI: skipping husky-setup-hooks in CI"
else
	(test -d .git && $(HUSKY) install) || true
endif
