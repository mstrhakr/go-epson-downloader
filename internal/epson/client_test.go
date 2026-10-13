package epson

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewClient()
	c.supportURL = server.URL
	c.downloadURL = server.URL + "/drivers/"
	t.Cleanup(c.http.CloseIdleConnections)
	return c
}

func TestNormalizeModel(t *testing.T) {
	for input, want := range map[string]string{
		"Epson WorkForce Pro WF-C5790": "wf-c5790",
		"Epson WorkForce WF C5790":     "wf-c5790",
		"WF5790":                       "wf-5790",
		"AM C4000":                     "am-c4000",
		" M5799 ":                      "m5799",
		"epson printer":                "",
		"":                             "",
	} {
		if got := normalizeModel(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestSearchModels(t *testing.T) {
	var query string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("searchText")
		fmt.Fprint(w, `<html>
<a title="Epson WorkForce WF-C5790" href="/Support/Printers/All-In-Ones/WF-C5790/s/SPT_1">Printer</a>
<a title="Epson WorkForce WF-C5790" href="/Support/Printers/All-In-Ones/WF-C5790/s/SPT_1?ignored=1">Duplicate</a>
<a title="Epson WorkForce WF-C5790" href="https://untrusted.invalid/Support/Printers/WF-C5790">External</a>
<a title="WF-C5790 review" href="/Reviews/WF-C5790">Review</a>
<a title="Epson WF-C5710" href="/Support/Printers/WF-C5710">Different</a>
</html>`)
	})
	models, err := c.SearchModels(context.Background(), "Epson WorkForce WF C5790")
	if err != nil {
		t.Fatal(err)
	}
	if query != "wf-c5790" || len(models) != 1 || models[0].URL != c.supportURL+"/Support/Printers/All-In-Ones/WF-C5790/s/SPT_1" {
		t.Fatalf("query=%q models=%+v", query, models)
	}
}

func TestSearchFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		query  string
		status int
	}{
		{"empty", "", 200},
		{"missing model", "WF-C5790", 200},
		{"server error", "WF-C5790", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) })
			if _, err := c.SearchModels(context.Background(), test.query); err == nil {
				t.Fatal("expected an explicit error")
			}
		})
	}
}

func TestDriverPackages(t *testing.T) {
	var filter string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		filter = r.URL.Query().Get("review-filter")
		fmt.Fprint(w, `<html>
<select id="review-filter"><option value="Windows 11">Windows 11</option><option value="Windows 10 64-bit">Windows 10 64-bit</option></select>
<a href="/drivers/MODEL_Lite_NA.exe" data-dl_name="MODEL_Lite_NA.exe">Lite</a>
<a href="/drivers/MODEL_STD_Core_X64.exe" data-dl_name="MODEL_STD_Core_X64.exe">Core</a>
<a href="/drivers/MODEL_STD_X64.exe" data-dl_name="MODEL_STD_X64.exe">Standard</a>
<a href="/drivers/MODEL_Lite_NA.exe" data-dl_name="MODEL_Lite_NA.exe">Duplicate</a>
<a href="/drivers/Firmware.exe" data-dl_name="Firmware.exe">Unrelated</a>
<a href="/drivers/MODEL_STD_X64.exe">No download metadata</a>
<a href="https://untrusted.invalid/MODEL_STD.exe" data-dl_name="MODEL_STD.exe">External</a>
</html>`)
	})
	for _, osFilter := range []string{"Windows 11", "Windows 10 64-bit"} {
		downloads, err := c.Downloads(context.Background(), Model{Name: "Printer", URL: c.supportURL + "/Support/Printers/test"}, osFilter)
		if err != nil {
			t.Fatal(err)
		}
		packages := downloads.Packages
		if filter != osFilter || len(packages) != 3 {
			t.Fatalf("filter=%q packages=%+v", filter, packages)
		}
		want := []string{"Lite", "Standard Core", "Standard"}
		for i, pkg := range packages {
			if pkg.Kind != want[i] {
				t.Errorf("kind %q != %q", pkg.Kind, want[i])
			}
		}
	}
	if _, err := c.Downloads(context.Background(), Model{URL: c.supportURL}, "macOS"); err == nil {
		t.Fatal("unsupported OS should fail")
	}
}

func TestUntrustedRedirect(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://untrusted.invalid/file.exe", http.StatusFound)
	})
	if _, err := c.page(context.Background(), c.supportURL); err == nil || !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("expected blocked redirect, got %v", err)
	}
}

func TestAdminRange(t *testing.T) {
	var requested []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("unexpected method: %s", r.Method)
		}
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/drivers/EDA_590.exe" {
			w.Header().Set("Content-Length", "100")
			return
		}
		http.NotFound(w, r)
	})
	pkg, version, err := c.AdminPackage(context.Background(), 570, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if version != 590 || pkg.Name != "EDA_590.exe" || !reflect.DeepEqual(requested, []string{"/drivers/EDA_600.exe", "/drivers/EDA_590.exe"}) {
		t.Fatalf("version=%d package=%+v requests=%v", version, pkg, requested)
	}
}

