#!/usr/bin/env bash
# Compila los agentes para linux (amd64/arm64), windows (amd64) y macOS
# darwin (amd64/arm64) en dist/.
# Genera dist/SHA256SUMS. Uso: ./scripts/build-agents.sh [versión]
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
LDFLAGS="-s -w -X github.com/juansoler/bdr/packages/version.Version=$VERSION"
OUT=dist
mkdir -p "$OUT"

build() {
  local goos=$1 goarch=$2
  echo "== agente $goos/$goarch =="
  # Raspberry Pi 32-bit (armv7): GOARM=7, igual que la matriz del release CI.
  local goarm=""
  [ "$goarch" = arm ] && goarm=7
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$OUT/bdr-agent-$goos-$goarch$( [ "$goos" = windows ] && echo .exe )" \
    ./apps/agent/cmd/bdr-agent
}

build linux amd64
build linux arm64
build linux arm
build windows amd64
build darwin amd64
build darwin arm64

# Binario local (para el smoke y pruebas rápidas del propio host).
echo "== agente local ($(go env GOOS)/$(go env GOARCH)) =="
go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/bdr-agent-local" ./apps/agent/cmd/bdr-agent

echo "== sumas SHA-256 =="
( cd "$OUT" && shasum -a 256 bdr-agent-* > SHA256SUMS && cat SHA256SUMS )
echo "dist/ listo (versión $VERSION)"
