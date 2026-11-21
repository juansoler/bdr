// Package storage transfiere los ficheros de staging al repositorio de
// backups. El destino puede ser una ruta local (laboratorio), una compartida
// SMB (NAS) o un bucket S3 con Object Lock (inmutabilidad).
//
// La configuración del destino usa el mismo contrato que el control plane
// (contracts.RepositoryCredentials): un destino definido en la plataforma
// funciona tal cual para el motor SQL.
package storage

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/juansoler/bdr/packages/contracts"
)

// Transferer transfiere ficheros de backup al repositorio.
type Transferer interface {
	// Transfer copia el fichero local srcPath al repositorio y devuelve la
	// URI del objeto destino ("s3://bucket/clave", UNC de Windows
	// \\servidor\recurso\... o ruta local).
	Transfer(ctx context.Context, srcPath string) (string, error)
	// Fetch descarga al staging local destPath el objeto uri del
	// repositorio (operación inversa de Transfer; la usa el restore).
	Fetch(ctx context.Context, uri, destPath string) error
	// List lista las URIs de los objetos bajo el prefijo (diagnóstico).
	List(ctx context.Context, prefix string) ([]string, error)
	// Cleanup elimina un objeto del repositorio. Solo para emergencias: los
	// backups no se borran por diseño y en S3 con Object Lock el servidor
	// puede rehusarse (correcto).
	Cleanup(ctx context.Context, uri string) error
	// Close libera recursos de la sesión (SMB). No-op en el resto.
	Close() error
}

// NewTransferer construye un transferer LOCAL al directorio repo.Path
// (flujo de laboratorio legacy; para destinos de plataforma usa
// NewTransfererFor).
func NewTransferer(repo Repository) Transferer {
	return &localTransferer{repo: repo}
}

// NewTransfererFor construye el transferer según las credenciales del
// destino de almacenamiento del control plane. retainDays > 0 activa
// Object Lock en modo GOVERNANCE para los objetos subidos a S3.
func NewTransfererFor(ctx context.Context, creds *contracts.RepositoryCredentials, retainDays int) (Transferer, error) {
	if creds == nil {
		return nil, fmt.Errorf("se requiere un destino de almacenamiento (credentials nil)")
	}
	switch creds.Kind {
	case contracts.RepoKindS3:
		return newS3Transferer(creds, retainDays)
	case contracts.RepoKindFilesystem:
		if contracts.IsSMBPath(creds.Path) {
			return newSMBTransferer(ctx, creds)
		}
		return NewTransferer(Repository{Type: "nas", Path: creds.Path}), nil
	case "":
		return nil, fmt.Errorf("el destino no declara tipo (kind vacío)")
	default:
		return nil, fmt.Errorf("el destino %q no soporta transferencia directa del motor SQL", creds.Kind)
	}
}

// ParseTargetURI interpreta la URI de destino del CLI de laboratorio y
// completa las credenciales desde el entorno:
//
//	s3://endpoint/bucket[/prefijo]   + BDR_S3_ACCESS_KEY / BDR_S3_SECRET_KEY
//	                                   (BDR_S3_REGION, BDR_S3_NO_TLS opcionales)
//	//host/recurso[/ruta] o smb://…  + BDR_SMB_USER / BDR_SMB_PASSWORD
//	                                   (BDR_SMB_DOMAIN opcional)
//	ruta/local                        (sin credenciales)
//
// getenv se inyecta para poder testear sin tocar el entorno del proceso.
// retainDays (Object Lock GOVERNANCE en S3) solo aplica al destino S3.
func ParseTargetURI(uri string, getenv func(string) string, retainDays int) (Transferer, error) {
	uri = strings.TrimSpace(uri)
	if getenv == nil {
		getenv = os.Getenv
	}

	switch {
	case strings.HasPrefix(uri, "s3://"):
		rest := strings.TrimPrefix(uri, "s3://")
		parts := strings.SplitN(rest, "/", 2)
		if parts[0] == "" || len(parts) < 2 || parts[1] == "" {
			return nil, fmt.Errorf("URI S3 inválida %q (s3://endpoint/bucket[/prefijo])", uri)
		}
		bucket, prefix := parts[1], ""
		if i := strings.Index(bucket, "/"); i >= 0 {
			bucket, prefix = bucket[:i], bucket[i+1:]
		}
		if bucket == "" {
			return nil, fmt.Errorf("URI S3 inválida %q: bucket vacío", uri)
		}
		creds := &contracts.RepositoryCredentials{
			Kind:      contracts.RepoKindS3,
			Endpoint:  contracts.S3EndpointHost(parts[0]),
			Bucket:    bucket,
			Prefix:    prefix,
			AccessKey: getenv("BDR_S3_ACCESS_KEY"),
			SecretKey: getenv("BDR_S3_SECRET_KEY"),
			Region:    getenv("BDR_S3_REGION"),
			NoTLS:     getenv("BDR_S3_NO_TLS") == "1",
		}
		if creds.AccessKey == "" || creds.SecretKey == "" {
			return nil, fmt.Errorf("destino S3 sin credenciales: define BDR_S3_ACCESS_KEY y BDR_S3_SECRET_KEY")
		}
		return NewTransfererFor(context.Background(), creds, retainDays)

	case strings.HasPrefix(uri, "smb://") || strings.HasPrefix(uri, "//"):
		if !strings.HasPrefix(uri, "smb://") {
			uri = "smb://" + uri // UNC //host/... → smb://host/...
		}
		creds := &contracts.RepositoryCredentials{
			Kind:        contracts.RepoKindFilesystem,
			Path:        uri,
			SMBUser:     getenv("BDR_SMB_USER"),
			SMBPassword: getenv("BDR_SMB_PASSWORD"),
			SMBDomain:   getenv("BDR_SMB_DOMAIN"),
		}
		if creds.SMBUser == "" {
			return nil, fmt.Errorf("destino SMB sin usuario: define BDR_SMB_USER y BDR_SMB_PASSWORD")
		}
		return NewTransfererFor(context.Background(), creds, 0)

	default:
		if uri == "" {
			return nil, fmt.Errorf("destino vacío")
		}
		return NewTransferer(Repository{Type: "nas", Path: uri}), nil
	}
}
