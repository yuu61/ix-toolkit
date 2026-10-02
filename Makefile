.DEFAULT_GOAL := check
# Environment setup and the fresh install must complete before their tests.
.NOTPARALLEL:

UV ?= uv
GO ?= go
GOLANGCI_LINT ?= golangci-lint
PYTHON ?= 3.14
FRESH_PYTHON ?= 3.14
PYTHON_VERSIONS ?= 3.10 3.14

# Keep each interpreter's locked dependencies separate from the usual .venv.
export UV_PROJECT_ENVIRONMENT := .make/python-$(PYTHON)
PYTHON_TEST_TARGETS := $(addprefix test-python-,$(PYTHON_VERSIONS))

.PHONY: check lint lint-python lint-go test test-python test-python-matrix \
        test-go test-fresh python-env build $(PYTHON_TEST_TARGETS)

check: lint test build test-fresh

lint: lint-python lint-go

test: test-python-matrix test-go

python-env:
	"$(UV)" sync --locked --extra dev --python "$(PYTHON)"

lint-python: python-env
	"$(UV)" run --locked --python "$(PYTHON)" ruff check src/ tests/
	"$(UV)" run --locked --python "$(PYTHON)" ruff format --check src/ tests/

lint-go:
	"$(GOLANGCI_LINT)" run ./...

test-python: python-env
	"$(UV)" run --locked --python "$(PYTHON)" python -m tests.run_checks

test-python-matrix: $(PYTHON_TEST_TARGETS)

$(PYTHON_TEST_TARGETS): test-python-%:
	$(MAKE) test-python PYTHON="$*"

test-go:
	"$(GO)" test ./...

build:
	"$(GO)" build -ldflags="-s -w" ./cmd/manualbook

test-fresh: python-env
	"$(UV)" run --locked --python "$(PYTHON)" python -m tests.check_fresh_install --uv "$(UV)" --python "$(FRESH_PYTHON)"
