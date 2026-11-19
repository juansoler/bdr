#!/usr/bin/env bash
# Backup y restauración de la base de datos del plano de control.
#
# Uso:
#   ./scripts/backup-control-db.sh                 # backup ahora (compose prod)
#   ./scripts/backup-control-db.sh restore FILE    # restaurar una copia (¡para el control!)
#
# Variables (por defecto, el stack de deploy/docker-compose.prod.yml):
#   BDR_DB_CONTAINER=${BDR_DB_CONTAINER:-$(docker compose -f deploy/docker-compose.prod.yml ps -q db 2>/dev/null)}
#   BDR_DB_USER / BDR_DB_NAME / BDR_DB_PASSWORD
#   BDR_BACKUP_DIR (default ./data/db-backups)
#
# La restauración exige que bdr-control esté parado: el documento de estado y
# la auditoría se sustituyen completos.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

COMPOSE="docker compose -f deploy/docker-compose.prod.yml"
DB_USER=${BDR_DB_USER:-bdr}
DB_NAME=${BDR_DB_NAME:-bdr}
DB_PASSWORD=${BDR_DB_PASSWORD:-}
BACKUP_DIR=${BDR_BACKUP_DIR:-./data/db-backups}

container() {
  local id
  id=$($COMPOSE ps -q db 2>/dev/null | head -1)
  if [[ -z "$id" ]]; then
    echo "no encuentro el contenedor db del stack de producción; ¿está levantado?" >&2
    exit 1
  fi
  echo "$id"
}

backup() {
  local cid file
  cid=$(container)
  mkdir -p "$BACKUP_DIR"
  file="$BACKUP_DIR/bdr-$(date +%Y%m%d-%H%M).dump"
  docker exec "$cid" pg_dump -U "$DB_USER" -Fc "$DB_NAME" >"$file"
  [[ -s "$file" ]] || { echo "pg_dump produjo un fichero vacío" >&2; exit 1; }
  # Retención: conserva las 14 más recientes.
  ls -1t "$BACKUP_DIR"/*.dump 2>/dev/null | tail -n +15 | xargs -r rm -f
  echo "backup creado: $file ($(du -h "$file" | cut -f1))"
}

restore() {
  local file=$1 cid
  [[ -f "$file" ]] || { echo "no existe $file" >&2; exit 1; }
  cid=$(container)
  echo "ATENCIÓN: se sustituirá por completo la base '$DB_NAME'."
  echo "Para bdr-control antes de continuar (docker compose -f deploy/docker-compose.prod.yml stop control)."
  read -r -p "¿Continuar? escribe 'restaurar': " answer
  [[ "$answer" == "restaurar" ]] || { echo "cancelado"; exit 1; }
  docker exec "$cid" psql -U "$DB_USER" -d postgres \
    -c "DROP DATABASE IF EXISTS $DB_NAME WITH (FORCE)" \
    -c "CREATE DATABASE $DB_NAME"
  docker exec -i "$cid" pg_restore -U "$DB_USER" --no-owner -d "$DB_NAME" <"$file"
  echo "restaurada. Levanta el control de nuevo:"
  echo "  $COMPOSE up -d control"
}

case "${1:-backup}" in
  backup)
    backup
    ;;
  restore)
    [[ -n "${2:-}" ]] || { echo "falta el fichero: $0 restore FILE" >&2; exit 1; }
    restore "$2"
    ;;
  *)
    echo "uso: $0 [backup|restore FILE]" >&2
    exit 2
    ;;
esac
