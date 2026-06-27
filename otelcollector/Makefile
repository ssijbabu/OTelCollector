.PHONY: setup tidy build generate install-ocb run \
        docker-build docker-run \
        send-logs send-metrics send-traces install-telemetrygen

# ─── Single source of truth ────────────────────────────────────────────────────
# Change dist.name in manifest.yaml to rename the binary and image everywhere.
# Run 'make generate' afterwards to sync .gitignore and .dockerignore.
BINARY     := $(shell awk '/^dist:/{f=1} f && /^[ \t]+name:/{print $$2; exit}' manifest.yaml)
IMAGE_NAME := $(shell echo $(BINARY) | tr '[:upper:]' '[:lower:]')

CONFIG        := collector-config.yaml
OTLP_ENDPOINT := localhost:4317
OCB_VERSION   := 0.152.0
IMAGE_TAG     := latest

# ---------------------------------------------------------------------------
# Setup: resolve and download all dependencies
# ---------------------------------------------------------------------------
setup: tidy
	@echo "Dependencies resolved. Run 'make build' next."

tidy:
	go mod tidy

# ---------------------------------------------------------------------------
# OCB (OpenTelemetry Collector Builder) — the standard EDOT build tool.
# install-ocb: download the builder binary.
# generate: regenerate main.go and go.mod from manifest.yaml.
# ---------------------------------------------------------------------------
install-ocb:
	go install go.opentelemetry.io/collector/cmd/builder@v$(OCB_VERSION)

generate: install-ocb
	builder --config manifest.yaml --skip-compilation
	go mod tidy
	@awk 'prev~/^# Compiled binaries/&&/^[A-Za-z]/{$$0="$(BINARY)"}{prev=$$0;print}' \
	    .gitignore > .gitignore.tmp && mv .gitignore.tmp .gitignore
	@awk 'prev~/^# Compiled binary/&&/^[A-Za-z]/{$$0="$(BINARY)"}{prev=$$0;print}' \
	    Dockerfile.dockerignore > Dockerfile.dockerignore.tmp && mv Dockerfile.dockerignore.tmp Dockerfile.dockerignore
	@echo "Generated. Binary=$(BINARY)  Image=$(IMAGE_NAME)"

# ---------------------------------------------------------------------------
# Build: compile the custom collector binary.
# Use 'make generate && make build' after editing manifest.yaml.
# ---------------------------------------------------------------------------
build:
	go build -o $(BINARY) .
	@echo "Built ./$(BINARY)"

# ---------------------------------------------------------------------------
# Run: start the collector (requires EVENTHUB_CONNECTION_STRING env var)
# ---------------------------------------------------------------------------
run: build
	@if [ -z "$$EVENTHUB_NAMESPACE" ] || [ -z "$$EVENTHUB_NAME" ] || \
	    [ -z "$$EVENTHUB_SAS_KEY_NAME" ] || [ -z "$$EVENTHUB_SAS_KEY" ]; then \
		echo "ERROR: required env vars not set. Export all four before running:"; \
		echo "  export EVENTHUB_NAMESPACE=<ns>.servicebus.windows.net"; \
		echo "  export EVENTHUB_NAME=<hub-name>"; \
		echo "  export EVENTHUB_SAS_KEY_NAME=<key-name>"; \
		echo "  export EVENTHUB_SAS_KEY=<key-value>"; \
		exit 1; \
	fi
	./$(BINARY) --config $(CONFIG)

# ---------------------------------------------------------------------------
# Docker: build and run the collector as a container.
# Requires EVENTHUB_NAMESPACE, EVENTHUB_NAME, EVENTHUB_SAS_KEY_NAME, EVENTHUB_SAS_KEY.
# ---------------------------------------------------------------------------
docker-build:
	docker build --build-arg BINARY_NAME=$(BINARY) -f Dockerfile --tag $(IMAGE_NAME):$(IMAGE_TAG) ..
	@echo "Built image $(IMAGE_NAME):$(IMAGE_TAG)"

docker-run:
	@if [ -z "$$EVENTHUB_NAMESPACE" ] || [ -z "$$EVENTHUB_NAME" ] || \
	    [ -z "$$EVENTHUB_SAS_KEY_NAME" ] || [ -z "$$EVENTHUB_SAS_KEY" ]; then \
		echo "ERROR: required env vars not set."; \
		exit 1; \
	fi
	docker run --rm \
		-e EVENTHUB_NAMESPACE="$$EVENTHUB_NAMESPACE" \
		-e EVENTHUB_NAME="$$EVENTHUB_NAME" \
		-e EVENTHUB_SAS_KEY_NAME="$$EVENTHUB_SAS_KEY_NAME" \
		-e EVENTHUB_SAS_KEY="$$EVENTHUB_SAS_KEY" \
		-p 4317:4317 \
		-p 4318:4318 \
		-p 13133:13133 \
		$(IMAGE_NAME):$(IMAGE_TAG)

# ---------------------------------------------------------------------------
# Test signal senders (requires telemetrygen; see install-telemetrygen)
# The collector must already be running in another terminal.
# ---------------------------------------------------------------------------
install-telemetrygen:
	go install github.com/open-telemetry/opentelemetry-collector-contrib/cmd/telemetrygen@latest

send-logs:
	telemetrygen logs \
		--otlp-insecure \
		--otlp-endpoint $(OTLP_ENDPOINT) \
		--duration 10s \
		--rate 5 \
		--body "test log from telemetrygen"

send-metrics:
	telemetrygen metrics \
		--otlp-insecure \
		--otlp-endpoint $(OTLP_ENDPOINT) \
		--duration 10s \
		--rate 5

send-traces:
	telemetrygen traces \
		--otlp-insecure \
		--otlp-endpoint $(OTLP_ENDPOINT) \
		--duration 10s \
		--rate 5

send-all: send-logs send-metrics send-traces
