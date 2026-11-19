// Rutas compartidas SMB para destinos de almacenamiento de tipo filesystem:
// "//servidor/recurso[/subdir]" o "smb://servidor[:puerto]/recurso[/subdir]".
// El acceso usa un cliente SMB nativo (NTLM con dominio AD/workgroup), sin
// montaje ni privilegios de root.
package contracts

import (
	"net"
	"strings"
)

// SMBPathInfo descompone una ruta SMB.
type SMBPathInfo struct {
	Host   string // host, host:puerto o [IPv6]:puerto
	Share  string // recurso compartido (obligatorio)
	Subdir string // subdirectorio dentro del recurso (opcional, con "/")
}

// ParseSMBPath reconoce rutas SMB: smb://host[:port]/share[/subdir] o
// //host[:port]/share[/subdir]. Devuelve ok=false para rutas locales.
// El recurso compartido es obligatorio: una ruta solo con host no es válida.
func ParseSMBPath(path string) (SMBPathInfo, bool) {
	p := strings.TrimSpace(path)
	var rest string
	switch {
	case strings.HasPrefix(p, "smb://"):
		rest = strings.TrimPrefix(p, "smb://")
	case strings.HasPrefix(p, "//"):
		rest = strings.TrimPrefix(p, "//")
	default:
		return SMBPathInfo{}, false
	}
	rest = strings.Trim(rest, "/")
	host, sub, _ := strings.Cut(rest, "/")
	host = strings.TrimSpace(host)
	if host == "" {
		return SMBPathInfo{}, false
	}
	// El host debe ser un host/IP, con o sin puerto (admite [IPv6]:puerto).
	if h, _, err := net.SplitHostPort(host); err == nil {
		if strings.TrimSpace(h) == "" {
			return SMBPathInfo{}, false
		}
	}
	sub = strings.Trim(sub, "/")
	if sub == "" {
		return SMBPathInfo{}, false // sin recurso compartido
	}
	share, subdir := sub, ""
	if i := strings.Index(sub, "/"); i >= 0 {
		share = sub[:i]
		subdir = strings.Trim(sub[i+1:], "/")
	}
	if share == "" || strings.EqualFold(share, "ipc$") {
		return SMBPathInfo{}, false
	}
	return SMBPathInfo{Host: host, Share: share, Subdir: subdir}, true
}

// IsSMBPath indica si una ruta es SMB (compartida) en lugar de local.
func IsSMBPath(path string) bool {
	_, ok := ParseSMBPath(path)
	return ok
}

// NormalizeSMBPath devuelve la ruta SMB canónica
// "//host[:puerto]/recurso[/subdir]" (sin smb:// ni barras redundantes).
// Las rutas no SMB se devuelven recortadas sin cambios.
func NormalizeSMBPath(path string) string {
	info, ok := ParseSMBPath(path)
	if !ok {
		return strings.TrimSpace(path)
	}
	p := "//" + info.Host + "/" + info.Share
	if info.Subdir != "" {
		p += "/" + strings.Trim(info.Subdir, "/")
	}
	return p
}
