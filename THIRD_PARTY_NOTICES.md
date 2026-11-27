# Avisos de terceros (THIRD_PARTY_NOTICES)

BDR Self-Hosted se distribuye bajo la **Apache License 2.0** (texto completo en
`LICENSE`). Este fichero reúne los avisos de copyright, las condiciones y los
textos de licencia del software de terceros que se **redistribuye** dentro de
los binarios, bundles, paquetes e imágenes del producto, y del que se usa **por
API** sin redistribuir código.

Mantener este fichero actualizado es parte del requisito RNF-12: solo se
incorporan componentes con licencia compatible, y sus avisos viajan con cada
paquete de instalación (`LICENSE`, `NOTICE` y este fichero).

Las secciones 1–5 identifican cada componente y su titular; la sección 7
contiene los textos completos de las licencias aplicables.

---

## 1. Binario auxiliar redistribuido

### Kopia

- Proyecto: <https://kopia.io> · <https://github.com/kopia/kopia>
- Versión fijada en `scripts/make-release.sh` (`KOPIA_VER`).
- Copyright 2015-2026 Kopia Authors.
- Licencia: **Apache License 2.0** (texto completo en `LICENSE`).
- Se distribuye como binario auxiliar (`kopia` / `kopia.exe`) junto al plano de
  control, dentro de cada bundle `bdr-control-<os>-<arch>`. Kopia no publica
  fichero `NOTICE`, por lo que la obligación se satisface con la copia de la
  licencia.

---

## 2. Runtime de Go

Los binarios se compilan con Go y embeben su runtime.

- Copyright 2009 The Go Authors. All rights reserved.
- Licencia: **BSD-3-Clause** (texto en la sección 7.3).

---

## 3. Dependencias Go enlazadas en los binarios

Versiones resueltas por `go.mod`, enlazadas en `bdr-control`, `bdr-agent` y/o
`bdr-sql`.

### 3.1 Apache-2.0

| Módulo | Versión |
|---|---|
| github.com/golang-sql/civil | v0.0.0-20220223132316-b832511892a9 |
| github.com/jcmturner/aescts/v2 | v2.0.0 |
| github.com/jcmturner/dnsutils/v2 | v2.0.0 |
| github.com/jcmturner/goidentity/v6 | v6.0.1 |
| github.com/jcmturner/gokrb5/v8 | v8.4.4 |
| github.com/jcmturner/rpc/v2 | v2.0.3 |
| github.com/klauspost/compress | v1.20.2 |
| github.com/minio/crc64nvme | v1.1.1 |
| github.com/minio/md5-simd | v1.1.2 |
| github.com/minio/minio-go/v7 | v7.4.1 |
| gopkg.in/ini.v1 | v1.67.3 |

Texto completo: `LICENSE` (Apache-2.0). Avisos `NOTICE` de los módulos que los
publican:

**github.com/minio/minio-go/v7 — NOTICE**

```
MinIO Cloud Storage, (C) 2014-2020 MinIO, Inc.

This product includes software developed at MinIO, Inc.
(https://min.io/).

The MinIO project contains unmodified/modified subcomponents too with
separate copyright notices and license terms. Your use of the source
code for these subcomponents is subject to the terms and conditions
of Apache License Version 2.0
```

**go.yaml.in/yaml/v3 — NOTICE**

