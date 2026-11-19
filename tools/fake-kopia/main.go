// fake-kopia imita los comandos de Kopia que usa el agente en pruebas.
// No realiza ninguna copia real: sólo genera salidas realistas.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	for _, a := range args {
		if a == "--version" || a == "-version" {
			fmt.Println("0.23.1")
			return
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fake-kopia: falta un comando")
		os.Exit(2)
	}
	if args[0] == "version" {
		fmt.Println("0.23.1")
		return
	}

	cmdIdx := -1
	for i, a := range args {
		switch a {
		case "repository", "policy", "snapshot":
			cmdIdx = i
		}
		if cmdIdx >= 0 {
			break
		}
	}
	if cmdIdx < 0 {
		fmt.Fprintf(os.Stderr, "fake-kopia: comando no soportado: %v\n", args)
		os.Exit(2)
	}

	cmd := args[cmdIdx]
	parsed := parseArgs(args[cmdIdx+1:])
	switch cmd {
	case "repository":
		// connect, status, disconnect...
		fmt.Println("Connected to repository.")
	case "policy":
		// set, show, get... sin salida.
	case "snapshot":
		sub := ""
		if len(parsed.positionals) > 0 {
			sub = parsed.positionals[0]
		}
		switch sub {
		case "create":
			if err := fakeSnapshotCreate(parsed); err != nil {
				fmt.Fprintln(os.Stderr, "fake-kopia:", err)
				os.Exit(1)
			}
		case "restore":
			if err := fakeSnapshotRestore(parsed); err != nil {
				fmt.Fprintln(os.Stderr, "fake-kopia:", err)
				os.Exit(1)
			}
		case "list":
			fmt.Println("[]")
		default:
			fmt.Fprintln(os.Stderr, "fake-kopia: subcomando snapshot no soportado:", sub)
			os.Exit(2)
		}
	}
}

type parsedArgs struct {
	positionals []string
	flags       map[string]string
	bools       map[string]bool
}

func parseArgs(args []string) parsedArgs {
	p := parsedArgs{flags: map[string]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--"):
			name := strings.TrimPrefix(a, "--")
			if k := strings.IndexByte(name, '='); k >= 0 {
				p.flags[name[:k]] = name[k+1:]
				continue
			}
			if isBoolFlag(name) {
				p.bools[name] = true
				continue
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				p.flags[name] = args[i+1]
				i++
			} else {
				p.bools[name] = true
			}
		case strings.HasPrefix(a, "-") && len(a) == 2:
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				p.flags[string(a[1])] = args[i+1]
				i++
			} else {
				p.bools[string(a[1])] = true
			}
		default:
			p.positionals = append(p.positionals, a)
		}
	}
	return p
}

func isBoolFlag(name string) bool {
	switch name {
	case "json", "progress", "clear-ignore", "clear-only-compress",
		"clear-never-compress", "persist-action-script":
		return true
	}
	return strings.HasPrefix(name, "no-") || strings.HasPrefix(name, "overwrite-") ||
		strings.HasPrefix(name, "skip-") || strings.HasPrefix(name, "write-")
}

func fakeSnapshotCreate(p parsedArgs) error {
	if len(p.positionals) < 2 {
		return fmt.Errorf("snapshot create requiere una ruta")
	}
	path := p.positionals[1]
	description := p.flags["description"]
	if description == "" {
		description = "fake"
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "fake-host"
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "fake-user"
	}

	start := time.Now().UTC()
	files, dirs, size := walkStats(path)
	end := time.Now().UTC()
	name := filepath.Base(path)
	entryType := "d"
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		entryType = "f"
		dirs = 0
	}

	payload := map[string]any{
		"id":          fakeID(),
		"source":      map[string]string{"host": host, "userName": user, "path": path},
		"description": description,
		"startTime":   start.Format(time.RFC3339Nano),
		"endTime":     end.Format(time.RFC3339Nano),
		"rootEntry": map[string]any{
			"name": name,
			"type": entryType,
			"obj":  "k" + fakeID(),
			"summ": map[string]any{
				"size": size, "files": files, "symlinks": 0, "dirs": dirs, "numFailed": 0,
			},
		},
	}

	if p.bools["json"] {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("Snapshotting %s@%s:%s ...\n", user, host, path)
	fmt.Printf("Created snapshot with id %s\n", payload["id"])
	return nil
}

func fakeSnapshotRestore(p parsedArgs) error {
	if len(p.positionals) < 3 {
		return fmt.Errorf("snapshot restore requiere origen y destino")
	}
	source, dest := p.positionals[1], p.positionals[2]
	if strings.Contains(source, "/") {
		// Restauración selectiva: el destino es un archivo.
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte("fake restore de "+source+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Printf("Restored 1 files, 0 directories and 0 symbolic links.\n")
		return nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	marker := filepath.Join(dest, ".bdr-fake-restore")
	if err := os.WriteFile(marker, []byte(source+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Restored snapshot %s en %s\n", source, dest)
	return nil
}

func walkStats(root string) (files, dirs, size int64) {
	info, err := os.Stat(root)
	if err != nil {
		return 0, 0, 0
	}
	if !info.IsDir() {
		return 1, 0, info.Size()
	}
	dirs = 1 // la raíz cuenta como directorio, igual que Kopia
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		if fi.IsDir() {
			if fi.Name() != filepath.Base(root) || fi.Mode() != info.Mode() {
				// contamos todos los directorios; la raíz ya está contada
			}
			return nil
		}
		files++
		if fi.Mode().IsRegular() {
			size += fi.Size()
		}
		return nil
	})
	// Contar subdirectorios (Walk ya visitó la raíz; descontamos uno).
	var subDirs int64
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && fi.IsDir() && p != root {
			subDirs++
		}
		return nil
	})
	dirs += subDirs
	return files, dirs, size
}

func fakeID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
