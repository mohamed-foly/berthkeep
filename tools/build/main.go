// Command build makes release archives for one OS/arch: go run ./tools/build.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type binary struct {
	name, pkg, tags, ldExtra string
	cgo                      bool
}

func main() {
	goos := flag.String("os", runtime.GOOS, "target OS")
	arch := flag.String("arch", runtime.GOARCH, "target arch")
	version := flag.String("version", "dev", "version string")
	out := flag.String("out", "dist", "output folder")
	variant := flag.String("variant", "gui", "gui or server (server: no GUI libraries)")
	budgetMB := flag.Float64("budget", 20, "max binary size in MB")
	flag.Parse()
	if err := run(*goos, *arch, *version, *out, *variant, *budgetMB); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
}

func plan(goos, variant string) []binary {
	switch {
	case goos == "windows":
		return []binary{
			{name: "berthkeep.exe", pkg: "./cmd/berthkeep"},
			{name: "berthkeepw.exe", pkg: "./cmd/berthkeepw", ldExtra: " -H=windowsgui"},
		}
	case variant == "server":
		return []binary{{name: "berthkeep", pkg: "./cmd/berthkeep", tags: "server"}}
	default: // macOS / Linux GUI need cgo on a native runner
		return []binary{{name: "berthkeep", pkg: "./cmd/berthkeep", cgo: true}}
	}
}

func run(goos, arch, version, out, variant string, budgetMB float64) error {
	stage := filepath.Join(out, "stage-"+goos+"-"+arch+"-"+variant)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	var files []string
	for _, b := range plan(goos, variant) {
		dst := filepath.Join(stage, b.name)
		args := []string{"build", "-trimpath", "-ldflags", "-s -w -X main.version=" + version + b.ldExtra, "-o", dst}
		if b.tags != "" {
			args = append(args, "-tags", b.tags)
		}
		cmd := exec.Command("go", append(args, b.pkg)...)
		cgo := "0"
		if b.cgo {
			cgo = "1"
		}
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED="+cgo)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", b.name, err)
		}
		st, err := os.Stat(dst)
		if err != nil {
			return err
		}
		mb := float64(st.Size()) / (1 << 20)
		fmt.Printf("%-16s %6.2f MB\n", b.name, mb)
		if mb > budgetMB {
			return fmt.Errorf("%s is %.2f MB, over the %.0f MB budget", b.name, mb, budgetMB)
		}
		files = append(files, dst)
	}
	if _, err := os.Stat("LICENSE"); err == nil {
		files = append(files, "LICENSE")
	}
	name := fmt.Sprintf("berthkeep_%s_%s_%s", strings.TrimPrefix(version, "v"), goos, arch)
	if variant == "server" {
		name += "_server"
	}
	var archive string
	var err error
	if goos == "windows" {
		archive, err = zipFiles(filepath.Join(out, name+".zip"), files)
	} else {
		archive, err = tarFiles(filepath.Join(out, name+".tar.gz"), files)
	}
	if err != nil {
		return err
	}
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	return appendChecksum(filepath.Join(out, "checksums.txt"), archive)
}

func zipFiles(dst string, files []string) (string, error) {
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: filepath.Base(p), Method: zip.Deflate, Modified: st.ModTime()})
		if err != nil {
			return "", err
		}
		if err := copyFile(w, p); err != nil {
			return "", err
		}
	}
	return dst, zw.Close()
}

func tarFiles(dst string, files []string) (string, error) {
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		mode := int64(0o644)
		if st.Mode()&0o111 != 0 || !strings.Contains(filepath.Base(p), ".") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.Base(p), Mode: mode, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
			return "", err
		}
		if err := copyFile(tw, p); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	return dst, gz.Close()
}

func copyFile(w io.Writer, p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func appendChecksum(sums, archive string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return err
	}
	f.Close()
	out, err := os.OpenFile(sums, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = fmt.Fprintf(out, "%s  %s\n", hex.EncodeToString(h.Sum(nil)), filepath.Base(archive))
	fmt.Println("archive", archive)
	return err
}