```
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

### 3.2 MIT

| Módulo | Versión | Copyright |
|---|---|---|
| github.com/cespare/xxhash/v2 | v2.3.0 | Copyright (c) 2016 Caleb Spare |
| github.com/cloudsoda/sddl | v0.0.0-20261105185302-1c8f96799263 | Copyright 2026 Diskover Data, Inc. |
| github.com/creack/pty | v1.1.24 | Copyright (c) 2011 Keith Rarick |
| github.com/dustin/go-humanize | v1.0.1 | Copyright (c) 2005-2008 Dustin Sallings |
| github.com/jackc/pgpassfile | v1.0.0 | Copyright (c) 2019 Jack Christensen |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | Copyright (c) 2020 Jack Christensen |
| github.com/jackc/pgx/v5 | v5.13.0 | Copyright (c) 2013-2021 Jack Christensen |
| github.com/jackc/puddle/v2 | v2.2.3 | Copyright (c) 2018 Jack Christensen |
| github.com/klauspost/cpuid/v2 | v2.4.0 | Copyright (c) 2015 Klaus Post |
| github.com/philhofer/fwd | v1.2.0 | Copyright (c) 2014-2015 Philip Hofer |
| github.com/rs/xid | v1.6.0 | Copyright (c) 2015 Olivier Poitrey |
| github.com/shopspring/decimal | v1.4.0 | Copyright (c) 2015 Spring, Inc. |
| github.com/tinylib/msgp | v1.6.4 | Copyright (c) 2014 Philip Hofer; porciones Copyright (c) 2009 The Go Authors |
| go.yaml.in/yaml/v3 | v3.0.5 | Copyright 2011-2016 Canonical Ltd. (ficheros portados de libyaml) |

Texto completo: sección 7.1.

### 3.3 BSD-2-Clause

| Módulo | Versión | Copyright |
|---|---|---|
| github.com/cloudsoda/go-smb2 | v0.0.0-20261105190815-95be04a983b8 | Copyright (c) 2016 Hiroshi Ioka |
| github.com/zeebo/xxh3 | v1.1.0 | Copyright (c) 2012-2014 Yann Collet; Copyright (c) 2019 Jeff Wendling |

Texto completo: sección 7.2.

### 3.4 BSD-3-Clause

| Módulo | Versión | Copyright |
|---|---|---|
| github.com/geoffgarside/ber | v1.1.0 | Copyright (c) 2009 The Go Authors |
| github.com/golang-sql/sqlexp | v0.1.0 | Copyright (c) 2017 The Go Authors |
| github.com/google/uuid | v1.6.0 | Copyright (c) 2009,2014 Google Inc. |
| github.com/jezek/xgb | v1.3.1 | Copyright (c) 2009 The XGB Authors |
| github.com/jcmturner/gofork | v1.7.6 | Copyright (c) 2009 The Go Authors |
| github.com/klauspost/crc32 | v1.3.0 | Copyright (c) 2012 The Go Authors |
| github.com/microsoft/go-mssqldb | v1.12.0 | Copyright (c) 2012 The Go Authors; Copyright (c) Microsoft Corporation |
| golang.org/x/crypto | v0.59.0 | Copyright 2009 The Go Authors |
| golang.org/x/net | v0.63.0 | Copyright 2009 The Go Authors |
| golang.org/x/sync | v0.25.0 | Copyright 2009 The Go Authors |
| golang.org/x/sys | v0.50.0 | Copyright 2009 The Go Authors |
| golang.org/x/text | v0.44.0 | Copyright 2009 The Go Authors |

Además, `github.com/minio/md5-simd` (Apache-2.0) incluye porciones de código de
Go bajo BSD-3-Clause: Copyright (c) 2009 The Go Authors.

Texto completo: sección 7.3.

### 3.5 ISC

| Módulo | Versión | Copyright |
|---|---|---|
| github.com/coder/websocket | v1.8.15 | Copyright (c) 2025 Coder |

Texto completo: sección 7.4.

### 3.6 MPL-2.0

| Módulo | Versión | Copyright |
|---|---|---|
| github.com/hashicorp/go-uuid | v1.0.3 | Copyright © 2015-2022 HashiCorp, Inc. |

El código se usa **sin modificar** como librería. Su código fuente está
disponible en <https://github.com/hashicorp/go-uuid> y en el módulo publicado
(`go mod download github.com/hashicorp/go-uuid@v1.0.3`). Se aplica la MPL-2.0
(sección 7.5).

---

## 4. Panel web (Next.js / React / xterm)

El panel se compila a ficheros estáticos y se sirve embebido en el binario del
servidor. Se redistribuyen, ya compilados, los siguientes paquetes:

| Paquete | Versión | Copyright |
|---|---|---|
| next | 15.5.29 | Copyright (c) 2025 Vercel, Inc. |
| react | 19.1.10 | Copyright (c) Meta Platforms, Inc. and affiliates. |
| react-dom | 19.1.10 | Copyright (c) Meta Platforms, Inc. and affiliates. |
| @xterm/xterm | 6.0.0 | Copyright (c) 2017-2019 The xterm.js authors; Copyright (c) 2014-2016 SourceLair Private Company; Copyright (c) 2012-2013 Christopher Jeffrey |
| @xterm/addon-fit | 0.11.0 | Copyright (c) 2019 The xterm.js authors |
| @xterm/addon-web-links | 0.12.0 | Copyright (c) 2017 The xterm.js authors |

Licencia: **MIT** (texto completo en la sección 7.1). Las herramientas de
compilación (TypeScript, Tailwind CSS, PostCSS, Autoprefixer) no se
redistribuyen: solo generan los estáticos.

---

## 5. Fuentes autoalojadas del panel

Se redistribuyen como ficheros `woff2` embebidos en el binario, sin modificar
el nombre ni el diseño de las fuentes:

| Fuente | Copyright |
|---|---|
| Barlow Condensed | Copyright 2017 The Barlow Project Authors (<https://github.com/jpt/barlow>) |
| IBM Plex Sans | Copyright © 2017 IBM Corp. with Reserved Font Name "Plex" |
| IBM Plex Mono | Copyright © 2017 IBM Corp. with Reserved Font Name "Plex" |

Licencia: **SIL Open Font License 1.1** (texto completo en la sección 7.6).
Los ficheros de licencia originales acompañan a las fuentes en el repositorio
(`apps/web/fonts/OFL-BarlowCondensed.txt`, `OFL-IBMPlexSans.txt`,
`OFL-IBMPlexMono.txt`).

La cláusula de *Reserved Font Name* solo restringe las versiones modificadas
que conserven el nombre reservado; este producto redistribuye las fuentes
originales sin renombrarlas.

---

## 6. Software usado por API (no redistribuido)

- **Proxmox Backup Server** (AGPL-3.0): orquestación mediante su API REST. No
  se redistribuye código ni se enlaza con sus librerías.
- **Proxmox VE** (AGPL-3.0): orquestación mediante su API REST. No se
  redistribuye código ni se enlaza con sus librerías.

---

## 7. Textos completos de licencia

### 7.1 MIT License

```
Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

