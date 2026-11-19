# Contribuir a BDR Self-Hosted

Gracias por tu interés. Este proyecto se acepta bajo la **Apache License 2.0**
(ver [`LICENSE`](LICENSE)); toda contribución queda sujeta a sus términos,
incluida la concesión expresa de patentes.

## Cómo proponer cambios

1. Abre un issue o discusión describiendo el problema o la propuesta.
2. Para cambios de diseño relevantes (nuevos endpoints, cambios de contrato,
   persistencia), documenta la decisión en `docs/adr/NNN-*.md`.
3. Haz fork, crea una rama y envía un PR a `main`.

### Sign-off (DCO)

Cada commit debe llevar sign-off para certificar que tienes derecho a
contribuirlo:

```bash
git commit -s   # añade "Signed-off-by: Tu Nombre <correo>"
```

## Requisitos de calidad

- `make test` en verde (`go test ./...`).
- `go vet ./...` y `gofmt -w` sin diff.
- Panel: `npm run typecheck` en `apps/web` y `npm run build` sin errores.
- Nuevos endpoints: test de contrato en `apps/server/internal/control/*_test.go`.
- Cambios de persistencia: paridad memory/file/postgres en `contract_test.go`.
- Sin referencias a marcas comerciales de terceros en código o docs.
- Sin datos reales: IPs de laboratorio, hostnames, dominios o credenciales.
  Usa rangos reservados (`203.0.113.0/24`, `198.51.100.0/24`) o `.example`.

## Estilo

- Go: gofmt, idiomatic Go, comentarios en español como el resto del código.
- Panel: TypeScript estricto, Tailwind, sin librerías nuevas salvo necesidad
  justificada en el PR.
- Commits: conventional commits (`feat:`, `fix:`, `docs:`, `chore:`…).

## Construir el panel (solo si tocas apps/web)

```bash
cd apps/web && npm install
NEXT_PUBLIC_API_BASE=  # en dev: NEXT_PUBLIC_API_BASE=http://127.0.0.1:8080 npm run dev
make web-build   # exporta a apps/server/internal/webui/out
```

Ver [`DEVELOPMENT.md`](DEVELOPMENT.md) para el resto.

## Reporte de vulnerabilidades

No lo hagas en un issue: ver [`SECURITY.md`](SECURITY.md).
