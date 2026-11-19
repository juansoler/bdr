//go:build windows

// Probe G0.1 (comparativa): crea una shadow copy con mxk/go-vss (WMI),
// lee los primeros bytes del volumen y la elimina al terminar.
//
// Requiere Go >= 1.21 y una consola elevada (Administrador).
// No modifica datos: la instantánea es de solo lectura y se elimina.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	vss "github.com/mxk/go-vss"
)

type result struct {
	Volume       string `json:"volume"`
	ShadowID     string `json:"shadow_id"`
	DeviceObject string `json:"device_object"`
	Path         string `json:"path"`
	BytesRead    int64  `json:"bytes_read"`
	SHA256       string `json:"sha256"`
	DurationMS   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

func main() {
	volume := flag.String("volume", `C:\`, "volumen a instantanear")
	bytesFlag := flag.Int64("bytes", 1<<20, "bytes a leer")
	confirm := flag.Bool("confirm", false, "confirma la creación de la instantánea")
	list := flag.Bool("list", false, "lista instantáneas existentes (solo lectura)")
	flag.Parse()

	if *list {
		snaps, err := vss.List("")
		if err != nil {
			snaps, err = vss.List(*volume)
		}
		if err != nil {
			fatal("list", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"snapshots": snaps})
		return
	}

	if !*confirm {
		fmt.Fprintln(os.Stderr, "rechazado: este probe crea una instantánea de volumen.")
		fmt.Fprintln(os.Stderr, "revisa el README y repite con -confirm si es un entorno de laboratorio.")
		os.Exit(2)
	}

	start := time.Now()
	name, err := vss.Create(*volume)
	if err != nil {
		fatal("create", err)
	}
	defer func() {
		if rerr := vss.Remove(name); rerr != nil {
			fmt.Fprintf(os.Stderr, "aviso al eliminar la instantánea: %v\n", rerr)
		}
	}()

	sc, err := vss.Get(name)
	if err != nil {
		fatal("get", err)
	}
	path, err := sc.VolumePath()
	if err != nil {
		fatal("volume path", err)
	}

	res := result{
		Volume:       *volume,
		ShadowID:     sc.ID,
		DeviceObject: sc.DeviceObject,
		Path:         path,
	}
	f, err := os.Open(path)
	if err != nil {
		res.Error = err.Error()
		writeResult(res)
		os.Exit(1)
	}
	h := sha256.New()
	n, readErr := io.Copy(h, io.LimitReader(f, *bytesFlag))
	res.BytesRead = n
	res.SHA256 = hex.EncodeToString(h.Sum(nil))
	if readErr != nil && readErr != io.EOF {
		res.Error = readErr.Error()
	}
	_ = f.Close()
	res.DurationMS = time.Since(start).Milliseconds()
	writeResult(res)
}

func writeResult(r result) {
	b, _ := json.Marshal(r)
	fmt.Println(string(b))
}

func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "ERROR (%s): %v\n", what, err)
	os.Exit(1)
}
