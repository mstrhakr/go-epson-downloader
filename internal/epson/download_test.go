package epson

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertFolderEmpty(t *testing.T, folder string) {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected leftover files: %v", entries)
	}
}

func TestDownloadAndReuse(t *testing.T) {
	payload := "MZ" + strings.Repeat("installer", 100)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, payload)
	})
	folder := t.TempDir()
	pkg := Package{Name: "driver.exe", URL: c.downloadURL + "driver.exe"}
	var progress []Progress
	path, err := c.Download(context.Background(), pkg, folder, func(p Progress) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != payload {
		t.Fatalf("saved payload mismatch: %v", err)
	}
	last := progress[len(progress)-1]
	if last.Bytes != int64(len(payload)) || last.Total != int64(len(payload)) {
		t.Fatalf("incorrect completion progress: %+v", last)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if reused, err := c.Download(context.Background(), pkg, folder, nil); err != nil || reused != path {
		t.Fatalf("reuse: %q, %v", reused, err)
	}
	after, err := os.Stat(path)
	if err != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("existing file was rewritten")
	}
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestDownloadFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		content string
		mime    string
		length  string
	}{
		{"server error", 503, "", "", ""},
		{"html", 200, "<html>denied</html>", "text/html", ""},
		{"empty", 200, "", "application/octet-stream", ""},
		{"not executable", 200, "not a binary", "application/octet-stream", ""},
		{"truncated", 200, "MZpartial", "application/octet-stream", "1000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if test.mime != "" {
					w.Header().Set("Content-Type", test.mime)
				}
				if test.length != "" {
					w.Header().Set("Content-Length", test.length)
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.content)
			})
			folder := t.TempDir()
			if _, err := c.Download(context.Background(), Package{Name: "driver.exe", URL: c.downloadURL + "driver.exe"}, folder, nil); err == nil {
				t.Fatal("expected download error")
			}
			assertFolderEmpty(t, folder)
		})
	}
}

func TestDownloadExistingMismatch(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "MZinstaller")
	})
	folder := t.TempDir()
	path := filepath.Join(folder, "driver.exe")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Download(context.Background(), Package{Name: "driver.exe", URL: c.downloadURL + "driver.exe"}, folder, nil); err == nil {
		t.Fatal("mismatched file must not be overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatal("original file changed")
	}
}

func TestDownloadCancellation(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "MZ"+strings.Repeat("x", 8192))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	folder := t.TempDir()
	_, err := c.Download(ctx, Package{Name: "driver.exe", URL: c.downloadURL + "driver.exe"}, folder, func(p Progress) {
		if p.Bytes > 0 {
			cancel()
		}
	})
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected cancellation: %v", err)
	}
	assertFolderEmpty(t, folder)
}

func TestDownloadUnknownLength(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "MZinstaller")
	})
	folder := t.TempDir()
	var last Progress
	path, err := c.Download(context.Background(), Package{Name: "driver.exe", URL: c.downloadURL + "driver.exe"}, folder, func(p Progress) { last = p })
	if err != nil || path == "" || last.Total != -1 || last.Bytes != 11 {
		t.Fatalf("path=%q err=%v progress=%+v", path, err, last)
	}
}

func TestUnsafeFilenames(t *testing.T) {
	c := NewClient()
	for _, name := range []string{"", "../evil.exe", `..\evil.exe`, `C:\evil.exe`, "CON.exe", "NUL", "trailing.", "trailing ", "control\x00.exe"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			folder := t.TempDir()
			if _, err := c.Download(context.Background(), Package{Name: name, URL: c.downloadURL + "unused"}, folder, nil); err == nil {
				t.Fatal("unsafe name accepted")
			}
			assertFolderEmpty(t, folder)
		})
	}
}