func TestAdminSeedAndErrors(t *testing.T) {
	for _, status := range []int{200, 404, 403, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requested []string
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				requested = append(requested, r.URL.Path)
				if strings.HasSuffix(r.URL.Path, "570.exe") {
					w.Header().Set("Content-Length", "20")
					w.WriteHeader(status)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			})
			_, version, err := c.AdminPackage(context.Background(), 570, 1, nil)
			if status == 200 {
				if err != nil || version != 570 {
					t.Fatalf("seed not checked: %d, %v", version, err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
			if len(requested) != 2 {
				t.Fatalf("requests=%v", requested)
			}
		})
	}
	t.Run("stop on server error", func(t *testing.T) {
		calls := 0
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(503)
		})
		if _, _, err := c.AdminPackage(context.Background(), 570, 10, nil); err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	})
}

func TestAdminLocalState(t *testing.T) {
	folder := t.TempDir()
	if err := RememberAdminVersion(folder, 590); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"EDA_610.exe": "MZ", "EDA_700.exe": "", "other.exe": "MZ"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if version, err := AdminStartVersion(folder, 570); err != nil || version != 610 {
		t.Fatalf("got %d, %v", version, err)
	}
	if err := os.WriteFile(filepath.Join(folder, "latest_version.txt"), []byte("invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AdminStartVersion(folder, 570); err == nil {
		t.Fatal("invalid remembered state should be reported")
	}
}

func TestLive(t *testing.T) {
	if os.Getenv("EPSON_LIVE_TEST") != "1" {
		t.Skip("set EPSON_LIVE_TEST=1 to query live Epson endpoints")
	}
	c := NewClient()
	defer c.http.CloseIdleConnections()
	ctx := context.Background()
	t.Run("drivers", func(t *testing.T) {
		for _, query := range []string{"WF-C5790", "AM-C4000"} {
			models, err := c.SearchModels(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			for _, osFilter := range []string{"Windows 11", "Windows 10 64-bit"} {
				downloads, err := c.Downloads(ctx, models[0], osFilter)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s [%s]: %d resources, %d OS choices", models[0].Name, downloads.SelectedOS, len(downloads.Packages), len(downloads.OperatingSystems))
				verified := make(map[string]bool)
				for _, pkg := range downloads.Packages {
					key := pkg.Category
					if pkg.Kind == "Standard" || pkg.Kind == "Standard Core" {
						key += " " + pkg.Kind
					}
					if pkg.OpenOnly || verified[key] {
						continue
					}
					verified[key] = true
					verified[pkg.Category] = true
					resp, err := c.request(ctx, http.MethodHead, pkg.URL)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK || resp.ContentLength == 0 {
						t.Fatalf("unavailable package: %s HTTP %d length %d", pkg.Name, resp.StatusCode, resp.ContentLength)
					}
					t.Logf("Verified %s: %s", pkg.Category, pkg.Name)
				}
				for _, category := range []string{"Drivers", "Firmware", "Manuals"} {
					if !verified[category] {
						t.Errorf("expected live %s resource for %s", category, query)
					}
				}
			}
		}
	})
	t.Run("other operating systems", func(t *testing.T) {
		models, err := c.SearchModels(ctx, "WF-C5790")
		if err != nil {
			t.Fatal(err)
		}
		initial, err := c.Downloads(ctx, models[0], "")
		if err != nil {
			t.Fatal(err)
		}
		for _, osFilter := range initial.OperatingSystems {
			if osFilter != "Windows 7 32-bit" && osFilter != "Windows Server 2022" && osFilter != "Linux" && !strings.HasPrefix(osFilter, "macOS 15.") {
				continue
			}
			downloads, err := c.Downloads(ctx, models[0], osFilter)
			if err != nil {
				t.Fatal(err)
			}
			hasDriver := false
			for _, pkg := range downloads.Packages {
				hasDriver = hasDriver || pkg.Category == "Drivers"
			}
			if !hasDriver || downloads.SelectedOS != osFilter {
				t.Errorf("%s: driver resources missing or OS substituted", osFilter)
			}
			t.Logf("%s: %d resources", downloads.SelectedOS, len(downloads.Packages))
		}
	})
	t.Run("admin", func(t *testing.T) {
		release, err := c.PublishedAdmin(ctx, "Windows 11")
		if err != nil {
			t.Fatal(err)
		}
		if err := c.VerifyPackage(ctx, release.Package); err != nil {
			t.Fatal(err)
		}
		t.Logf("Published version %s -> %s, %d OS choices", release.DisplayVersion, release.Package.URL, len(release.OperatingSystems))
	})
}
