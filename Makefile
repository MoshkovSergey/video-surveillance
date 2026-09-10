.PHONY: help env-check latest

help:
	@echo "Available targets:"
	@echo "  make env-check - check local environment"
	@echo "  make latest    - check latest component versions"

env-check:
	./scripts/check-env.sh

latest:
	./scripts/check-latest.sh