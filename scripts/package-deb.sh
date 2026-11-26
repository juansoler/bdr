#!/usr/bin/env bash
# Empaqueta un .deb del agente a partir de dist/bdr-agent-linux-amd64 (o arm64).
# Usa solo ar y tar, así que funciona también en macOS.
# Uso: ./scripts/package-deb.sh [amd64|arm64] [versión]
set -euo pipefail
cd "$(dirname "$0")/.."

ARCH=${1:-amd64}
VERSION=${2:-$(git describe --tags --always --dirty 2>/dev/null || echo "0.4.0")}
VERSION_DEB=${VERSION#v}
DEB="dist/bdr-agent_${VERSION_DEB}_${ARCH}.deb"
STAGING=$(mktemp -d)
trap 'rm -rf "$STAGING"' EXIT

BIN="dist/bdr-agent-linux-$ARCH"
[[ -f "$BIN" ]] || { echo "falta $BIN; ejecuta antes scripts/build-agents.sh" >&2; exit 1; }

# Árbol del paquete.
mkdir -p "$STAGING/DEBIAN" \
  "$STAGING/usr/bin" \
  "$STAGING/lib/systemd/system" \
  "$STAGING/share/doc/bdr-agent"

install -m 0755 "$BIN" "$STAGING/usr/bin/bdr-agent"
cp deploy/agent/linux/bdr-agent.service "$STAGING/lib/systemd/system/"

cat > "$STAGING/DEBIAN/control" <<EOF
Package: bdr-agent
Version: $VERSION_DEB
Section: admin
Priority: optional
Architecture: $ARCH
Maintainer: BDR Self-Hosted <bdr@localhost>
Depends: systemd | systemd-tmpfiles
Description: Agente de copia de seguridad de BDR Self-Hosted
  Se enrola contra el plano de control con un token de un solo uso,
  publica heartbeat e inventario y ejecuta los trabajos de backup y
  restauración con Kopia.
EOF

cat > "$STAGING/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
getent passwd bdr-agent >/dev/null || adduser --system --group --home /var/lib/bdr-agent bdr-agent
mkdir -p /var/lib/bdr-agent
chmod 0700 /var/lib/bdr-agent
chown bdr-agent:bdr-agent /var/lib/bdr-agent
systemctl daemon-reload || true
echo "Para enrolar el agente:"
echo "  sudo bdr-agent register --server http://SERVIDOR:8080 --token TOKEN"
echo "  sudo systemctl enable --now bdr-agent"
EOF

cat > "$STAGING/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
  systemctl stop bdr-agent || true
  systemctl disable bdr-agent || true
fi
EOF

cat > "$STAGING/DEBIAN/conffiles" <<'EOF'
EOF

# Licencia y avisos completos: el .deb es una redistribución, así que la
# Apache-2.0 exige acompañar la copia de la licencia y el NOTICE.
install -m 0644 LICENSE NOTICE THIRD_PARTY_NOTICES.md "$STAGING/share/doc/bdr-agent/"

cat > "$STAGING/share/doc/bdr-agent/copyright" <<EOF
BDR Self-Hosted — agente $VERSION_DEB

Copyright 2026 BDR Self-Hosted contributors.
Distribuido bajo la Apache License 2.0 (texto completo en LICENSE).

Este paquete incluye software de terceros; sus avisos de copyright y
licencia están en NOTICE y THIRD_PARTY_NOTICES.md.
EOF

chmod 755 "$STAGING/DEBIAN/postinst" "$STAGING/DEBIAN/prerm"

( cd "$STAGING" && tar --owner=0 --group=0 -czf debian.tar.gz DEBIAN )
( cd "$STAGING" && tar --owner=0 --group=0 -czf data.tar.gz usr lib share )
DEB_ABS="$PWD/$DEB"
mkdir -p "$(dirname "$DEB_ABS")"

# .deb = ar con debian-binary primero. El ar de BSD (macOS) añade tablas de
# símbolos incompatibles, así que se escribe el formato directamente.
printf '2.0\n' > "$STAGING/debian-binary"
python3 - "$DEB_ABS" "$STAGING/debian-binary" "$STAGING/debian.tar.gz" "$STAGING/data.tar.gz" <<'PYEOF'
import sys, os

def write_ar(out_path, members):
    with open(out_path, "wb") as f:
        f.write(b"!<arch>\n")
        for name, path in members:
            data = open(path, "rb").read()
            hdr = f"{name:<16}{0:<12}{0:<6}{0:<6}{420:<8}{len(data):<10}`\n"
            assert len(hdr) == 60, len(hdr)
            f.write(hdr.encode("ascii"))
            f.write(data)
            if len(data) % 2:
                f.write(b"\n")

out, *files = sys.argv[1:]
write_ar(out, [("debian-binary", files[0]), ("control.tar.gz", files[1]), ("data.tar.gz", files[2])])
PYEOF
echo "paquete: $DEB"
