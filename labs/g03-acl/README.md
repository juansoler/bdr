# G0.3 — ACL del Repository Server

**Objetivo:** comprobar si un agente puede listar o restaurar snapshots de otro, cuánto tarda la revocación y si la conexión no interactiva es viable.

**Decide:** mantener un único repositorio con dominio de confianza o separar repositorios por grupo (riesgo R-14).

---

## 1. Arrancar el servidor

En una VM de laboratorio, con un repositorio temporal:

```bash
export KOPIA_PASSWORD="$(openssl rand -base64 24)"
export KOPIA_SERVER_CONTROL_PASSWORD="$(openssl rand -base64 24)"
mkdir -p ~/g03/repo ~/g03/server

kopia repository create filesystem --path=~/g03/repo

kopia server start \
  --config-file=~/g03/server/server.config \
  --tls-generate-cert \
  --tls-cert-file=~/g03/server/kopia.crt \
  --tls-key-file=~/g03/server/kopia.key \
  --address=127.0.0.1:51515 \
  --server-control-username=control
```

Anota la huella TLS que imprime el servidor.

---

## 2. Crear usuarios y ACL

En otra terminal, con `KOPIA_PASSWORD` y `KOPIA_SERVER_CONTROL_PASSWORD`:

```bash
kopia server user add agent-a@lab
kopia server user add agent-b@lab
kopia server acl enable
```

Asigna a `agent-a@lab` acceso `APPEND` sobre su host y deja `agent-b@lab` con su propio espacio. La sintaxis exacta de las etiquetas es una decisión pendiente (P-22); anota la que funcione.

Conecta dos clientes:

```bash
kopia repository connect server \
  --url=https://127.0.0.1:51515 \
  --server-cert-fingerprint=<huella> \
  --override-username=agent-a@lab \
  --config-file=~/g03/a.config

kopia repository connect server \
  --url=https://127.0.0.1:51515 \
  --server-cert-fingerprint=<huella> \
  --override-username=agent-b@lab \
  --config-file=~/g03/b.config
```

**P-17:** si la conexión pide la contraseña del servidor de forma interactiva, documenta el método no interactivo que funcione (variables de entorno, flags o archivo de configuración). No asumas que `KOPIA_PASSWORD` sirve para el usuario del servidor.

---

## 3. Crear un snapshot de B

```bash
mkdir -p ~/g03/b-data
echo "secreto de B" > ~/g03/b-data/b.txt
kopia snapshot create ~/g03/b-data --config-file=~/g03/b.config --json
```

Anota el `id` del snapshot.

---

## 4. Ejecutar la comprobación

```bash
CFG_A=~/g03/a.config \
CFG_B=~/g03/b.config \
B_SNAPSHOT_ID=<id-del-snapshot-de-B> \
./acl-check.sh
```

El script:

1. Lista snapshots de A.
2. Comprueba si A ve el snapshot de B.
3. Intenta restaurarlo con A.
4. Crea un snapshot propio con A.
5. Escribe `g03-summary.md` en `labs/evidence/`.

---

## 5. Revocación

1. Anota la hora.
2. `kopia server user delete agent-a@lab` o revoca la ACL.
3. Ejecuta `kopia server refresh --address=... --server-username=control --server-password=...`.
4. Intenta una operación con A y mide cuánto tarda en fallar.
5. Repite sin refresh para medir el retardo de 5–10 minutos.

| Escenario | Tiempo hasta denegar |
|---|---|
| Con `server refresh` | |
| Sin refresh | |

---

## 6. Criterios de aceptación

- A no puede listar ni restaurar el snapshot de B, o el riesgo se acepta y se documenta.
- A puede crear su propio snapshot.
- La revocación es efectiva en un tiempo conocido.
- La conexión no interactiva está documentada.
- La sintaxis de ACL utilizada está registrada.

---

## 7. Evidencia

- `g03-a-list.json`, `g03-a-all.json`, `g03-a-sees-b.txt`.
- `g03-a-restore-b.txt` con código de salida.
- `g03-a-create.json` y `.exit`.
- `g03-summary.md`.
- Tiempos de revocación.
- Conclusión sobre el modelo de confianza y actualización de `ADR-0002` si aplica.
