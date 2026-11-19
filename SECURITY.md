# Política de seguridad

## Versiones con soporte

| Versión | Soporte |
|---|---|
| última release estable (rama `main`) | correcciones de seguridad |
| versiones anteriores | sin soporte; actualiza a la última |

## Cómo reportar una vulnerabilidad

1. **No abras un issue público** si describe un fallo explotable.
2. Usa [Security Advisories privados](https://github.com/juansoler/bdr/security/advisories/new)
   ("Report a vulnerability") o escribe a la dirección de contacto del perfil del repositorio.
3. Incluye: versión afectada, sistema operativo, impacto estimado y pasos de
   reproducción o PoC. Adjunta logs con los secretos redactados.

Nos comprometemos a responder en un plazo de 72 horas y a mantener informado
al reportero durante el proceso. Publicamos el advisory (con crédito salvo
petición expresa) cuando existe versión corregida.

## Alcance

- API de administración y endpoints de agentes (enrolamiento, mTLS, sesiones, TOTP).
- Instaladores generados (tokens embebidos), autoactualización y verificación de firmas.
- Panel web (XSS, inyección, CSRF, fuga de token).
- Cifrado en reposo de secretos y custodia de claves.

## Fuera de alcance

- Despliegues con `--insecure-agents`, HTTP sin TLS o `.env` sin permisos.
- Especies de credenciales en repositorios de terceros (NAS/S3 mal configurados).
- Ataques que exijan acceso previo al equipo del agente o del servidor como root.
- Ausencia de firma Authenticode/Apple (limitación documentada del proyecto sin certificados de distribución).

## Buenas prácticas de despliegue (resumen)

- `BDR_SECRET_KEY` en custodia externa; sin ella los secretos cifrados no son recuperables.
- Panel detrás de TLS (Caddy u otro proxy): la API de administración en claro solo en localhost.
- mTLS de agentes activo (es el valor por defecto); `--insecure-agents` solo durante transición.
- Revisa `deploy/PRODUCTION.md` §8 antes de operar.