### 7.2 BSD 2-Clause License

```
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### 7.3 BSD 3-Clause License

```
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of the copyright holder nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### 7.4 ISC License

```
Permission to use, copy, modify, and distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

### 7.5 Mozilla Public License 2.0

El módulo `github.com/hashicorp/go-uuid` se distribuye bajo la MPL-2.0, sin
modificar. Su código fuente está disponible en
<https://github.com/hashicorp/go-uuid> y el texto completo de la licencia en
<https://mozilla.org/MPL/2.0/>.

Aviso exigido por la sección 3.1 de la MPL-2.0: el código fuente de las partes
cubiertas por la MPL-2.0 está disponible en la URL indicada y se puede obtener
también con `go mod download github.com/hashicorp/go-uuid@v1.0.3`. No se ha
modificado ningún fichero cubierto por esta licencia.

### 7.6 SIL Open Font License 1.1

```
SIL OPEN FONT LICENSE Version 1.1 - 26 February 2007

PREAMBLE
The goals of the Open Font License (OFL) are to stimulate worldwide
development of collaborative font projects, to support the font creation
efforts of academic and linguistic communities, and to provide a free and
open framework in which fonts may be shared and improved in partnership
with others.

The OFL allows the licensed fonts to be used, studied, modified and
redistributed freely as long as they are not sold by themselves. The
fonts, including any derivative works, can be bundled, embedded,
redistributed and/or sold with any software provided that any reserved
names are not used by derivative works. The fonts and derivatives,
however, cannot be released under any other type of license. The
requirement for fonts to remain under this license does not apply
to any document created using the fonts or their derivatives.

DEFINITIONS
"Font Software" refers to the set of files released by the Copyright
Holder(s) under this license and clearly marked as such. This may
include source files, build scripts and documentation.

"Reserved Font Name" refers to any names specified as such after the
copyright statement(s).

"Original Version" refers to the collection of Font Software components as
distributed by the Copyright Holder(s).

"Modified Version" refers to any derivative made by adding to, deleting,
or substituting -- in part or in whole -- any of the components of the
Original Version, by changing formats or by porting the Font Software to a
new environment.

"Author" refers to any designer, engineer, programmer, technical
writer or other person who contributed to the Font Software.

PERMISSION & CONDITIONS
Permission is hereby granted, free of charge, to any person obtaining
a copy of the Font Software, to use, study, copy, merge, embed, modify,
redistribute, and sell modified and unmodified copies of the Font
Software, subject to the following conditions:

1) Neither the Font Software nor any of its individual components,
in Original or Modified Versions, may be sold by itself.

2) Original or Modified Versions of the Font Software may be bundled,
redistributed and/or sold with any software, provided that each copy
contains the above copyright notice and this license. These can be
included either as stand-alone text files, human-readable headers or
in the appropriate machine-readable metadata fields within text or
binary files as long as those fields can be easily viewed by the user.

3) No Modified Version of the Font Software may use the Reserved Font
Name(s) unless explicit written permission is granted by the corresponding
Copyright Holder. This restriction only applies to the primary font name as
presented to the users.

4) The name(s) of the Copyright Holder(s) or the Author(s) of the Font
Software shall not be used to promote, endorse or advertise any
Modified Version, except to acknowledge the contribution(s) of the
Copyright Holder(s) and the Author(s) or with their explicit written
permission.

5) The Font Software, modified or unmodified, in part or in whole,
must be distributed entirely under this license, and must not be
distributed under any other license. The requirement for fonts to
remain under this license does not apply to any document created
using the Font Software.

TERMINATION
This license becomes null and void if any of the above conditions are
not met.

DISCLAIMER
THE FONT SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO ANY WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT
OF COPYRIGHT, PATENT, TRADEMARK, OR OTHER RIGHT. IN NO EVENT SHALL THE
COPYRIGHT HOLDER BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
INCLUDING ANY GENERAL, SPECIAL, INDIRECT, INCIDENTAL, OR CONSEQUENTIAL
DAMAGES, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF THE USE OR INABILITY TO USE THE FONT SOFTWARE OR FROM
OTHER DEALINGS IN THE FONT SOFTWARE.
```
