# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# dash0.com/dash0-operator-bundle:$VERSION and dash0.com/dash0-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= dash0hq/dash0-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.34.1

# Use the local Helm chart sources by default, can be overridden with a chart referencing a remote repository,
# for example dash0-operator/dash0-operator. The remote repository needs to have been installed previously, e.g. via
# helm repo add dash0-operator https://dash0hq.github.io/dash0-operator && helm repo update
OPERATOR_HELM_CHART ?= helm-chart/dash0-operator

# image repository and tag to use for building/pushing the operator image
CONTROLLER_IMG_REPOSITORY ?= operator-controller
CONTROLLER_IMG_TAG ?= latest
CONTROLLER_IMG ?= $(CONTROLLER_IMG_REPOSITORY):$(CONTROLLER_IMG_TAG)
CONTROLLER_IMG_PULL_POLICY ?= Never

INSTRUMENTATION_IMG_REPOSITORY ?= instrumentation
INSTRUMENTATION_IMG_TAG ?= latest
INSTRUMENTATION_IMG ?= $(INSTRUMENTATION_IMG_REPOSITORY):$(INSTRUMENTATION_IMG_TAG)
INSTRUMENTATION_IMG_PULL_POLICY ?= Never

COLLECTOR_IMG_REPOSITORY ?= collector
COLLECTOR_IMG_TAG ?= latest
COLLECTOR_IMG ?= $(COLLECTOR_IMG_REPOSITORY):$(COLLECTOR_IMG_TAG)
COLLECTOR_IMG_PULL_POLICY ?= Never

CONFIGURATION_RELOADER_IMG_REPOSITORY ?= configuration-reloader
CONFIGURATION_RELOADER_IMG_TAG ?= latest
CONFIGURATION_RELOADER_IMG ?= $(CONFIGURATION_RELOADER_IMG_REPOSITORY):$(CONFIGURATION_RELOADER_IMG_TAG)
CONFIGURATION_RELOADER_IMG_PULL_POLICY ?= Never

FILELOG_OFFSET_SYNC_IMG_REPOSITORY ?= filelog-offset-sync
FILELOG_OFFSET_SYNC_IMG_TAG ?= latest
FILELOG_OFFSET_SYNC_IMG ?= $(FILELOG_OFFSET_SYNC_IMG_REPOSITORY):$(FILELOG_OFFSET_SYNC_IMG_TAG)
FILELOG_OFFSET_SYNC_IMG_PULL_POLICY ?= Never

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.3

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
test: go-unit-tests injector-unit-tests helm-unit-tests

.PHONY: go-unit-tests
go-unit-tests: common-package-unit-tests operator-manager-unit-tests

.PHONY: operator-manager-unit-tests
operator-manager-unit-tests: manifests generate fmt vet envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: common-package-unit-tests
common-package-unit-tests:
	go test github.com/dash0hq/dash0-operator/images/pkg/common

.PHONY: injector-unit-tests
injector-unit-tests: zig-installed
	cd images/instrumentation/injector && zig build test

.PHONY: helm-unit-tests
helm-unit-tests:
	cd helm-chart/dash0-operator && helm unittest -f 'tests/**/*.yaml' .

# Invoking ginkgo via go run makes sure we use the version from go.mod and not a version installed globally, which
# would be used when simply running `ginkgo -v test/e2e`. An alternative would be to invoke ginkgo via go test, that
# is, `go test ./test/e2e/ -v -ginkgo.v`, but that would require us to manage go test's timeout (via the -timeout
# flag), and ginkgo's own timeout.
.PHONY: test-e2e
test-e2e:
	go run github.com/onsi/ginkgo/v2/ginkgo -v test/e2e

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
GOLANGCI_LINT_VERSION ?= v1.64.8
golangci-lint-install:
	@[ -f $(GOLANGCI_LINT) ] || { \
	set -e ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) $(GOLANGCI_LINT_VERSION) ;\
	}

.PHONY: golangci-lint
golangci-lint: golangci-lint-install
	@echo "-------------------------------- (linting Go code)"
	$(GOLANGCI_LINT) run

.PHONY: golang-lint-fix
golang-lint-fix: golangci-lint-install
	$(GOLANGCI_LINT) run --fix

.PHONY: helm-chart-lint
helm-chart-lint:
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
shellcheck-lint: shellcheck-check-installed
	@echo "-------------------------------- (linting shell scripts)"
	find . -name \*.sh -not -path "./images/instrumentation/injector-experiments/third-party/*" | xargs shellcheck -x

.PHONY: prometheus-crd-version-check
prometheus-crd-version-check:
	@echo "-------------------------------- (verifying the Prometheus CRD version is in sync)"
	./test-resources/bin/prometheus-crd-version-check.sh

.PHONY: perses-crd-version-check
perses-crd-version-check:
	@echo "-------------------------------- (verifying the Perses CRD version is in sync)"
	./test-resources/bin/perses-crd-version-check.sh

.PHONY: zig-installed
zig-installed:
	@set +x
	@if ! zig version > /dev/null; then \
	echo "error: zig is not installed. Run 'brew install zig' or similar."; \
	exit 1; \
	fi

.PHONY: zig-fmt-check
zig-fmt-check: zig-installed
ifeq ("${CI}","true")
	@echo "CI: skip linting Zig source files via make lint, will run as separate Github action job step"
