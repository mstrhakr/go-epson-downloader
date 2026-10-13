package epson

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const fixtureOS = `<select id="review-filter">
<option value="">Choose OS</option>
<option value="Windows 11">Windows 11</option>
<option value="Windows 7 32-bit">Windows 7</option>
<option value="macOS 15.x">macOS</option>
<option value="Windows Server 2022">Server</option>
<option value="Windows 11">Duplicate</option>
</select>`

func TestUnifiedDownloads(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, fixtureOS+`
<a data-dl_name="Model_STD.exe" data-dl_type="icc_drivers" data-dl_title="Printer Driver v1.0" href="/drivers/Model_STD.exe">Download</a>
<a data-dl_name="FW_NEW.zip" data-dl_type="icc_firmware" data-dl_title="Firmware v2" href="/drivers/FW_NEW.zip">Download</a>
<a data-dl_name="Updater.exe" data-dl_type="icc_utilities" data-dl_title="Software Updater" href="/drivers/Updater.exe">Download</a>
<a title="Unrelated brochure_pdf" href="/brochure.pdf">Not a product manual</a>
<section id="manuals">
<a title="User's Guide_pdf" href="/docid/guide.pdf">Guide</a>
<a title="User's Guide_pdf" href="/docid/guide.pdf">Duplicate</a>
<a title="User's Guide_html" href="/docid/guide/index.html">Online guide</a>
<a title="Not trusted_pdf" href="https://untrusted.invalid/manual.pdf">External</a>
<a title="Contact" href="/contact">Not a manual</a>
</section>`)
	})
	result, err := c.Downloads(context.Background(), Model{Name: "Model", URL: c.supportURL + "/Support/Printers/model"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedOS != "Windows 11" || len(result.OperatingSystems) != 4 || len(result.Packages) != 5 {
		t.Fatalf("unexpected result: %+v", result)
	}
	wantCategories := []string{"Drivers", "Firmware", "Utilities", "Manuals", "Manuals"}
	for i, pkg := range result.Packages {
		if pkg.Category != wantCategories[i] || pkg.Title == "" {
			t.Errorf("unexpected resource: %+v", pkg)
		}
	}
	if !result.Packages[4].OpenOnly || result.Packages[3].OpenOnly {
		t.Fatal("HTML guides should be link-only; PDFs should be downloadable")
	}
}

func TestOperatingSystemSelection(t *testing.T) {
	var requested []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Query().Get("review-filter"))
		fmt.Fprint(w, `<select id="review-filter"><option value="macOS 15.x">Mac</option></select>
<a data-dl_name="Driver.dmg" data-dl_type="icc_drivers" href="/drivers/Driver.dmg">Download</a>`)
	})
	model := Model{Name: "Printer", URL: c.supportURL}
	result, err := c.Downloads(context.Background(), model, "")
	if err != nil || result.SelectedOS != "macOS 15.x" {
		t.Fatalf("explicit initial fallback: %+v, %v", result, err)
	}
	if !reflect.DeepEqual(requested, []string{"Windows 11", "macOS 15.x"}) {
		t.Fatalf("requests=%v", requested)
	}
	if _, err := c.Downloads(context.Background(), model, "Windows 7 32-bit"); err == nil {
		t.Fatal("explicit unsupported OS must not silently fall back")
	}
}

func TestMissingOperatingSystems(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<select id="country"><option value="USA">USA</option></select>`)
	})
	if _, err := c.Downloads(context.Background(), Model{URL: c.supportURL}, ""); err == nil {
		t.Fatal("a missing OS selector must be reported")
	}
}

func TestLinuxPortalLinks(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<select id="review-filter"><option value="Linux">Linux</option></select>
<a href="https://download.ebz.epson.net/dsc/search/01/search/?OSC=LX">Linux drivers</a>
<a href="https://download.ebz.epson.net/man/linux/">Linux manuals</a>
<a href="https://download.ebz.epson.net/faq/linux/">Linux FAQ</a>`)
	})
	result, err := c.Downloads(context.Background(), Model{URL: c.supportURL}, "Linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 2 || !result.Packages[0].OpenOnly || !result.Packages[1].OpenOnly || result.Packages[0].Category != "Drivers" {
		t.Fatalf("Linux download portals should be visible browser links: %+v", result)
	}
}

func TestPublishedAdmin(t *testing.T) {
	var requests []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "EDA_590.exe") {
			w.Header().Set("Content-Length", "100")
			return
		}
		fmt.Fprint(w, fixtureOS+`<a title="Epson Device Admin&nbsp;v5.9.0 " href="#panel">Version</a>
<a title="USB Agent v5.0.0" href="/drivers/USB_Agent_500.exe">Unrelated</a>`)
	})
	release, err := c.PublishedAdmin(context.Background(), "Windows 11")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != 590 || release.DisplayVersion != "5.9.0" || release.Package.Name != "EDA_590.exe" || len(release.OperatingSystems) != 4 {
		t.Fatalf("unexpected release: %+v", release)
	}
	if err := c.VerifyPackage(context.Background(), release.Package); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET " + AdminPath, "HEAD /drivers/EDA_590.exe"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("should use a page hint and one direct-file probe, not scan: %v", requests)
	}
}

func TestPublishedAdminFailures(t *testing.T) {
	for _, test := range []struct {
		name  string
		title string
		head  int
	}{
		{"no version for selected OS", "USB Agent v5.0.0", 200},
		{"unmappable version", "Epson Device Admin v1000000.0.0", 200},
		{"direct file missing", "Epson Device Admin v5.9.0", 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(test.head)
					return
				}
				fmt.Fprintf(w, `%s<a title="%s">Version</a>`, fixtureOS, test.title)
			})
			release, err := c.PublishedAdmin(context.Background(), "Windows 11")
			if err == nil {
				err = c.VerifyPackage(context.Background(), release.Package)
			}
			if err == nil {
				t.Fatal("expected an explicit error, not automatic scanning")
			}
		})
	}
}
