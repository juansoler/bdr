package jobs

import (
	"path"
	"runtime"
	"strings"
)

// IgnorePattern convierte una exclusión del plan en un patrón de ignore de
// Kopia (estilo gitignore) relativo a la raíz de la política.
//
// Las rutas absolutas que caen dentro de la ruta respaldada se convierten en
// patrones anclados ("/sub/dir"). Las exclusiones no absolutas ("*.tmp") se
// pasan tal cual. Devuelve false si la exclusión absoluta queda fuera de root.
func IgnorePattern(exclude, root string) (string, bool) {
	return ignorePattern(exclude, root, runtime.GOOS == "windows")
}

// ignorePattern permite probar la semántica de Windows desde cualquier SO.
func ignorePattern(exclude, root string, windows bool) (string, bool) {
	ex := strings.TrimSpace(exclude)
	if ex == "" {
		return "", false
	}
	if windows {
		ex = strings.ReplaceAll(ex, `\`, "/")
	}
	if !isAbsPattern(ex, windows) {
		// Patrón relativo: Kopia lo interpreta respecto a la raíz.
		return ex, true
	}

	rootN := strings.TrimSpace(root)
	if windows {
		rootN = strings.ReplaceAll(rootN, `\`, "/")
	}
	rootN = strings.TrimRight(rootN, "/")
	if rootN == "" {
		// La raíz es "/": el patrón absoluto ya está anclado.
		return path.Clean(ex), true
	}

	exN := path.Clean(ex)
	rootN = path.Clean(rootN)

	cmpEx, cmpRoot := exN, rootN
	if windows {
		cmpEx, cmpRoot = strings.ToLower(cmpEx), strings.ToLower(cmpRoot)
	}
	if cmpEx == cmpRoot {
		// Excluir la propia raíz deja todo fuera.
		return "/", true
	}
	prefix := cmpRoot + "/"
	if strings.HasPrefix(cmpEx, prefix) {
		return "/" + exN[len(prefix):], true
	}
	return "", false
}

func isAbsPattern(p string, windows bool) bool {
	if windows {
		if strings.HasPrefix(p, "//") {
			return true // UNC
		}
		return len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0])
	}
	return strings.HasPrefix(p, "/")
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
