##@ Utilities

KUBE_APISERVER_VERSION ?= v1.35.0
GOLANGCI_LINT_VERSION  ?= v2.1.6
CAPI_VERSION           ?= v1.11.2

.PHONY: install-tools
install-tools: ## Install required external tools (kine, kube-apiserver, golangci-lint, pkgsite)
	@echo "Installing kine..."
	go install github.com/k3s-io/kine/cmd/kine@latest
	@echo "Installing kube-apiserver $(KUBE_APISERVER_VERSION)..."
	@GOBIN=$$(go env GOPATH)/bin && \
		curl -fsSL "https://dl.k8s.io/$(KUBE_APISERVER_VERSION)/bin/$$(go env GOOS)/$$(go env GOARCH)/kube-apiserver" -o "$$GOBIN/kube-apiserver" && \
		chmod +x "$$GOBIN/kube-apiserver"
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "Installing pkgsite..."
	go install golang.org/x/pkgsite/cmd/pkgsite@latest
	@echo "All tools installed!"

.PHONY: docs
docs: ## Serve project documentation locally (requires pkgsite)
	@if ! command -v pkgsite > /dev/null; then \
		echo "pkgsite not installed. Run 'make install-tools'"; \
		exit 1; \
	fi
	@echo "Documentation available at: http://localhost:8080/github.com/giantswarm/k8senv"
	@pkgsite -http=:8080 .

.PHONY: download-crds
download-crds: ## Download all CRDs from upstream
	@CAPI_VERSION=$(CAPI_VERSION) ./scripts/download-crds.sh --all

.PHONY: download-capi-crds
download-capi-crds: ## Download CAPI core CRDs
	@CAPI_VERSION=$(CAPI_VERSION) ./scripts/download-crds.sh capi

.PHONY: clean-crds
clean-crds: ## Remove downloaded CRDs
	rm -f crds/*.yaml
