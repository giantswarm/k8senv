##@ Development

.PHONY: build
build: ## Compile the library
	@echo "Building k8senv..."
	go build -v ./...

.PHONY: fmt
fmt: ## Format Go code
	golangci-lint fmt

.PHONY: lint
lint: ## Run golangci-lint with full configuration
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run --config .golangci.yml; \
	else \
		echo "golangci-lint not installed. Run 'make install-tools'"; \
		exit 1; \
	fi

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	@golangci-lint run --config .golangci.yml --fix

.PHONY: deps
deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

.PHONY: clean
clean::
	go clean
