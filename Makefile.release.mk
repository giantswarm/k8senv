##@ CI

.PHONY: check
check: lint test-integration ## Run lint and unit tests (used by CI)

.PHONY: govulncheck
govulncheck: ## Scan for known vulnerabilities
	@command -v govulncheck >/dev/null 2>&1 || { echo "Installing govulncheck..."; go install golang.org/x/vuln/cmd/govulncheck@latest; }
	govulncheck ./...

##@ Release

.PHONY: release-dry-run
release-dry-run: ## Test the release process without publishing
	goreleaser release --snapshot --clean --skip=announce,publish,validate

.PHONY: release-dry-run-fast
release-dry-run-fast: ## Fast release dry-run for CI validation
	goreleaser release --config .goreleaser.ci.yaml --snapshot --clean --skip=announce,publish,validate
