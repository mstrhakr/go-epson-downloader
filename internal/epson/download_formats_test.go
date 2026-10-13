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

func TestDownloadResourceFormats(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
	}{
		{"firmware.zip", "PK\x03\x04example"},
		{"manual.pdf", "%PDF-1.7\nexample"},
		{"driver.dmg", "example-disk-image"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				fmt.Fprint(w, test.payload)
			})
			path, err := c.Download(context.Background(), Package{Name: test.name, URL: c.downloadURL + test.name}, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil || string(content) != test.payload {
				t.Fatalf("payload mismatch: %v", err)
			}
		})
	}
}

func TestDownloadBadSignatures(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "not-the-promised-file")
	})
	for _, name := range []string{"firmware.zip", "manual.pdf"} {
		folder := t.TempDir()
		if _, err := c.Download(context.Background(), Package{Name: name, URL: c.downloadURL + name}, folder, nil); err == nil {
			t.Fatalf("%s: invalid signature accepted", name)
		}
		assertFolderEmpty(t, folder)
	}
}

func TestOnlineManualIsNotDownloaded(t *testing.T) {
	c := NewClient()
	folder := t.TempDir()
	if _, err := c.Download(context.Background(), Package{Title: "Online manual", OpenOnly: true}, folder, nil); err == nil {
		t.Fatal("online manuals must be opened, not downloaded")
	}
	assertFolderEmpty(t, folder)
}

func TestLiveManualDownload(t *testing.T) {
	if os.Getenv("EPSON_LIVE_DOWNLOAD_TEST") != "1" {
		t.Skip("set EPSON_LIVE_DOWNLOAD_TEST=1 to download a small public Epson PDF")
	}
	c := NewClient()
	defer c.http.CloseIdleConnections()
	pkg := Package{
		Category: "Manuals", Title: "AM-C4000 Start Here",
		Name: "cpd62840.pdf", URL: "https://files.support.epson.com/docid/cpd6/cpd62840.pdf",
	}
	path, err := c.Download(context.Background(), pkg, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(content), "%PDF-") {
		t.Fatalf("invalid public manual download: %v", err)
	}
	t.Logf("Downloaded and verified %s (%d bytes)", filepath.Base(path), len(content))
}
