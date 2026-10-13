package epson

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrDownloadCleanup = errors.New("download cleanup failed")

func validFilename(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `\/:*?"<>|`) || strings.TrimRight(name, ". ") != name {
		return false
	}
	for _, r := range name {
		if r < 32 {
			return false
		}
	}
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

type progressWriter struct {
	ctx      context.Context
	out      io.Writer
	progress Progress
	notify   func(Progress)
	last     time.Time
}

func (w *progressWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.out.Write(data)
	w.progress.Bytes += int64(n)
	if w.notify != nil && time.Since(w.last) >= 100*time.Millisecond {
		w.notify(w.progress)
		w.last = time.Now()
	}
	return n, err
}

func (c *Client) Download(ctx context.Context, pkg Package, folder string, notify func(Progress)) (savedPath string, resultErr error) {
	if pkg.OpenOnly {
		return "", fmt.Errorf("%s is a browser-only resource; open its link in a browser", pkg.Title)
	}
	if !validFilename(pkg.Name) {
		return "", fmt.Errorf("unsafe download filename: %q", pkg.Name)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", fmt.Errorf("create destination: %w", err)
	}
	target := filepath.Join(folder, pkg.Name)
	resp, err := c.request(ctx, http.MethodGet, pkg.URL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", pkg.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", pkg.Name, resp.StatusCode)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return "", fmt.Errorf("download %s returned a web page, not the requested file", pkg.Name)
	}
	info, err := os.Lstat(target)
	if err == nil {
		if info.Mode().IsRegular() && info.Size() > 0 && resp.ContentLength > 0 && info.Size() == resp.ContentLength {
			if notify != nil {
				notify(Progress{Name: pkg.Name, Bytes: info.Size(), Total: info.Size()})
			}
			return target, nil
		}
		return "", fmt.Errorf("%s already exists but its size cannot be verified; move or remove it before retrying", target)
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect destination file: %w", err)
	}
	body := bufio.NewReader(resp.Body)
	signature := ""
	switch strings.ToLower(filepath.Ext(pkg.Name)) {
	case ".exe":
		signature = "MZ"
	case ".zip":
		signature = "PK"
	case ".pdf":
		signature = "%PDF-"
	case ".rpm":
		signature = "\xed\xab\xee\xdb"
	case ".deb":
		signature = "!<arch>\n"
	}
	header, err := body.Peek(len(signature))
	if err != nil {
		return "", fmt.Errorf("read file header for %s: %w", pkg.Name, err)
	}
	if string(header) != signature {
		return "", fmt.Errorf("%s does not have the expected %s file header", pkg.Name, filepath.Ext(pkg.Name))
	}
	temp, err := os.CreateTemp(folder, "."+pkg.Name+"-*.part")
	if err != nil {
		return "", fmt.Errorf("create temporary download: %w", err)
	}
	defer func() {
		if err := temp.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: close temporary file: %w", ErrDownloadCleanup, err))
		}
		if err := os.Remove(temp.Name()); err != nil && !os.IsNotExist(err) {
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: remove %s: %w", ErrDownloadCleanup, temp.Name(), err))
		}
	}()
	writer := &progressWriter{
		ctx: ctx, out: temp, notify: notify,
		progress: Progress{Name: pkg.Name, Total: resp.ContentLength},
	}
	if notify != nil {
		notify(writer.progress)
	}
	if _, err := io.Copy(writer, body); err != nil {
		return "", fmt.Errorf("save %s: %w", pkg.Name, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if writer.progress.Bytes == 0 || (resp.ContentLength >= 0 && writer.progress.Bytes != resp.ContentLength) {
		return "", fmt.Errorf("incomplete download of %s: received %d of %d bytes", pkg.Name, writer.progress.Bytes, resp.ContentLength)
	}
	if err := temp.Sync(); err != nil {
		return "", fmt.Errorf("flush download: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close download: %w", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		if err != nil {
			return "", fmt.Errorf("inspect destination file: %w", err)
		}
		return "", fmt.Errorf("destination appeared during download: %s", target)
	}
	if err := os.Rename(temp.Name(), target); err != nil {
		return "", fmt.Errorf("finish download: %w", err)
	}
	if notify != nil {
		notify(writer.progress)
	}
	return target, nil
}
