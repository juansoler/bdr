#!/usr/bin/env bash
# =============================================================================
# make-release.sh — genera los artefactos de una release de servidor.
#
# Entrada: binarios ya compilados de bdr-control (por plataforma) y de
# agentes (bdr-agent-*) en dist/. Salida en dist/:
#   - bdr-control-<os>-<arch>.tar.gz / .zip  (bundle: control + kopia + licencias)
#   - SHA256SUMS
#   - latest.json + latest.json.sig (firmado con BDR_RELEASE_KEY)
#
# Uso en CI (los binarios de control se compilan en el workflow):
#   BDR_RELEASE_KEY=clave.private.pem ./scripts/make-release.sh 0.8.0 \
#     bin/bdr-control-linux-amd64 bin/bdr-control-windows-amd64.exe ...
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${1:?versión requerida, p. ej. 0.8.0}
shift
OUT=dist
mkdir -p "$OUT"

KOPIA_VER=0.23.1
REPO_URL="https://github.com/juansoler/bdr/releases/download/v${VERSION#v}"
KEY=${BDR_RELEASE_KEY:?define BDR_RELEASE_KEY (ruta de la clave privada Ed25519)}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

rm -f "$OUT"/SHA256SUMS "$OUT"/latest.json "$OUT"/latest.json.sig

for binpath in "$@"; do
  bin=$(basename "$binpath")                       # bdr-control-linux-amd64[.exe]
  plat=${bin#bdr-control-}; plat=${plat%.exe}      # linux-amd64 | windows-amd64
  [[ -f "$binpath" ]] || { echo "falta $binpath" >&2; exit 1; }

  stage="$TMP/$plat"
  mkdir -p "$stage" "$TMP/kx"
  if [[ $plat = windows-* ]]; then
    cp "$binpath" "$stage/bdr-control.exe"
  else
    cp "$binpath" "$stage/bdr-control"
    chmod 0755 "$stage/bdr-control"
  fi

  # Kopia fijado por versión desde la release oficial.
  case "$plat" in
    linux-amd64)   kasset="kopia-$KOPIA_VER-linux-x64.tar.gz" ;;
    linux-arm64)   kasset="kopia-$KOPIA_VER-linux-arm64.tar.gz" ;;
    darwin-amd64)  kasset="kopia-$KOPIA_VER-macOS-x64.tar.gz" ;;
    darwin-arm64)  kasset="kopia-$KOPIA_VER-macOS-arm64.tar.gz" ;;
    windows-amd64) kasset="kopia-$KOPIA_VER-windows-x64.zip" ;;
    *) echo "plataforma no soportada: $plat" >&2; exit 1 ;;
  esac
  curl -fsSL "https://github.com/kopia/kopia/releases/download/v$KOPIA_VER/$kasset" -o "$TMP/kopia-asset"
  case "$kasset" in
    *.zip)
      unzip -o -q "$TMP/kopia-asset" -d "$TMP/kx"
      find "$TMP/kx" -name kopia.exe -type f -exec cp {} "$stage/kopia.exe" \;
      ;;
    *.tar.gz)
      tar xzf "$TMP/kopia-asset" -C "$TMP/kx"
      find "$TMP/kx" -name kopia -type f -exec cp {} "$stage/kopia" \;
      ;;
  esac
  rm -rf "$TMP/kx"
  [[ -f "$stage/kopia" || -f "$stage/kopia.exe" ]] || { echo "kopia no extraído para $plat" >&2; exit 1; }

  cp LICENSE NOTICE "$stage/"
  [[ -f THIRD_PARTY_NOTICES.md ]] && cp THIRD_PARTY_NOTICES.md "$stage/"

  echo "== bundle ${plat}…"
  if [[ $plat = windows-* ]]; then
    (cd "$stage" && zip -q -r "$OUT/bdr-control-$plat.zip" .)
    (cd "$OUT" && shasum -a 256 "bdr-control-$plat.zip" >> SHA256SUMS)
  else
    tar -C "$stage" -czf "$OUT/bdr-control-$plat.tar.gz" .
    (cd "$OUT" && shasum -a 256 "bdr-control-$plat.tar.gz" >> SHA256SUMS)
  fi
done

# Agentes del mismo directorio (si existen) para el manifiesto.
agents_json=$(python3 - "$OUT" <<'PY'
import json, sys, os
out = sys.argv[1]
agents = {}
for f in sorted(os.listdir(out)):
    if f.startswith("bdr-agent-") and not f.endswith("SUMS"):
        plat = f[len("bdr-agent-"):].removesuffix(".exe")
        agents[plat] = {"url": "", "sha256": ""}
if agents:
    print(json.dumps(agents, indent=2))
else:
    print("{}")
PY
)

# latest.json: solo url/sha de los bundles de servidor de esta release.
echo "== latest.json…"
python3 - "$OUT" "$VERSION" "$REPO_URL" "$agents_json" <<'PY'
import json, sys, os, hashlib
out, version, repo, agents_json = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
server = {}
for f in sorted(os.listdir(out)):
    if f.startswith("bdr-control-") and (f.endswith(".tar.gz") or f.endswith(".zip")):
        plat = f[len("bdr-control-"):]
        plat = plat[:-len(".tar.gz")] if plat.endswith(".tar.gz") else plat[:-len(".zip")]
        h = hashlib.sha256(open(os.path.join(out, f), "rb").read()).hexdigest()
        server[plat] = {"url": f"{repo}/{f}", "sha256": h}
if not server:
    sys.exit("sin bundles de servidor")
agents = {}
for f in sorted(os.listdir(out)):
    if f.startswith("bdr-agent-") and not f.endswith("SUMS") and "local" not in f:
        plat = f[len("bdr-agent-"):]
        plat = plat[:-4] if plat.endswith(".exe") else plat
        h = hashlib.sha256(open(os.path.join(out, f), "rb").read()).hexdigest()
        agents[plat] = {"url": f"{repo}/{f}", "sha256": h}
manifest = {
    "version": version.removeprefix("v"),
    "channel": "stable",
    "released_at": __import__("datetime").datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    "notes_url": f"https://github.com/juansoler/bdr/releases/tag/v{version.removeprefix('v')}",
    "server": server,
    "agents": agents,
}
with open(os.path.join(out, "latest.json"), "w") as fh:
    json.dump(manifest, fh, indent=2)
    fh.write("\n")
print("latest.json:", json.dumps(manifest, indent=2)[:400], "…")
PY

# SHA-256 de los agentes al final del fichero de sumas.
for f in "$OUT"/bdr-agent-*; do
  [[ -f "$f" ]] || continue
  [[ "$f" == *local* ]] && continue
  (cd "$OUT" && shasum -a 256 "$(basename "$f")" >> SHA256SUMS)
done
sort -o "$OUT/SHA256SUMS" "$OUT/SHA256SUMS"

echo "== firma del manifiesto…"
go run ./tools/update-sign sign -key "$KEY" -in "$OUT/latest.json"

echo "release lista en $OUT:"
ls -la "$OUT"
