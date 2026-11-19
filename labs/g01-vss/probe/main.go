//go:build windows

// Probe G0.1: crea un shadow set con SmithOperatingSolutions/go-vss (COM),
// verifica el conjunto y lee los primeros bytes de cada volumen del snapshot.
//
// Requiere Go >= 1.25 y una consola elevada (Administrador).
// No modifica datos: crea instantáneas y las elimina al cerrar el conjunto.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	vss "github.com/SmithOperatingSolutions/go-vss"
)

type snapshotInfo struct {
	ID           string `json:"id"`
	SetID        string `json:"set_id"`
	Volume       string `json:"volume"`
	VolumePath   string `json:"volume_path"`
	DeviceObject string `json:"device_object"`
	CreatedAt    string `json:"created_at"`
	State        string `json:"state"`
}

type readResult struct {
	Volume         string `json:"volume"`
	SnapshotID     string `json:"snapshot_id"`
	DeviceObject   string `json:"device_object"`
	BytesRequested int64  `json:"bytes_requested"`
	BytesRead      int64  `json:"bytes_read"`
	SHA256         string `json:"sha256"`
	DurationMS     int64  `json:"duration_ms"`
	Error          string `json:"error,omitempty"`
}

func normalizeVolume(v string) string {
	v = strings.TrimSpace(v)
	if len(v) == 2 && v[1] == ':' {
		return v + `\`
	}
	return v
}

func main() {
	volumesFlag := flag.String("volumes", `C:\`, "rutas de volumen separadas por comas (C:\\, \\\\?\\Volume{GUID}\\)")
	bytesFlag := flag.Int64("bytes", 1<<20, "bytes a leer por volumen")
	confirm := flag.Bool("confirm", false, "confirma la creación del shadow set")
	list := flag.Bool("list", false, "lista snapshots existentes (solo lectura)")
	timeout := flag.Duration("timeout", 10*time.Minute, "timeout total")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if *list {
		snaps, err := vss.List(ctx)
		if err != nil {
			fatal("list", err)
		}
		out := make([]snapshotInfo, 0, len(snaps))
		for _, s := range snaps {
			out = append(out, snapshotInfo{
				ID:           s.ID,
				SetID:        s.SetID,
				Volume:       s.Volume,
				VolumePath:   s.VolumePath,
				DeviceObject: s.DeviceObject,
				CreatedAt:    s.CreatedAt.UTC().Format(time.RFC3339),
				State:        fmt.Sprint(s.State),
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"snapshots": out})
		return
	}

	if !*confirm {
		fmt.Fprintln(os.Stderr, "rechazado: este probe crea una instantánea de volumen.")
		fmt.Fprintln(os.Stderr, "revisa el README y repite con -confirm si es un entorno de laboratorio.")
		os.Exit(2)
	}

	var vols []string
	for _, v := range strings.Split(*volumesFlag, ",") {
		v = normalizeVolume(v)
		if v != "" {
			vols = append(vols, v)
		}
	}
	if len(vols) == 0 {
		fatal("volúmenes", fmt.Errorf("no se indicó ningún volumen"))
	}

	start := time.Now()
	var (
		set *vss.SnapshotSet
		err error
	)
	if len(vols) == 1 {
		set, err = vss.Create(ctx, vols[0])
	} else {
		set, err = vss.CreateSet(ctx, vols)
	}
	if err != nil {
		fatal("crear shadow set", err)
	}
	defer func() {
		if cerr := set.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "aviso al cerrar el conjunto: %v\n", cerr)
		}
	}()

	verifyErr := set.Verify(ctx)
	writerCount := len(set.WriterStatuses())
	degraded := len(set.Degraded())

	meta := map[string]any{
		"event":           "shadow_set_created",
		"requested":       vols,
		"snapshots":       len(set.Snapshots()),
		"verify_ok":       verifyErr == nil,
		"verify_error":    errString(verifyErr),
		"writer_statuses": writerCount,
		"degraded":        degraded,
		"duration_ms":     time.Since(start).Milliseconds(),
	}
	writeJSON(meta)

	if verifyErr != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", verifyErr)
	}

	enc := json.NewEncoder(os.Stdout)
	for _, s := range set.Snapshots() {
		res := readResult{
			Volume:         s.Volume,
			SnapshotID:     s.ID,
			DeviceObject:   s.DeviceObject,
			BytesRequested: *bytesFlag,
		}
		readStart := time.Now()
		f, openErr := s.OpenRaw()
		if openErr != nil {
			res.Error = openErr.Error()
		} else {
			h := sha256.New()
			n, readErr := io.Copy(h, io.LimitReader(f, *bytesFlag))
			res.BytesRead = n
			res.SHA256 = hex.EncodeToString(h.Sum(nil))
			if readErr != nil && readErr != io.EOF {
				res.Error = readErr.Error()
			}
			if closeErr := f.Close(); closeErr != nil && res.Error == "" {
				res.Error = closeErr.Error()
			}
		}
		res.DurationMS = time.Since(readStart).Milliseconds()
		_ = enc.Encode(res)
	}

	writeJSON(map[string]any{
		"event":       "shadow_set_closed",
		"duration_ms": time.Since(start).Milliseconds(),
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "json: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "ERROR (%s): %v\n", what, err)
	os.Exit(1)
}
