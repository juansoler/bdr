package contracts

import (
	"net"
	"net/url"
	"strings"
)

// S3EndpointHost normaliza un endpoint S3 a "host[:puerto]" sin esquema ni
// ruta: kopia (minio-go v7.2) rechaza endpoints con esquema ("Endpoint url
// cannot have fully qualified paths") y el puerto indicado debe respetarse
// tal cual (MinIO/SILO separan API y consola: 9000 vs 9001).
func S3EndpointHost(endpoint string) string {
	e := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if i := strings.Index(e, "://"); i > 0 && !strings.ContainsAny(e[:i], "/?#") {
		e = e[i+3:]
	}
	if i := strings.IndexAny(e, "/?#"); i >= 0 {
		e = e[:i]
	}
	return e
}

// DetectS3Region deduce la región a partir del endpoint de proveedores S3
// conocidos (Hetzner Object Storage, DigitalOcean Spaces, AWS S3, Wasabi,
// Linode/Akamai, Scaleway, Backblaze B2). Devuelve "" si no se puede deducir.
func DetectS3Region(endpoint string) string {
	e := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if e == "" {
		return ""
	}
	if strings.Contains(e, "://") {
		if u, err := url.Parse(e); err == nil {
			e = u.Host
		} else {
			e = strings.TrimPrefix(e, "https://")
			e = strings.TrimPrefix(e, "http://")
		}
	}
	if host, _, err := net.SplitHostPort(e); err == nil {
		e = host
	}
	e = strings.ToLower(strings.Trim(e, "/"))
	parts := strings.Split(e, ".")
	if len(parts) < 3 {
		return ""
	}

	// Hetzner Object Storage: <region>.your-objectstorage.com
	// o <bucket>.<region>.your-objectstorage.com
	if strings.HasSuffix(e, ".your-objectstorage.com") {
		idx := len(parts) - 3
		if idx >= 0 && parts[idx] != "" {
			return parts[idx]
		}
	}

	// DigitalOcean Spaces: <region>.digitaloceanspaces.com
	if strings.HasSuffix(e, ".digitaloceanspaces.com") {
		idx := len(parts) - 3
		if idx >= 0 && parts[idx] != "" {
			return parts[idx]
		}
	}

	// Linode Objects: <region>.linodeobjects.com
	if strings.HasSuffix(e, ".linodeobjects.com") {
		idx := len(parts) - 3
		if idx >= 0 && parts[idx] != "" {
			return parts[idx]
		}
	}

	// Wasabi: s3.<region>.wasabisys.com
	if strings.HasSuffix(e, ".wasabisys.com") {
		idx := len(parts) - 3
		if idx >= 0 && parts[idx] != "" && parts[idx] != "s3" {
			return parts[idx]
		}
	}

	// Scaleway: s3.<region>.scw.cloud
	if strings.HasSuffix(e, ".scw.cloud") {
		idx := len(parts) - 3
		if idx >= 0 && parts[idx] != "" && parts[idx] != "s3" {
			return parts[idx]
		}
	}

	// Backblaze B2: s3.<region>.backblazeb2.com
	if strings.HasSuffix(e, ".backblazeb2.com") {
		idx := len(parts) - 3
		if idx >= 0 && parts[idx] != "" && parts[idx] != "s3" {
			return parts[idx]
		}
	}

	// AWS S3: s3.<region>.amazonaws.com o s3-<region>.amazonaws.com
	if strings.HasSuffix(e, ".amazonaws.com") {
		for i, p := range parts {
			if p == "s3" && i+1 < len(parts)-2 {
				return parts[i+1]
			}
			if strings.HasPrefix(p, "s3-") {
				return strings.TrimPrefix(p, "s3-")
			}
		}
	}

	return ""
}
