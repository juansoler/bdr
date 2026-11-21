package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Repository struct {
	Type string
	Path string
}

type Transferer struct {
	repo Repository
}

func NewTransferer(repo Repository) *Transferer {
	return &Transferer{repo: repo}
}

func (t *Transferer) Transfer(ctx context.Context, stagingPath string) (string, error) {
	if err := os.MkdirAll(t.repo.Path, 0750); err != nil {
		return "", fmt.Errorf("crear destino %s: %w", t.repo.Path, err)
	}
	base := filepath.Base(stagingPath)
	dest := filepath.Join(t.repo.Path, base)
	if err := copyFile(stagingPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}