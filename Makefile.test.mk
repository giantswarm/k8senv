COVERAGE_FILE = coverage.out

# Test options (set on command line, e.g. make test-integration RACE=1)
LOG_LEVEL       ?= INFO
RACE            ?=
NOCACHE         ?=
TEST            ?=
PARALLEL        ?= 8
STRESS_SUBTESTS ?=

# Common test flags shared by both phases.
_TEST_FLAGS = -tags=integration -parallel=$(PARALLEL) $(if $(RACE),-race) $(if $(NOCACHE),-count=1) -v $(if $(TEST),-run $(TEST)) -timeout=15m

##@ Testing

.PHONY: test-unit
test-unit: ## Run unit tests (options: RACE=1, NOCACHE=1, TEST=pattern, LOG_LEVEL=DEBUG)
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) go test $(if $(RACE),-race) $(if $(NOCACHE),-count=1) -v $(if $(TEST),-run $(TEST)) ./...

.PHONY: test-integration
test-integration: ## Run integration tests (options: RACE=1, NOCACHE=1, TEST=pattern, LOG_LEVEL=DEBUG)
	@echo "Note: Requires kine and kube-apiserver binaries"
	@echo "Phase 1: non-stress packages"
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) go test $(_TEST_FLAGS) $$(go list -tags=integration ./... | grep -v '/tests/stress$$')
	@echo "Phase 2: stress packages"
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) $(if $(STRESS_SUBTESTS),K8SENV_STRESS_SUBTESTS=$(STRESS_SUBTESTS)) go test $(_TEST_FLAGS) ./tests/stress

.PHONY: test-stress
test-stress: ## Run stress tests (options: RACE=1, NOCACHE=1, TEST=pattern, STRESS_SUBTESTS=N, LOG_LEVEL=DEBUG)
	@echo "Note: Requires kine and kube-apiserver binaries"
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) $(if $(STRESS_SUBTESTS),K8SENV_STRESS_SUBTESTS=$(STRESS_SUBTESTS)) go test $(_TEST_FLAGS) ./tests/stress

.PHONY: test
test: test-unit test-integration ## Run all tests (unit + integration)

.PHONY: coverage
coverage: ## Generate test coverage report (requires kine and kube-apiserver)
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) go test -tags=integration -coverprofile=$(COVERAGE_FILE) -coverpkg=./... ./...
	@echo "Coverage report saved to $(COVERAGE_FILE)"
	@echo "View with: go tool cover -html=$(COVERAGE_FILE)"

.PHONY: coverage-html
coverage-html: coverage ## Generate and open coverage report in browser
	go tool cover -html=$(COVERAGE_FILE)
