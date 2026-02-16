# k8senv Makefile

include Makefile.*.mk

.DEFAULT_GOAL := help

.PHONY: clean
clean:: ## Remove build artifacts and temp directories

.PHONY: help
help: ## Show this help message
	@awk '/^##@/ { printf "\n\033[0;32m%s\033[0m\n", substr($$0, 5) } \
		/^[a-zA-Z_-]+::?.*?## / { name=$$1; sub(/:+$$/, "", name); printf "  \033[0;36m%-22s\033[0m %s\n", \
		name, substr($$0, index($$0, "## ") + 3) }' $(MAKEFILE_LIST)
