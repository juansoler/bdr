#!/bin/sh
# Genera el sitio público en _site/ a partir de site/.
#
# La única variable es la versión del producto: se lee de
# packages/version/version.go y se inyecta en los marcadores __BDR_VERSION__
# y __BDR_TAG__ para que la web no se quede atrás respecto al código.
# GitHub Pages publica el resultado; nunca se commitea _site/.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version_file="$root/packages/version/version.go"
source_dir="$root/site"
out_dir="$root/_site"

version=$(sed -n 's/^var Version = "\(.*\)"$/\1/p' "$version_file")
if [ -z "$version" ]; then
  echo "build-site: no se pudo leer la versión de $version_file" >&2
  exit 1
fi

rm -rf "$out_dir"
mkdir -p "$out_dir"
cp -R "$source_dir/." "$out_dir/"

# sed -i no es portable entre BSD y GNU: se escribe a un temporal y se mueve.
find "$out_dir" -type f | while IFS= read -r file; do
  case "$file" in
    *.html|*.css|*.js|*.xml|*.txt|*.svg) ;;
    *) continue ;;
  esac
  sed -e "s/__BDR_VERSION__/$version/g" -e "s/__BDR_TAG__/v$version/g" \
    "$file" > "$file.tmp"
  mv "$file.tmp" "$file"
done

if grep -R -q '__BDR_' "$out_dir"; then
  echo "build-site: quedan marcadores sin resolver en _site/" >&2
  grep -R -n '__BDR_' "$out_dir" >&2
  exit 1
fi

echo "build-site: sitio generado en _site/ (v$version)"
