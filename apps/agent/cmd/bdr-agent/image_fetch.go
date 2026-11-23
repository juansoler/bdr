// image-fetch descarga una imagen completa del bucket del destino a un
// directorio local, listo para `image-write`. Pensado para el entorno de
// recuperación del equipo destino (cualquier máquina con credenciales del
// bucket puede hacerlo, no hace falta estar enrolado).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	pkgimg "github.com/juansoler/bdr/packages/image"
	"github.com/juansoler/bdr/packages/logging"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func cmdImageFetch(args []string) {
	fs := flag.NewFlagSet("image-fetch", flag.ExitOnError)
	endpoint := fs.String("endpoint", "", "endpoint S3 (host:puerto)")
	bucket := fs.String("bucket", "", "bucket del destino")
	prefix := fs.String("prefix", "", "prefijo de objetos de imagen (opcional)")
	region := fs.String("region", "", "región S3 (fsn1, hel1… opcional; auto-detectada si vacía)")
	accessKey := fs.String("access-key", "", "access key del bucket")
	secretKey := fs.String("secret-key", "", "secret key del bucket")
	noTLS := fs.Bool("no-tls", false, "sin TLS (MinIO local)")
	manifestKey := fs.String("manifest", "", "clave del objeto manifest-*.json (ver resultado del job)")
	dir := fs.String("dir", "", "directorio destino de la descarga")
	_ = fs.Parse(args)

	if *prefix != "" {
		*prefix = strings.Trim(*prefix, "/") + "/"
	}
	if *endpoint == "" || *bucket == "" || *accessKey == "" || *manifestKey == "" || *dir == "" {
		fmt.Fprintln(os.Stderr, "faltan --endpoint, --bucket, --access-key, --manifest o --dir")
		fs.Usage()
		os.Exit(2)
	}
	reg := *region
	if reg == "" {
		reg = contracts.DetectS3Region(*endpoint)
	}
	log := logging.New("info")
	mc, err := minio.New(*endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(*accessKey, *secretKey, ""),
		Secure:       !*noTLS,
		Region:       reg,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		log.Error("endpoint inválido", "error", err.Error())
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Error("directorio", "error", err.Error())
		os.Exit(1)
	}
	// Manifiesto.
	obj, err := mc.GetObject(ctx, *bucket, *manifestKey, minio.GetObjectOptions{})
	if err != nil {
		log.Error("manifiesto", "error", err.Error())
		os.Exit(1)
	}
	var manifest pkgimg.Manifest
	if err := json.NewDecoder(obj).Decode(&manifest); err != nil {
		_ = obj.Close()
		log.Error("manifiesto inválido", "error", err.Error())
		os.Exit(1)
	}
	_ = obj.Close()
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(*dir, "manifest.json"), mb, 0o644); err != nil {
		log.Error("manifiesto", "error", err.Error())
		os.Exit(1)
	}

	total := 0
	for _, s := range manifest.Streams {
		if s.Blob == "" || s.Zero {
			continue
		}
		total++
	}
	log.Info("descargando imagen", "segmentos", total, "destino", *dir)
	done := 0
	for _, s := range manifest.Streams {
		if s.Blob == "" || s.Zero {
			continue
		}
		local := filepath.Join(*dir, filepath.Base(s.Blob))
		// Reanudación: si el fichero local ya está y su SHA-256 coincide
		// con el del manifiesto, no se descarga de nuevo.
		if data, err := os.ReadFile(local); err == nil && len(data) == int(s.Bytes) {
			sum := sha256.Sum256(data)
			if strings.EqualFold(hex.EncodeToString(sum[:]), s.SHA256) {
				done++
				continue
			}
		}
		if err := mc.FGetObject(ctx, *bucket, s.Blob, local, minio.GetObjectOptions{}); err != nil {
			log.Error("segmento", "blob", s.Blob, "error", err.Error())
			os.Exit(1)
		}
		done++
		if done%20 == 0 || done == total {
			log.Info("progreso", "segmento", fmt.Sprintf("%d/%d", done, total))
		}
	}
	fmt.Println("descarga completa; ahora: bdr-agent image-write --segments-dir " + *dir + " --target <disco> --yes")
}
