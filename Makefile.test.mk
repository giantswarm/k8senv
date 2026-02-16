COVERAGE_FILE = coverage.out

# Test options (set on command line, e.g. make test-integration RACE=1)
LOG_LEVEL       ?= INFO
RACE            ?=
NOCACHE         ?=
TEST            ?=
PARALLEL        ?= 8
STRESS_SUBTESTS ?=
FLAKY_LOG       ?= find-flaky.log

##@ Testing

.PHONY: test-unit
test-unit: ## Run unit tests (options: RACE=1, NOCACHE=1, TEST=pattern, LOG_LEVEL=DEBUG)
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) go test $(if $(RACE),-race) $(if $(NOCACHE),-count=1) -v $(if $(TEST),-run $(TEST)) ./...

.PHONY: test-integration
test-integration: ## Run integration tests (options: RACE=1, NOCACHE=1, TEST=pattern, LOG_LEVEL=DEBUG)
	@echo "Note: Requires kine and kube-apiserver binaries"
	K8SENV_LOG_LEVEL=$(LOG_LEVEL) $(if $(STRESS_SUBTESTS),K8SENV_STRESS_SUBTESTS=$(STRESS_SUBTESTS)) go test -tags=integration -parallel=$(PARALLEL) $(if $(RACE),-race) $(if $(NOCACHE),-count=1) -v $(if $(TEST),-run $(TEST)) -timeout=7m ./...

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

.PHONY: find-flaky
find-flaky: ## Run integration tests in loop to find flaky tests (options: RACE=1, TEST=pattern, PARALLEL=8)
	@set -euo pipefail; \
	race_flag="$(if $(RACE),-race)"; \
	run_flag="$(if $(TEST),-run $(TEST))"; \
	stress="$(if $(STRESS_SUBTESTS),$(STRESS_SUBTESTS),5000)"; \
	cyan='\033[0;36m'; green='\033[0;32m'; red='\033[0;31m'; nc='\033[0m'; \
	rcfile=$$(mktemp); \
	trap 'rm -f "$$rcfile"' EXIT; \
	: > "$(FLAKY_LOG)"; \
	printf "$${green}Running integration tests in a loop to find flaky tests...$${nc}\n"; \
	printf "$${cyan}Press Ctrl+C to stop. Log: $(FLAKY_LOG)$${nc}\n"; \
	printf "$${cyan}Configuration:$${nc}\n"; \
	printf "$${cyan}  PARALLEL        = $(PARALLEL)$${nc}\n"; \
	printf "$${cyan}  LOG_LEVEL       = $(LOG_LEVEL)$${nc}\n"; \
	printf "$${cyan}  RACE            = $(if $(RACE),1 (enabled),0 (disabled))$${nc}\n"; \
	printf "$${cyan}  TEST            = $(if $(TEST),$(TEST),<all>)$${nc}\n"; \
	printf "$${cyan}  FLAKY_LOG       = $(FLAKY_LOG)$${nc}\n"; \
	printf "$${cyan}  STRESS_SUBTESTS = %s$${nc}\n" "$$stress"; \
	i=1; \
	while true; do \
		(K8SENV_LOG_LEVEL="$(LOG_LEVEL)" K8SENV_STRESS_SUBTESTS="$$stress" \
			go test -tags=integration -parallel="$(PARALLEL)" $$race_flag -count=1 -v $$run_flag -timeout=7m ./... 2>&1; echo $$? > "$$rcfile") | tee -a "$(FLAKY_LOG)"; \
		if [ "$$(cat "$$rcfile")" != "0" ]; then \
			printf "$${red}Failed on pass %d$${nc}\n" "$$i" | tee -a "$(FLAKY_LOG)"; \
			exit 1; \
		fi; \
		printf "$${green}Pass %d succeeded$${nc}\n" "$$i" | tee -a "$(FLAKY_LOG)"; \
		i=$$((i + 1)); \
	done

.PHONY: find-all-flaky
find-all-flaky: ## Run find-flaky with per-iteration logs (options: PARALLEL=8)
	@set -euo pipefail; \
	cyan='\033[0;36m'; green='\033[0;32m'; nc='\033[0m'; \
	printf "$${green}Running find-flaky continuously with per-iteration logs...$${nc}\n"; \
	printf "$${cyan}Press Ctrl+C to stop.$${nc}\n"; \
	i=1; \
	while true; do \
		logfile=$$(printf 'flaky-tests-%03d.log' "$$i"); \
		printf "$${cyan}--- Iteration %d → %s ---$${nc}\n" "$$i" "$$logfile"; \
		$(MAKE) find-flaky \
			FLAKY_LOG="$$logfile" \
			LOG_LEVEL=$(if $(filter-out file,$(origin LOG_LEVEL)),$(LOG_LEVEL),DEBUG) \
			PARALLEL=$(PARALLEL) \
			|| true; \
		i=$$((i + 1)); \
	done

.PHONY: clean
clean::
	rm -f $(COVERAGE_FILE) find-flaky.log flaky-tests-*.log
	rm -rf /tmp/k8senv* 2>/dev/null || true
	rm -rf /tmp/nix-shell-*/k8senv 2>/dev/null || true
