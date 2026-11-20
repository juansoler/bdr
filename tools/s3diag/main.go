// s3diag verifica destinos S3-compatible con el flujo exacto que usa BDR
// (el mismo cliente MinIO que el verify y la réplica del control).
//
// Diagnostica el InvalidRequest de la escritura de prueba: prueba el bucket,
// el PUT sin lock y el PUT con lock GOVERNANCE, y opcionalmente crea el
// bucket con Object Lock habilitado si el proveedor lo exige para el lock.
//
// Uso:
//
//	go run ./tools/s3diag -endpoint hel1.your-objectstorage.com \
//	                     -bucket bdr-vergel-imagen \
//	                     -access-key AKIA... -secret-key ... \
//	                     -region hel1 [-create-with-lock]
package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	endpoint := flag.String("endpoint", "", "endpoint S3 (hel1.your-objectstorage.com)")
	bucket := flag.String("bucket", "", "bucket a verificar")
	ak := flag.String("access-key", "", "access key")
	sk := flag.String("secret-key", "", "secret key")
	region := flag.String("region", "", "región del scope SigV4 (hel1, fsn1…)")
	createLock := flag.Bool("create-with-lock", false, "crea el bucket con Object Lock si no existe")
	flag.Parse()

	if *endpoint == "" || *bucket == "" || *ak == "" || *sk == "" {
		fmt.Println("faltan -endpoint, -bucket, -access-key y -secret-key")
		return
	}

	mc, err := minio.New(*endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(*ak, *sk, ""),
		Secure:       true,
		Region:       *region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		fmt.Println("✗ cliente:", err)
		return
	}
	fmt.Printf("✓ cliente MinIO en %s (path-style, región=%q)\n", *endpoint, *region)

	ctx := context.Background()
	ret := time.Now().UTC().AddDate(0, 0, 7)

	fmt.Printf("BucketExists(%q): ", *bucket)
	exists, err := mc.BucketExists(ctx, *bucket)
	if err != nil {
		fmt.Println("✗", err)
	} else {
		fmt.Println("✓ existe =", exists)
	}

	if !exists && *createLock {
		fmt.Printf("MakeBucket(%q, Object Lock): ", *bucket)
		if err := mc.MakeBucket(ctx, *bucket, minio.MakeBucketOptions{ObjectLocking: true}); err != nil {
			fmt.Println("✗", err)
		} else {
			fmt.Println("✓ creado con Object Lock")
		}
		time.Sleep(16 * time.Second) // Hetzner: propagación de la config del bucket
	}

	if !strings.Contains(*bucket, "/") {
		fmt.Printf("PUT sin lock: ")
		if _, err := mc.PutObject(ctx, *bucket, "bdr-diag/sin-lock.txt", strings.NewReader("prueba"), 7, minio.PutObjectOptions{}); err != nil {
			fmt.Println("✗", err)
		} else {
			fmt.Println("✓")
		}
	}

	fmt.Printf("PUT con Object Lock GOVERNANCE 7d: ")
	if _, err := mc.PutObject(ctx, *bucket, "bdr-diag/lock-7d.txt", strings.NewReader("prueba"), 7, minio.PutObjectOptions{
		Mode:             minio.Governance,
		RetainUntilDate:  ret,
	}); err != nil {
		fmt.Println("✗", err)
	} else {
		fmt.Println("✓ (el bucket tiene lock; se puede usar con Object Lock activo en BDR)")
	}

	buckets, err := mc.ListBuckets(ctx)
	if err != nil {
		fmt.Println("ListBuckets: ✗", err)
		return
	}
	fmt.Printf("Buckets accesibles con estas credenciales (%d):\n", len(buckets))
	for _, b := range buckets {
		fmt.Println("  •", b.Name, "creado:", b.CreationDate.Format("2006-01-02"))
	}
}
