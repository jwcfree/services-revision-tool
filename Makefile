# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= $(shell cat VERSION)

CGO_ENABLED ?= 0
GOOS ?= $(shell uname -s | tr A-Z a-z)
GOARCH ?= amd64

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...


##@ Build

build: fmt vet## Build manager binary.
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o bin/services-revision-tool main.go
	cp config.json bin/config.json
	cp readme.md.tmpl bin/readme.md.tmpl

mod: ## Update go.mod file.
	@go mod tidy

vendor: mod ## Update vendored packages.
	@go mod vendor

doc-check: doc-lint doc-check-links ## Lint documentation and check links.

.PHONY: doc-lint
doc-lint:  ## Check documentation via markdownlint.
	@markdownlint-cli2 *.md

.PHONY: doc-check-links
doc-check-links:  ## Check documentation links via markdown-link-check.
	@NODE_TLS_REJECT_UNAUTHORIZED=0 markdown-link-check --config .markdown-link-check.json *.md

major:  ## Release new major version: make major release.
	$(eval VERSION_TYPE := "major")
	$(eval VERSION := $(shell echo $(VERSION) | awk -F. '/[0-9]+\./{$$1++; print $$1".0.0"}' OFS=.))
	@:

minor:  ## Release new minor version: make minor release.
	$(eval VERSION_TYPE := "minor")
	$(eval VERSION := $(shell echo $(VERSION) | awk -F. '/[0-9]+\./{$$2++; print $$1"."$$2".0"}' OFS=.))
	@:

patch:  ## Release new patch version: make patch release.
	$(eval VERSION_TYPE := "patch")
	$(eval VERSION := $(shell echo $(VERSION) | awk -F. '/[0-9]+\./{$$3++; print $$1"."$$2"."$$3}' OFS=.))
	@:

VERSION_TYPE ?= ""
release: ## Release new version: make ?type? release.
	@$(eval DATE := $(shell \date '+%Y.%m.%d'))
	@$(eval JIRA_TASK := $(shell git rev-parse --abbrev-ref HEAD | cut -d '-' -f 1,2))
	@$(eval JIRA_TASK_URL := "[$(JIRA_TASK)](https://jira.egovdev.ru/browse/$(JIRA_TASK))")

	@echo "# Make new $(VERSION_TYPE) application version $(VERSION) as part of Jira task $(JIRA_TASK)"
	@$(shell echo $(VERSION) > VERSION)

	@echo "# Check documentation"
	@$(MAKE) doc-lint

	@echo "# Check documentation links"
	@$(MAKE) doc-check-links

	@echo "# Update CHANGELOG.md"
	@if ! grep -q $(JIRA_TASK) CHANGELOG.md; then \
		sed -i "/# CHANGELOG/a \\\n$(shell printf "## %s / %s\\\n\\\n- %s " $(VERSION) $(DATE) $(JIRA_TASK_URL))" CHANGELOG.md; \
	else \
		echo "CHANGELOG.md already contains Jira Task $(JIRA_TASK)."; \
	fi

	@echo "New version $(VERSION) is ready. Please fill in CHANGELOG.md, check and commit changes."


