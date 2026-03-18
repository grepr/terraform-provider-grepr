default: generate lint build

BINARY_NAME=terraform-provider-grepr
VERSION?=$(shell cat LATEST_VERSION 2>/dev/null || echo 0.0.1)
OS_ARCH=$(shell go env GOOS)_$(shell go env GOARCH)
INSTALL_PATH=~/.terraform.d/plugins/registry.terraform.io/grepr-ai/grepr/$(VERSION)/$(OS_ARCH)
LINT_VERSION=v1.64.5
PROVIDER_DIR=$(shell pwd)

.PHONY: build
build:
	go build -o $(BINARY_NAME)

.PHONY: install
install: build
	mkdir -p $(INSTALL_PATH)
	cp $(BINARY_NAME) $(INSTALL_PATH)/

.PHONY: setup
setup: generate build
	@sed 's|__PROVIDER_DIR__|$(PROVIDER_DIR)|g' .terraformrc > .terraformrc.local
	@echo "Setup complete! Run terraform commands with:"
	@echo "  TF_CLI_CONFIG_FILE=$(PROVIDER_DIR)/.terraformrc.local terraform plan"
	@echo ""
	@echo "Or export it for the session:"
	@echo "  export TF_CLI_CONFIG_FILE=$(PROVIDER_DIR)/.terraformrc.local"

.PHONY: test
test:
	go test -v ./...

.PHONY: testacc
testacc:
	TF_ACC=1 go test -v ./... -timeout 120m

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, running via go run..."; \
		go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(LINT_VERSION) run; \
	fi

.PHONY: clean
clean:
	rm -f $(BINARY_NAME)
	rm -rf $(INSTALL_PATH)

.PHONY: docs
docs:
	go generate ./...

.PHONY: deps
deps:
	go mod download
	go mod tidy

.PHONY: generate
generate:
	go generate ./internal/client/generated/...

.PHONY: validate-generate
validate-generate: generate
	@git diff --exit-code internal/client/generated/ || (echo "Generated files are out of date. Run 'make generate' and commit." && exit 1)
