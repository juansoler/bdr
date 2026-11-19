BIN := bin
GOFLAGS ?=

.PHONY: all build test vet fmt smoke smoke-postgres smoke-krs smoke-web web-install web-build web-dev run-server clean
all: build test

build:
	mkdir -p $(BIN)
	go build $(GOFLAGS) -o $(BIN)/bdr-control ./apps/server/cmd/bdr-control
	go build $(GOFLAGS) -o $(BIN)/bdr-agent ./apps/agent/cmd/bdr-agent

test:
	go test $(GOFLAGS) ./...

vet:
	go vet ./...

fmt:
	gofmt -w apps packages

smoke: build
	./scripts/smoke.sh

# E2E completo con el estado en PostgreSQL (requiere docker).
smoke-postgres: build
	BDR_SMOKE_STORE=postgres ./scripts/smoke.sh

# El mismo E2E con un Kopia Repository Server real (aislamiento por agente).
# Requiere `kopia` en el PATH.
smoke-krs: build
	BDR_SMOKE_KRS=yes ./scripts/smoke.sh

smoke-web: build
	./scripts/smoke-web.sh

web-install:
	cd apps/web && npm install --no-audit --no-fund

web-build:
	cd apps/web && npm ci --no-audit --no-fund && npm run build
	rm -rf apps/server/internal/webui/out
	mkdir -p apps/server/internal/webui/out
	cp -R apps/web/out/. apps/server/internal/webui/out/
	@echo "panel exportado a apps/server/internal/webui/out"

web-dev:
	cd apps/web && NEXT_PUBLIC_API_BASE=http://127.0.0.1:8080 npm run dev

run-server: build
	BDR_ADMIN_TOKEN=dev-admin-token ./$(BIN)/bdr-control --dev --addr 127.0.0.1:8080

clean:
	rm -rf $(BIN)
