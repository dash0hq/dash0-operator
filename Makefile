# Use the local Helm chart sources by default, can be overridden with a chart referencing a remote repository,
# for example dash0-operator/dash0-operator. The remote repository needs to have been installed previously, e.g. via
# helm repo add dash0-operator https://dash0hq.github.io/dash0-operator && helm repo update
OPERATOR_HELM_CHART ?= helm-chart/dash0-operator

# Variables for all operator container images:

# Leave empty for local images, set to registry path (e.g., "ghcr.io/dash0hq/") for remote images
IMAGE_REPOSITORY_PREFIX ?= ""

# Use "latest" for local dev, specific tag for CI/remote registries
IMAGE_TAG ?= latest

# Use "Never" for local images, "" (empty) to let Kubernetes decide for remote images
PULL_POLICY ?= Never

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

# Variables for test application container images:

TEST_IMAGE_REPOSITORY_PREFIX ?= ""
TEST_IMAGE_TAG ?= latest
TEST_IMAGE_PULL_POLICY ?= Never

TEST_APP_DOTNET_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-dotnet-test-app
TEST_APP_DOTNET_IMAGE_TAG ?= $(TEST_IMAGE_TAG)
TEST_APP_DOTNET_IMAGE ?= $(TEST_APP_DOTNET_IMAGE_REPOSITORY):$(TEST_APP_DOTNET_IMAGE_TAG)
TEST_APP_DOTNET_IMAGE_PULL_POLICY ?= $(TEST_IMAGE_PULL_POLICY)

TEST_APP_JVM_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-jvm-spring-boot-test-app
TEST_APP_JVM_IMAGE_TAG ?= $(TEST_IMAGE_TAG)
TEST_APP_JVM_IMAGE ?= $(TEST_APP_JVM_IMAGE_REPOSITORY):$(TEST_APP_JVM_IMAGE_TAG)
TEST_APP_JVM_IMAGE_PULL_POLICY ?= $(TEST_IMAGE_PULL_POLICY)

TEST_APP_NODEJS_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-operator-nodejs-20-express-test-app
TEST_APP_NODEJS_IMAGE_TAG ?= $(TEST_IMAGE_TAG)
TEST_APP_NODEJS_IMAGE ?= $(TEST_APP_NODEJS_IMAGE_REPOSITORY):$(TEST_APP_NODEJS_IMAGE_TAG)
TEST_APP_NODEJS_IMAGE_PULL_POLICY ?= $(TEST_IMAGE_PULL_POLICY)

# Variables for additional container images used in end-to-end tests:
DASH0_API_MOCK_IMAGE_REPOSITORY ?= $(TEST_IMAGE_REPOSITORY_PREFIX)dash0-api-mock
DASH0_API_MOCK_IMAGE_TAG ?= $(TEST_IMAGE_TAG)
DASH0_API_MOCK_IMAGE ?= $(DASH0_API_MOCK_IMAGE_REPOSITORY):$(DASH0_API_MOCK_IMAGE_TAG)
DASH0_API_MOCK_IMAGE_PULL_POLICY ?= $(TEST_IMAGE_PULL_POLICY)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
# Maintenance note: Keep this in sync with EnvtestK8sVersion in test/util/constants.go.
ENVTEST_K8S_VERSION = 1.34.1

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

.PHONY: build-images-test-e2e
build-images-test-e2e: docker-build test-e2e ## Build all container images, then run the end-to-end tests.

# Invoking ginkgo via go run makes sure we use the version from go.mod and not a version installed globally, which
# would be used when simply running `ginkgo -v test/e2e`. An alternative would be to invoke ginkgo via go test, that
# is, `go test ./test/e2e/ -v -ginkgo.v`, but that would require us to manage go test's timeout (via the -timeout
# flag), and ginkgo's own timeout.
.PHONY: test-e2e
test-e2e: ## Run the end-to-end tests. When testing local code, container images should be built beforehand (or use target build-images-test-e2e).
	go run github.com/onsi/ginkgo/v2/ginkgo -v test/e2e

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.5.0
golangci-lint-install:
	@[ -f $(GOLANGCI_LINT) ] || { \
	set -e ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) $(GOLANGCI_LINT_VERSION) ;\
	}

.PHONY: golangci-lint
golangci-lint: golangci-lint-install ## Run static code analysis for Go code.
	@echo "-------------------------------- (linting Go code)"
	$(GOLANGCI_LINT) run

.PHONY: golang-lint-fix
golang-lint-fix: golangci-lint-install ## Run static code analysis for Go code and fix issues automatically.
	$(GOLANGCI_LINT) run --fix

.PHONY: helm-chart-lint
helm-chart-lint: ## Run static code analysis for the Helm chart templates.
	@echo "-------------------------------- (linting Helm charts)"
	$(eval HELM_LINT_OUTPUT=$(shell helm lint --quiet helm-chart/dash0-operator))
	@if [ -n "$(HELM_LINT_OUTPUT)" ]; then \
	  echo helm lint found issues: ; \
	  echo "$(HELM_LINT_OUTPUT)" ; \
		exit 1; \
	fi

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
lint: golangci-lint helm-chart-lint shellcheck-lint zig-fmt-check instrumentation-test-lint perses-crd-version-check prometheus-crd-version-check ## Run all static code analysis checks (Go, Helm, shell scripts, Zig, etc.).

