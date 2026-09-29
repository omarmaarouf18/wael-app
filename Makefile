.PHONY: setup ci ensure-hooks commit push since-last-report report-hash contract-test e2e

ensure-hooks:
	@if [ "$$(git config --get core.hooksPath 2>/dev/null)" != ".githooks" ]; then \
		git config core.hooksPath .githooks; \
		echo "[MAKE] Configured git core.hooksPath to .githooks"; \
	fi

setup: ensure-hooks
	@echo "Git hooks path set to .githooks/"
	@chmod +x .githooks/pre-push

ci: ensure-hooks
	./.githooks/pre-push

contract-test: ensure-hooks
	cd tests/contracts && go test -v -count=1 ./...

e2e: ensure-hooks
	cd tests/e2e && go test -v -count=1 ./...

commit: ensure-hooks
	git add -A && git commit -m "$(MSG)"

push: ensure-hooks
	@BRANCH=$$(git rev-parse --abbrev-ref HEAD); \
	LOCAL=$$(git rev-parse HEAD); \
	git push origin $$BRANCH; \
	REMOTE=$$(git ls-remote origin $$BRANCH | awk '{print $$1}'); \
	if [ "$$LOCAL" = "$$REMOTE" ]; then \
		echo "$$LOCAL" > .last_push_verified; \
		echo "PUSH_VERIFIED: $$LOCAL"; \
	else \
		echo "PUSH_MISMATCH: local=$$LOCAL remote=$$REMOTE"; \
		exit 1; \
	fi

report-hash:
	@if [ ! -f .last_push_verified ]; then \
		echo "ERROR: no .last_push_verified file — run 'make push' first"; \
		exit 1; \
	fi; \
	cat .last_push_verified

since-last-report:
	@if [ -z "$(SINCE)" ]; then \
		echo "Usage: make since-last-report SINCE=<commit-sha>"; \
		exit 1; \
	fi; \
	git log --oneline $(SINCE)..HEAD --stat
