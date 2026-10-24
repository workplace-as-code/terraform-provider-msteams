# terraform-provider-msteams Makefile

BINARY_NAME = terraform-provider-msteams
NAMESPACE   = workplace-as-code
PROVIDER    = msteams
VERSION    ?= 0.1.0
OS_ARCH    ?= $(shell go env GOOS)_$(shell go env GOARCH)
PLUGIN_DIR  = ~/.terraform.d/plugins/registry.terraform.io/$(NAMESPACE)/$(PROVIDER)/$(VERSION)/$(OS_ARCH)

TFPLUGINDOCS = go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0

default: build

## Build the provider locally
build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY_NAME)_v$(VERSION) .

## Install the provider into the local plugin directory for manual testing
install: build
	mkdir -p $(PLUGIN_DIR)
	mv $(BINARY_NAME)_v$(VERSION) $(PLUGIN_DIR)/

## Unit tests
test:
	go test ./... -v -timeout 120s

## Acceptance tests (require a real M365 tenant)
testacc:
	TF_ACC=1 go test ./... -v -timeout 600s

## Generate docs/ from the provider schema, templates/ and examples/
docs:
	$(TFPLUGINDOCS) generate --provider-name $(PROVIDER)
	$(TFPLUGINDOCS) validate --provider-name $(PROVIDER)

## Validate every example against the provider schema (needs a dev_overrides entry)
examples:
	./test/validate_examples.sh

## Code quality
lint:
	golangci-lint run ./...

## Format Go code
fmt:
	gofmt -s -w .

## Clean build artifacts
clean:
	rm -f $(BINARY_NAME)_v*
	rm -rf dist/

.PHONY: default build install test testacc lint fmt clean docs examples
