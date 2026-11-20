// Panel web embebido. El export estático de apps/web se copia a webui/out
// (make web-build) y se sirve desde el propio binario: servidor y panel
// comparten origen, sin Node ni CORS en producción.
//
// Si el export no se ha compilado, out/index.html contiene una página de
// placeholder que lo indica y las instrucciones de compilación.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

//go:embed all:out
var embedded embed.FS

// FS devuelve el sistema de ficheros del panel con el índice como entrada.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "out")
	if err != nil {
		return nil
	}
	return sub
}

// Handler sirve el panel con fallback SPA: las rutas que no corresponden a
// un fichero (p. ej. /agents) devuelven index.html para el router de cliente.
// Las rutas de API y de agentes no pasan por aquí: se registran antes en el mux.
func Handler() http.Handler {
	sub := FS()
	if sub == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "panel no compilado", http.StatusNotImplemented)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			// El export de Next sin trailingSlash genera agents.html (no
			// /agents/index.html): probamos <ruta>.html antes de caer al
			// índice, que llegaría a pintar la portada hasta que el router
			// de cliente recuperara la página correcta.
			if htmlPath := path + ".html"; htmlPath != "index.html" {
				if _, err := fs.Stat(sub, htmlPath); err == nil {
					path = htmlPath
				}
			}
		}
		if _, err := fs.Stat(sub, path); err != nil {
			// Sin fichero: devolvemos el índice (SPA) salvo en peticiones
			// de assets, donde un 404 real evita servir HTML por error.
			if !strings.HasPrefix(path, "_next/") {
				index, statErr := fs.ReadFile(sub, "index.html")
				if statErr == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Header().Set("Cache-Control", "no-store")
					_, _ = w.Write(index)
					return
				}
			}
			http.NotFound(w, r)
			return
		}
		// Assets con hash: cacheables.
		if strings.HasPrefix(path, "_next/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// FileServer resuelve por r.URL.Path: lo reescribimos a la ruta que
		// hemos localizado (p. ej. /agents → /agents.html). Con el índice no
		// hace falta: FileServer resuelve / → index.html y evitamos el 301.
		if path == "index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = "/" + path
		fileServer.ServeHTTP(w, &r2)
	})
}
