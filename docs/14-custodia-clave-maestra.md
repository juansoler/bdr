# Custodia de la clave maestra

**Documento:** 14-custodia-clave-maestra  
**Versión:** 1.0  
**Requisitos:** RF-38, RNF-11  
**Criticidad:** máxima. Sin la clave, todos los backups son irrecuperables.

---

## 1. Principio

La contraseña maestra del repositorio Kopia y las claves de cifrado de las imágenes **no pueden depender exclusivamente de PostgreSQL ni del servidor de control**. Deben existir:

1. Una copia custodiada offline.
2. Un procedimiento de recuperación documentado y probado.
3. Una rotación controlada.
4. Un simulacro periódico sin el servidor central.

No hay recuperación mágica: si se pierden todas las copias de la clave, los datos están perdidos.

---

## 2. Jerarquía de claves

```
Clave maestra del repositorio (Kopia)
        │
        ├── cifra el repositorio (AES-256-GCM/ChaCha20)
        │
        └── se protege con envelope encryption
                │
                ├── DEK por secreto
                └── KEK versionada (fuera de la BD)

Shares Shamir 3-de-5 de la clave maestra
        └── custodia física offline
```

- La **clave maestra** es la contraseña del repositorio Kopia.
- La **KEK** protege los secretos en la BD; no es la clave maestra.
- Los **shares Shamir** permiten reconstruir la clave maestra sin el servidor.
- Las claves de cifrado de imagen derivan del repositorio; no se guardan en el manifiesto.

---

## 3. Generación

Ceremonia con al menos dos personas:

1. Generar la clave maestra con un CSPRNG (32 bytes; codificación base64).
2. Crear el repositorio Kopia con esa clave.
3. Verificar que se puede abrir y crear un snapshot de prueba.
4. Dividir la clave con Shamir 3-de-5.
5. Sellar cada share en un sobre opaco, numerado y firmado.
6. Entregar los sobres a los custodios.
7. Registrar el evento en `audit_log` y en el libro de custodia.
8. Guardar una copia cifrada adicional en un medio offline separado.

**Nunca** se guardan los shares en la BD, en el repositorio ni en el panel. Solo metadatos y hash en `key_custody_shares`.

---

## 4. Custodia

| Elemento | Ubicación | Acceso |
|---|---|---|
| Share 1 | Custodio A, caja fuerte | Dos personas |
| Share 2 | Custodio B, caja fuerte | Dos personas |
| Share 3 | Custodio C, caja fuerte | Dos personas |
| Share 4 | Custodio D, ubicación separada | Dos personas |
| Share 5 | Custodio E, ubicación separada | Dos personas |
| Copia cifrada | Medio offline (USB/CD) en caja fuerte | Dos personas |
| Metadatos | PostgreSQL (`key_custody_shares`) | Admin |

Reglas:

- Ningún custodio conoce la clave completa.
- Los shares no se fotografían, no se envían por email ni se guardan en la nube.
- El libro de custodia registra quién tiene cada share, cuándo se verificó y cuándo se rotó.
- La copia cifrada adicional usa una passphrase distinta, custodiada por otra persona.

---

## 5. Break-glass

Procedimiento con regla de dos personas:

1. Declarar el incidente y registrar la hora.
2. Reunir al menos 3 shares de 5.
3. Arrancar la ISO de rescate en un host limpio, sin conexión al control plane.
4. Reconstruir la clave maestra en memoria.
5. Conectar al repositorio (NAS o S3) y verificar integridad.
6. Restaurar el snapshot necesario.
7. Reparar el arranque y verificar.
8. Destruir la clave de memoria y los temporales.
9. Rotar la clave maestra y re-repartir los shares.
10. Registrar el incidente y las lecciones.

**Nunca** se usa break-glass para operaciones rutinarias ni se guarda la clave en un archivo persistente.

---

## 6. Rotación

- Rotar tras un break-glass, una sospecha de compromiso o un cambio de custodios.
- La rotación de la clave maestra de Kopia implica crear un repositorio nuevo y migrar, o usar el mecanismo de cambio de contraseña de Kopia si existe y está verificado.
- Rotar la KEK es independiente: recifrar los secretos por lotes con `key_version`.
- Re-repartir los shares y destruir los antiguos.
- Verificar que los backups antiguos siguen siendo legibles con la clave correcta.

---

## 7. Escenarios de pérdida

| Escenario | Consecuencia | Acción |
|---|---|---|
| Se pierde un share | Redundancia 4-de-5 | Reemplazarlo y re-repartir |
| Se pierden dos shares | Quedan 3; la clave se puede reconstruir | Rotar y re-repartir |
| Se pierden tres o más | Clave irrecuperable | Declarar pérdida; los backups no se pueden abrir |
| Se compromete un share | Riesgo de reconstrucción si se comprometen 3 | Rotar clave y shares |
| Se pierde la BD | La clave sigue en los shares | Reconstruir el control plane y reconectar |
| Se pierde el servidor | La clave sigue en los shares | Break-glass desde la ISO |

La pérdida de tres shares es un escenario crítico. La probabilidad debe minimizarse con ubicaciones separadas y verificación periódica.

---

## 8. Simulacro anual

1. Elegir un snapshot de prueba.
2. Reunir 3 shares.
3. Reconstruir la clave en un host limpio.
4. Restaurar el snapshot.
5. Verificar el arranque o el contenido.
6. Medir el tiempo total.
7. Registrar el resultado y las mejoras.

El simulacro es obligatorio. Un procedimiento no probado no es un procedimiento.

---

## 9. Roles

| Rol | Responsabilidad |
|---|---|
| Propietario | Aprueba la ceremonia y la rotación |
| Custodios (5) | Guardan los shares y participan en break-glass |
| Operador | Ejecuta la restauración |
| Auditor | Revisa el registro y los simulacros |

Una misma persona no debe acumular más de un share ni aprobar y ejecutar sola un break-glass.

---

## 10. Registro

- Libro de custodia físico o cifrado.
- `key_custody_shares`: holder, índice, hash, fecha de verificación.
- `audit_log`: generación, rotación, break-glass, simulacro.
- Retención del registro según política; el propio registro debe estar respaldado.

---

## 11. Antipatrones prohibidos

- Guardar la clave maestra en el repositorio o junto a él.
- Guardar los shares en PostgreSQL, Git, email o nube.
- Usar la misma clave para el repositorio y para la BD.
- Compartir un share entre varios custodios.
- Saltarse el simulacro anual.
- Usar break-glass sin registro.
- Custodiar la clave en el mismo host del control plane.
- Asumir que «el proveedor S3 tiene la clave»: el cifrado es en cliente.

---

## 12. Criterios de aceptación

- La ceremonia de generación está documentada y ejecutada.
- 5 shares en 5 ubicaciones separadas.
- 3 de 5 reconstruyen la clave en un host limpio.
- El break-glass funciona sin el control plane.
- La rotación se ha probado al menos una vez.
- El simulacro anual está registrado.
- Ningún secreto está en la BD en claro.

---

## 13. Preguntas abiertas

- ¿Quiénes serán los 5 custodios?
- ¿Dónde se guardan físicamente los sobres?
- ¿Se usa una caja de seguridad externa?
- ¿Con qué frecuencia se rota la clave?
- ¿Quién aprueba un break-glass en ausencia del propietario?
