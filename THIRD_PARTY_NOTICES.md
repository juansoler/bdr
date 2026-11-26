# Avisos de terceros (THIRD_PARTY_NOTICES)

BDR Self-Hosted se distribuye bajo la **Apache License 2.0** (ver `LICENSE`).
Este fichero reúne los avisos de copyright y licencia del software de terceros
que se **redistribuye** dentro de los binarios, bundles e imágenes del
producto, y del que se usa **por API** sin redistribuir código.

Mantener este fichero actualizado es parte del requisito RNF-12: solo se
incorporan componentes con licencia compatible, y sus avisos viajan con cada
paquete de instalación (`LICENSE`, `NOTICE` y este fichero).

---

## 1. Binario auxiliar redistribuido

### Kopia

- Proyecto: <https://kopia.io> · <https://github.com/kopia/kopia>
- Versión fijada en `scripts/make-release.sh` (`KOPIA_VER`).
- Copyright 2015-2026 Kopia Authors.
- Licencia: **Apache License 2.0**.
- Se distribuye como binario auxiliar (`kopia` / `kopia.exe`) junto al plano de
  control, dentro de cada bundle `bdr-control-<os>-<arch>`. Se conservan sus
  avisos de copyright y licencia; el texto completo de la Apache-2.0 es el de
  `LICENSE`.

---

## 2. Dependencias Go enlazadas en los binarios

Las siguientes versiones son las que resuelve `go.mod` y quedan enlazadas en
`bdr-control`, `bdr-agent` y/o `bdr-sql`. Todas son compatibles con la
distribución de un producto Apache-2.0.

| Módulo | Versión | Licencia |
|---|---|---|
| github.com/cespare/xxhash/v2 | v2.3.0 | MIT |
| github.com/cloudsoda/go-smb2 | v0.0.0-20261105190815-95be04a983b8 | BSD-2-Clause |
| github.com/cloudsoda/sddl | v0.0.0-20261105185302-1c8f96799263 | MIT |
| github.com/coder/websocket | v1.8.15 | ISC |
| github.com/creack/pty | v1.1.24 | MIT |
| github.com/dustin/go-humanize | v1.0.1 | MIT |
| github.com/geoffgarside/ber | v1.1.0 | BSD-3-Clause |
| github.com/golang-sql/civil | v0.0.0-20220223132316-b832511892a9 | Apache-2.0 |
| github.com/golang-sql/sqlexp | v0.1.0 | BSD-3-Clause |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/hashicorp/go-uuid | v1.0.3 | MPL-2.0 |
| github.com/jackc/pgpassfile | v1.0.0 | MIT |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| github.com/jackc/pgx/v5 | v5.13.0 | MIT |
| github.com/jackc/puddle/v2 | v2.2.3 | MIT |
| github.com/jcmturner/aescts/v2 | v2.0.0 | Apache-2.0 |
| github.com/jcmturner/dnsutils/v2 | v2.0.0 | Apache-2.0 |
| github.com/jcmturner/gofork | v1.7.6 | BSD-3-Clause |
| github.com/jcmturner/goidentity/v6 | v6.0.1 | Apache-2.0 |
| github.com/jcmturner/gokrb5/v8 | v8.4.4 | Apache-2.0 |
| github.com/jcmturner/rpc/v2 | v2.0.3 | Apache-2.0 |
| github.com/klauspost/compress | v1.20.2 | Apache-2.0 |
| github.com/klauspost/cpuid/v2 | v2.4.0 | MIT |
| github.com/klauspost/crc32 | v1.3.0 | BSD-3-Clause |
| github.com/microsoft/go-mssqldb | v1.12.0 | BSD-3-Clause |
| github.com/minio/crc64nvme | v1.1.1 | Apache-2.0 |
| github.com/minio/md5-simd | v1.1.2 | Apache-2.0 (incluye código BSD-3-Clause de Go) |
| github.com/minio/minio-go/v7 | v7.4.1 | Apache-2.0 |
| github.com/philhofer/fwd | v1.2.0 | MIT |
| github.com/rs/xid | v1.6.0 | MIT |
| github.com/shopspring/decimal | v1.4.0 | MIT |
| github.com/tinylib/msgp | v1.6.4 | MIT |
| github.com/zeebo/xxh3 | v1.1.0 | BSD-2-Clause |
| go.yaml.in/yaml/v3 | v3.0.5 | MIT |
| golang.org/x/crypto | v0.57.0 | BSD-3-Clause |
| golang.org/x/net | v0.60.0 | BSD-3-Clause |
| golang.org/x/sync | v0.23.0 | BSD-3-Clause |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause |
| golang.org/x/text | v0.42.0 | BSD-3-Clause |
| gopkg.in/ini.v1 | v1.67.3 | Apache-2.0 |

Los textos completos de cada licencia están en el repositorio de cada módulo
(ruta `LICENSE` del módulo correspondiente) y en la caché de módulos de Go que
generó la compilación. Las licencias MIT, BSD, ISC y MPL-2.0 exigen conservar
el aviso de copyright original, que se mantiene intacto en el código fuente
distribuido en este repositorio.

---

## 3. Panel web (Next.js / React)

El panel se compila a ficheros estáticos y se sirve embebido en el binario del
servidor. Se redistribuyen, ya compilados, los siguientes paquetes:

| Paquete | Versión | Licencia |
|---|---|---|
| next | 15.5.29 | MIT |
| react | 19.1.10 | MIT |
| react-dom | 19.1.10 | MIT |
| @xterm/xterm | 6.0.0 | MIT |
| @xterm/addon-fit | 0.11.0 | MIT |
| @xterm/addon-web-links | 0.12.0 | MIT |

- Next.js: Copyright Meta Platforms, Inc. y Vercel, Inc. — MIT.
- React / React DOM: Copyright Meta Platforms, Inc. y afiliados — MIT.
- xterm.js: Copyright The xterm.js Authors — MIT.

Las herramientas de compilación (TypeScript, Tailwind CSS, PostCSS,
Autoprefixer) no se redistribuyen: solo generan los estáticos.

---

## 4. Fuentes autoalojadas del panel

Se redistribuyen como ficheros `woff2` embebidos en el binario:

| Fuente | Copyright | Licencia |
|---|---|---|
| Barlow Condensed | 2018 The Barlow Project Authors | SIL Open Font License 1.1 |
| IBM Plex Sans | 2018 IBM Corp. | SIL Open Font License 1.1 |
| IBM Plex Mono | 2018 IBM Corp. | SIL Open Font License 1.1 |

El texto completo de la OFL-1.1 acompaña a los ficheros en
`apps/web/fonts/OFL-BarlowCondensed.txt`, `apps/web/fonts/OFL-IBMPlexSans.txt`
y `apps/web/fonts/OFL-IBMPlexMono.txt`, y debe conservarse en cualquier
redistribución.

---

## 5. Software usado por API (no redistribuido)

- **Proxmox Backup Server** (AGPL-3.0): orquestación mediante su API REST. No
  se redistribuye código.
- **Proxmox VE** (AGPL-3.0): orquestación mediante su API REST. No se
  redistribuye código.
- **UrBackup** (AGPL-3.0+): solo referencia de diseño; no se incorpora.
- **Duplicacy**: no se usa.