else
	@echo "-------------------------------- (linting Zig source files)"
	zig fmt --check images/instrumentation/injector/src
endif

.PHONY: zig-fmt
zig-fmt: zig-installed
	zig fmt images/instrumentation/injector/src

.PHONY: lint
lint: golangci-lint helm-chart-lint shellcheck-lint perses-crd-version-check prometheus-crd-version-check zig-fmt-check

.PHONY: lint-fix
lint-fix: golang-lint-fix zig-fmt

##@ Build

.PHONY: build
build: husky-setup-hooks manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: \
  docker-build-controller \
  docker-build-instrumentation \
  docker-build-collector \
  docker-build-config-reloader \
  docker-build-filelog-offset-sync ## Build all container images.

define build_container_image
$(eval $@_IMAGE_REPOSITORY = $(1))
$(eval $@_IMAGE_TAG = $(2))
$(eval $@_CONTEXT = $(3))
$(eval $@_DOCKERFILE = $(4))
if [[ -n "$($@_IMAGE_REPOSITORY)" ]]; then                                                                \
  if [[ "$($@_IMAGE_REPOSITORY)" = *"/"* ]]; then                                                         \
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
	@$(call build_container_image,$(CONTROLLER_IMG_REPOSITORY),$(CONTROLLER_IMG_TAG),.)

.PHONY: docker-build-instrumentation
docker-build-instrumentation: ## Build the instrumentation image.
	@$(call build_container_image,$(INSTRUMENTATION_IMG_REPOSITORY),$(INSTRUMENTATION_IMG_TAG),images/instrumentation)

.PHONY: docker-build-collector
docker-build-collector: ## Build the OpenTelemetry collector container image.
	@$(call build_container_image,$(COLLECTOR_IMG_REPOSITORY),$(COLLECTOR_IMG_TAG),images/collector)

.PHONY: docker-build-config-reloader
docker-build-config-reloader: ## Build the config reloader container image.
	@$(call build_container_image,$(CONFIGURATION_RELOADER_IMG_REPOSITORY),$(CONFIGURATION_RELOADER_IMG_TAG),images,images/configreloader/Dockerfile)

.PHONY: docker-build-filelog-offset-sync
docker-build-filelog-offset-sync: ## Build the filelog offset sync container image.
	@$(call build_container_image,$(FILELOG_OFFSET_SYNC_IMG_REPOSITORY),$(FILELOG_OFFSET_SYNC_IMG_TAG),images,images/filelogoffsetsync/Dockerfile)

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) --wait=false -f -

.PHONY: deploy-via-helm
deploy-via-helm: ## Deploy the controller via helm to the K8s cluster specified in ~/.kube/config.
	test-resources/bin/render-templates.sh
	helm upgrade --install \
		--namespace dash0-system \
		--create-namespace \
		--set operator.image.repository=$(CONTROLLER_IMG_REPOSITORY) \
		--set operator.image.tag=$(CONTROLLER_IMG_TAG) \
		--set operator.image.pullPolicy=$(CONTROLLER_IMG_PULL_POLICY) \
		--set operator.initContainerImage.repository=$(INSTRUMENTATION_IMG_REPOSITORY) \
		--set operator.initContainerImage.tag=$(INSTRUMENTATION_IMG_TAG) \
		--set operator.initContainerImage.pullPolicy=$(INSTRUMENTATION_IMG_PULL_POLICY) \
		--set operator.collectorImage.repository=$(COLLECTOR_IMG_REPOSITORY) \
		--set operator.collectorImage.tag=$(COLLECTOR_IMG_TAG) \
		--set operator.collectorImage.pullPolicy=$(COLLECTOR_IMG_PULL_POLICY) \
		--set operator.configurationReloaderImage.repository=$(CONFIGURATION_RELOADER_IMG_REPOSITORY) \
		--set operator.configurationReloaderImage.tag=$(CONFIGURATION_RELOADER_IMG_TAG) \
		--set operator.configurationReloaderImage.pullPolicy=$(CONFIGURATION_RELOADER_IMG_PULL_POLICY) \
		--set operator.filelogOffsetSyncImage.repository=$(FILELOG_OFFSET_SYNC_IMG_REPOSITORY) \
		--set operator.filelogOffsetSyncImage.tag=$(FILELOG_OFFSET_SYNC_IMG_TAG) \
		--set operator.filelogOffsetSyncImage.pullPolicy=$(FILELOG_OFFSET_SYNC_IMG_PULL_POLICY) \
		--set operator.developmentMode=true \
		dash0-operator \
		$(OPERATOR_HELM_CHART)

.PHONY: undeploy-via-helm
undeploy-via-helm: ## Undeploy the controller via helm from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
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
KUSTOMIZE_VERSION ?= v5.2.1
CONTROLLER_TOOLS_VERSION ?= v0.14.0

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(CONTROLLER_IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push CONTROLLER_IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push CONTROLLER_IMG=$(CATALOG_IMG)

.PHONY: husky-install
husky-install:
	test -s $(HUSKY) || GOBIN=$(LOCALBIN) GO111MODULE=on go install github.com/automation-co/husky@latest

.PHONY: husky-setup-hooks
husky-setup-hooks: husky-install
	$(HUSKY) install

