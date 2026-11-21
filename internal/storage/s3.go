package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/juansoler/bdr/packages/contracts"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Transferer transfiere a un bucket S3 compatible (MinIO, Hetzner, AWS…)
// con Object Lock GOVERNANCE opcional (inmutabilidad RF-41).
type s3Transferer struct {
	mc         *minio.Client
	bucket     string
	prefix     string // con barra final ("" si no hay)
	retainDays int
}

func newS3Transferer(creds *contracts.RepositoryCredentials, retainDays int) (*s3Transferer, error) {
	if creds.Endpoint == "" || creds.Bucket == "" {
		return nil, fmt.Errorf("las credenciales del destino S3 no incluyen endpoint o bucket")
	}
	region := creds.Region
	if region == "" {
		region = contracts.DetectS3Region(creds.Endpoint)
	}
	mc, err := minio.New(contracts.S3EndpointHost(creds.Endpoint), &minio.Options{
		Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure:       !creds.NoTLS,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint S3 inválido: %w", err)
	}
	return &s3Transferer{
		mc:         mc,
		bucket:     creds.Bucket,
		prefix:     s3Prefix(creds.Prefix),
		retainDays: retainDays,
	}, nil
}

// s3Prefix normaliza el prefijo del destino con barra final.
func s3Prefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// s3Key compone la clave destino: prefijo del destino + nombre del fichero.
func (s *s3Transferer) s3Key(name string) (string, error) {
	name = path.Base(name)
	if name == "" || name == "." || name == "/" || strings.Contains(name, "..") {
		return "", fmt.Errorf("nombre de objeto inválido: %q", name)
	}
	return s.prefix + name, nil
}

// putOpts aplica Object Lock GOVERNANCE si el destino lo pide.
func (s *s3Transferer) putOpts(now time.Time) minio.PutObjectOptions {
	opts := minio.PutObjectOptions{}
	if s.retainDays > 0 {
		opts.Mode = minio.Governance
		opts.RetainUntilDate = now.UTC().AddDate(0, 0, s.retainDays)
	}
	return opts
}

// Transfer sube el fichero local al bucket con streaming (los backups SQL
// pueden pesar gigas: nunca se cargan en memoria).
func (s *s3Transferer) Transfer(ctx context.Context, srcPath string) (string, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("abrir %s: %w", srcPath, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", srcPath, err)
	}

	key, err := s.s3Key(fi.Name())
	if err != nil {
		return "", err
	}
	if _, err := s.mc.PutObject(ctx, s.bucket, key, f, fi.Size(), s.putOpts(time.Now())); err != nil {
		return "", fmt.Errorf("subir %s a s3://%s/%s: %w", fi.Name(), s.bucket, key, err)
	}
	return "s3://" + s.bucket + "/" + key, nil
}

// Fetch descarga un objeto del bucket (por URI s3://bucket/clave) al
// staging local con streaming.
func (s *s3Transferer) Fetch(ctx context.Context, uri, destPath string) error {
	key, err := s.uriToKey(uri)
	if err != nil {
		return err
	}
	obj, err := s.mc.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("descargar s3://%s/%s: %w", s.bucket, key, err)
	}
	defer obj.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return err
	}
	tmp := destPath + ".bdr-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, obj); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("descargar %s: %w", key, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}

// List lista las URIs s3:// bajo el prefijo dado (o el del destino).
func (s *s3Transferer) List(ctx context.Context, prefix string) ([]string, error) {
	if prefix == "" {
		prefix = s.prefix
	} else {
		prefix = s3Prefix(prefix)
	}
	objects := s.mc.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix: prefix, Recursive: true,
	})
	var out []string
	for obj := range objects {
		if obj.Err != nil {
			return nil, obj.Err
		}
		out = append(out, "s3://"+s.bucket+"/"+obj.Key)
	}
	return out, nil
}

// Cleanup elimina un objeto con bypass de GOVERNANCE (solo emergencia:
// el bypass exige credenciales privilegiadas y queda en el audit del servidor).
func (s *s3Transferer) Cleanup(ctx context.Context, uri string) error {
	key, err := s.uriToKey(uri)
	if err != nil {
		return err
	}
	return s.mc.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{
		GovernanceBypass: true,
	})
}

// uriToKey valida que la URI pertenezca a este bucket y prefijo.
func (s *s3Transferer) uriToKey(uri string) (string, error) {
	p := "s3://" + s.bucket + "/"
	if !strings.HasPrefix(uri, p) {
		return "", fmt.Errorf("URI %s no pertenece al bucket %s", uri, s.bucket)
	}
	key := strings.TrimPrefix(uri, p)
	if s.prefix != "" && !strings.HasPrefix(key, s.prefix) {
		return "", fmt.Errorf("clave %s fuera del prefijo %q del destino", key, s.prefix)
	}
	return key, nil
}

func (s *s3Transferer) Close() error { return nil }

var _ Transferer = (*s3Transferer)(nil)