.PHONY: lint-fix
lint-fix: golang-lint-fix zig-fmt

PHONY: test-apps-container-image-build
test-apps-container-image-build: \
  test-app-container-image-build-dotnet \
  test-app-container-image-build-jvm \
  test-app-container-image-build-nodejs ## Build all test application container images.

.PHONY: test-app-container-image-build-dotnet
test-app-container-image-build-dotnet: ## Build the .NET test application.
	@$(call build_container_image,$(TEST_APP_DOTNET_IMAGE_REPOSITORY),$(TEST_APP_DOTNET_IMAGE_TAG),test-resources/dotnet)

.PHONY: test-app-container-image-build-jvm
test-app-container-image-build-jvm: ## Build the JVM test application.
	@$(call build_container_image,$(TEST_APP_JVM_IMAGE_REPOSITORY),$(TEST_APP_JVM_IMAGE_TAG),test-resources/jvm/spring-boot)

.PHONY: test-app-container-image-build-nodejs
test-app-container-image-build-nodejs: ## Build the Node.js test application.
	@$(call build_container_image,$(TEST_APP_NODEJS_IMAGE_REPOSITORY),$(TEST_APP_NODEJS_IMAGE_TAG),test-resources/node.js/express)

.PHONY: dash0-api-mock-container-image
dash0-api-mock-container-image: ## Build the Dash0 API mock container image, which is used in end-to-end tests.
	@$(call build_container_image,$(DASH0_API_MOCK_IMAGE_REPOSITORY),$(DASH0_API_MOCK_IMAGE_TAG),test/e2e/dash0-api-mock)

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: \
  docker-build-controller \
  docker-build-instrumentation \
  docker-build-collector \
  docker-build-config-reloader \
  docker-build-filelog-offset-sync \
  docker-build-filelog-offset-volume-ownership ## Build all container images used by the operator.

define build_container_image
$(eval $@_IMAGE_REPOSITORY = $(1))
$(eval $@_IMAGE_TAG = $(2))
$(eval $@_CONTEXT = $(3))
$(eval $@_DOCKERFILE = $(4))
if [[ -n "$($@_IMAGE_REPOSITORY)" ]]; then                                                                \
  if [[ "$($@_IMAGE_REPOSITORY)" = *"/"* && "$($@_IMAGE_REPOSITORY)" != *"/library/"* ]]; then                                                         \
    echo "not rebuilding the image $($@_IMAGE_REPOSITORY), this looks like a remote image";               \
  else                                                                                                    \
    dockerfile=$($@_DOCKERFILE);                                                                          \
    if [[ -z $$dockerfile ]]; then                                                                        \
        dockerfile=$($@_CONTEXT)/Dockerfile;                                                              \
    fi;                                                                                                   \
    echo $(CONTAINER_TOOL) build -t $($@_IMAGE_REPOSITORY):$($@_IMAGE_TAG) -f $$dockerfile $($@_CONTEXT); \
    $(CONTAINER_TOOL) build -t $($@_IMAGE_REPOSITORY):$($@_IMAGE_TAG) -f $$dockerfile $($@_CONTEXT);      \
  fi;                                                                                                     \
elif [[ -n "$(OPERATOR_HELM_CHART_URL)" ]]; then                                                          \
  echo "not rebuilding image, a remote Helm chart is used with the default image from the chart";         \
fi
endef

.PHONY: docker-build-controller
docker-build-controller: ## Build the manager container image.
	@$(call build_container_image,$(CONTROLLER_IMAGE_REPOSITORY),$(CONTROLLER_IMAGE_TAG),.)

.PHONY: docker-build-instrumentation
docker-build-instrumentation: ## Build the instrumentation image.
	@$(call build_container_image,$(INSTRUMENTATION_IMAGE_REPOSITORY),$(INSTRUMENTATION_IMAGE_TAG),images/instrumentation)

.PHONY: docker-build-collector
docker-build-collector: ## Build the OpenTelemetry collector container image.
	@$(call build_container_image,$(COLLECTOR_IMAGE_REPOSITORY),$(COLLECTOR_IMAGE_TAG),images/collector)

.PHONY: docker-build-config-reloader
docker-build-config-reloader: ## Build the config reloader container image.
	@$(call build_container_image,$(CONFIGURATION_RELOADER_IMAGE_REPOSITORY),$(CONFIGURATION_RELOADER_IMAGE_TAG),images,images/configreloader/Dockerfile)

.PHONY: docker-build-filelog-offset-sync
docker-build-filelog-offset-sync: ## Build the filelog offset sync container image.
	@$(call build_container_image,$(FILELOG_OFFSET_SYNC_IMAGE_REPOSITORY),$(FILELOG_OFFSET_SYNC_IMAGE_TAG),images,images/filelogoffsetsync/Dockerfile)

.PHONY: docker-build-filelog-offset-volume-ownership
docker-build-filelog-offset-volume-ownership: ## Build the filelog offset volume ownership container image.
	@$(call build_container_image,$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_REPOSITORY),$(FILELOG_OFFSET_VOLUME_OWNERSHIP_IMAGE_TAG),images,images/filelogoffsetvolumeownership/Dockerfile)

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
