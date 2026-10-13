package epson

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const platformPCPage = `<html><body>
<p><a href="/download/files/pc/EpsonOPS_ReleaseHistory_20260713.pdf">Release History</a></p>
<h2>Quick Guide</h2>
<table><tr><th>Documentation</th><th>Last Updated</th><th>size</th><th>&nbsp;</th></tr>
<tr><td>Quick Guide</td><td>16/07/2021</td><td>36KB</td><td><a href="/download/files/pc/EpsonQuickGuideForPaperCut.pdf">pdf</a></td></tr></table>
<table id="table01">
<tr><th rowspan="2">Product</th><th colspan="2">Firmware Updater</th><th colspan="2">Documentation</th></tr>
<tr><th>Windows</th><th>Mac</th><th>Hardware Setup Guide</th><th>Administrator's Guide (updated)</th></tr>
<tr><td>WF-6590 Series</td>
<td><a href="/download/files/op/WF-6590Series_DB30a1.exe">exe <span>(44.7MB)</span></a><div>Version:DB30a1<br>Update: 05/03/2026</div></td>
<td><a href="/download/files/op/WF-6590Series_DB30a1.dmg">dmg <span>(49.1MB)</span></a><div>Version:DB30a1<br>Update: 05/03/2026</div></td>
<td><a href="/download/files/op/eop_hardware_setup_guide_en.pdf">pdf <span>(511KB)</span></a><div>Update: 26/02/2016</div></td>
<td><a href="/download/files/op/adm_guide_for_WF-6590_en.pdf">pdf <span>(2.2MB)</span></a><div>Update: 26/02/2016</div></td></tr>
<tr><td>LP-M8180A<br>LP-M8180F</td><td><br><div>&#x2015;</div></td><td><br><div>&#x2015;</div></td>
<td><a href="/download/files/op/lpm8180_eop_hwsg_npd6723-00_ja.pdf">pdf (916KB)</a><div>Update: 01/03/2021</div></td><td>&#x2015;</td></tr>
</table>
<h3>Windows</h3>
<table id="table03"><tr><th>Module</th><th>Version</th><th>Last Updated</th><th>size</th><th></th></tr>
<tr><td>Epson Open Platform Solution for PaperCut (Win)</td><td>2.7.4.0002</td><td>13/07/2026</td><td>69.2MB</td>
<td><a href="/download/files/pc/EpsonOpenPlatformSolutionForPaperCut_2_7_4_0002_1165.exe" class="disabled" disabled>exe</a></td></tr></table>
<h3>Linux</h3>
<table id="table05"><tr><th>Module</th><th>Version</th><th>Last Updated</th><th>size</th><th></th></tr>
<tr><td>Epson Open Platform Solution for PaperCut (Linux rpm package 64bit)</td><td>2.7.4</td><td>22/05/2026</td><td>69.6MB</td>
<td><a href="/download/files/pc/EpsonOpenPlatformSolutionForPaperCut-2.7.4-1.x86_64.rpm">rpm</a></td></tr></table>
<table id="table06"><tr><th>List</th><th>Last Updated</th><th>size</th><th></th></tr>
<tr><td>Card Reader Compatible List</td><td>07/10/2025</td><td>1.5MB</td><td><a href="/download/files/op/card_reader_support_list_0115_en.pdf">pdf</a></td></tr></table>
</body></html>`

const platformOPPage = `<html><body>
<table id="table01">
<tr><th rowspan="2">Product</th><th colspan="2">Firmware Updater</th><th colspan="2">Documentation</th></tr>
<tr><th>Windows</th><th>Mac</th><th>Hardware Setup Guide</th><th>Administrator's Guide</th></tr>
<tr><td>WF-8590 Series</td><td>&#x2015;</td><td>&#x2015;</td>
<td><a href="/download/files/op/eop_hardware_setup_guide_en.pdf">pdf (511KB)</a><div>Update: 26/02/2016</div></td>
<td><a href="https://evil.example/download/files/op/adm.pdf">pdf</a></td></tr>
<tr><td>WF-C5790 Series</td><td>&#x2015;</td><td>&#x2015;</td>
<td><a href="/download/files/op/eop_hardware_setup_guide_npd5941-01_en.pdf">pdf (307KB)</a><div>Update: 09/11/2018</div></td><td>&#x2015;</td></tr>
</table>
<table id="table02"><tr><th>List</th><th>Last Updated</th><th>size</th><th></th></tr>
<tr><td>Card Reader Compatible List</td><td>07/10/2025</td><td>1.5MB</td><td><a href="/download/files/op/card_reader_support_list_0115_en.pdf">pdf</a></td></tr></table>
<table id="table03"><tr><th>File</th><th>Last Updated</th><th>size</th><th></th></tr>
<tr><td>Profiles of security configuration</td><td>10/02/2016</td><td>4KB</td><td><a href="/download/files/op/profile_0100.zip">zip</a></td></tr></table>
<a href="/download/opg/index.htm">Operational parameter generator</a>
</body></html>`

func platformClient(t *testing.T) *Client {
	t.Helper()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case platformPaperCut:
			w.Write([]byte(platformPCPage))
		case platformGeneral:
			w.Write([]byte(platformOPPage))
		default:
			http.NotFound(w, r)
		}
	})
	c.platformURL = c.supportURL
	return c
}

func resourceByName(t *testing.T, d PlatformDownloads, name string) PlatformResource {
	t.Helper()
	for _, r := range d.Resources {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("missing %s", name)
	return PlatformResource{}
}

func TestPlatformDownloads(t *testing.T) {
	d, err := platformClient(t).PlatformDownloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Resources) != 13 {
		for _, r := range d.Resources {
			t.Log(r.Category, r.Name, r.Models)
		}
		t.Fatalf("got %d resources", len(d.Resources))
	}
	if d.Resources[0].Category != "PaperCut software" || d.Resources[len(d.Resources)-1].Category != "Tools" {
		t.Errorf("unexpected ordering: first %s, last %s", d.Resources[0].Category, d.Resources[len(d.Resources)-1].Category)
	}
	want := []string{"LP-M8180A", "LP-M8180F", "WF-6590 Series", "WF-8590 Series", "WF-C5790 Series"}
	if !slices.Equal(d.Models, want) {
		t.Errorf("models = %v", d.Models)
	}
	win := resourceByName(t, d, "EpsonOpenPlatformSolutionForPaperCut_2_7_4_0002_1165.exe")
	if !win.PaperCut() || win.Kind != "Windows" || win.Version != "2.7.4.0002" || win.Updated != "2026-07-13" || win.Size != "69.2MB" {
		t.Errorf("windows installer = %+v", win)
	}
	if rpm := resourceByName(t, d, "EpsonOpenPlatformSolutionForPaperCut-2.7.4-1.x86_64.rpm"); rpm.Kind != "Linux (rpm)" {
		t.Errorf("rpm kind = %s", rpm.Kind)
	}
	fw := resourceByName(t, d, "WF-6590Series_DB30a1.exe")
	if fw.PaperCut() || fw.Category != "Firmware" || fw.Version != "DB30a1" || fw.Updated != "2026-03-05" || fw.Size != "44.7MB" || !slices.Equal(fw.Models, []string{"WF-6590 Series"}) {
		t.Errorf("firmware = %+v", fw)
	}
	if mac := resourceByName(t, d, "WF-6590Series_DB30a1.dmg"); mac.Kind != "macOS" {
		t.Errorf("mac firmware kind = %s", mac.Kind)
	}
	guide := resourceByName(t, d, "eop_hardware_setup_guide_en.pdf")
	if guide.Kind != "Hardware Setup Guide" || !slices.Equal(guide.Models, []string{"WF-6590 Series", "WF-8590 Series"}) {
		t.Errorf("shared guide = %+v", guide)
	}
	if admin := resourceByName(t, d, "adm_guide_for_WF-6590_en.pdf"); admin.Kind != "Administrator's Guide" {
		t.Errorf("admin guide kind = %q", admin.Kind)
	}
	if ja := resourceByName(t, d, "lpm8180_eop_hwsg_npd6723-00_ja.pdf"); ja.Title != "Hardware Setup Guide (Japanese)" || len(ja.Models) != 2 {
		t.Errorf("japanese guide = %+v", ja)
	}
	if history := resourceByName(t, d, "EpsonOPS_ReleaseHistory_20260713.pdf"); history.Title != "Release history" || history.Updated != "2026-07-13" || !history.PaperCut() {
		t.Errorf("release history = %+v", history)
	}
	if profile := resourceByName(t, d, "profile_0100.zip"); profile.Category != "Tools" || profile.Kind != "Security profile" {
		t.Errorf("profile = %+v", profile)
	}
	if tool := resourceByName(t, d, "(open in browser)"); !tool.OpenOnly || !strings.HasSuffix(tool.URL, "/download/opg/index.htm") {
		t.Errorf("parameter generator = %+v", tool)
	}
	for _, r := range d.Resources {
		if strings.Contains(r.URL, "evil.example") {
			t.Errorf("foreign link accepted: %s", r.URL)
		}
	}
}

func TestPlatformModelMatching(t *testing.T) {
	for input, want := range map[string]string{
		"WF-6590 Series":               "wf6590",
		"Epson WorkForce Pro WF-6590":  "wf6590",
		"wf6590":                       "wf6590",
		"AM-C4000 Series":              "amc4000",
		"Epson WorkForce Pro WF-C5790": "wfc5790",
	} {
		if got := PlatformModelKey(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
	d, err := platformClient(t).PlatformDownloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range d.ForModel("Epson WorkForce Pro WF-6590") {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	want := []string{"WF-6590Series_DB30a1.dmg", "WF-6590Series_DB30a1.exe", "adm_guide_for_WF-6590_en.pdf", "eop_hardware_setup_guide_en.pdf"}
	if !slices.Equal(names, want) {
		t.Errorf("ForModel = %v", names)
	}
	if got := d.ForModel("WF-65"); len(got) != 0 {
		t.Errorf("partial model should not match exactly: %d", len(got))
	}
	if got := d.Filter("", "All"); len(got) != len(d.Resources) {
		t.Errorf("empty filter returned %d", len(got))
	}
	model := d.Filter("WF-6590", "All")
	general := 0
	for _, r := range model {
		if len(r.Models) == 0 {
			general++
		} else if !slices.Contains(r.Models, "WF-6590 Series") {
			t.Errorf("unrelated model item: %s %v", r.Name, r.Models)
		}
	}
	if general == 0 || len(model) != general+4 {
		t.Errorf("model filter returned %d items, %d general", len(model), general)
	}
	if got := d.Filter("WF-6590", "Firmware"); len(got) != 2 {
		t.Errorf("firmware filter returned %d", len(got))
	}
	if got := d.Filter("rpm", "All"); len(got) != 1 || got[0].Kind != "Linux (rpm)" {
		t.Errorf("keyword filter = %+v", got)
	}
	if got := d.Filter("LP-M8180F", "All"); !slices.ContainsFunc(got, func(r PlatformResource) bool { return strings.HasSuffix(r.Name, "_ja.pdf") }) {
		t.Errorf("multi-model row did not match its second model")
	}
}

func TestPlatformDownloadsErrors(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html><body>maintenance</body></html>")) })
	c.platformURL = c.supportURL
	if _, err := c.PlatformDownloads(context.Background()); err == nil || !strings.Contains(err.Error(), "no Open Platform downloads") {
		t.Errorf("empty pages: %v", err)
	}
	c = testClient(t, http.NotFound)
	c.platformURL = c.supportURL
	if _, err := c.PlatformDownloads(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("missing page: %v", err)
	}
}

func TestDownloadChecksLinuxPackageHeaders(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".rpm") {
			w.Write([]byte("\xed\xab\xee\xdbpayload"))
			return
		}
		w.Write([]byte("not a deb archive"))
	})
	folder := t.TempDir()
	path, err := c.Download(context.Background(), Package{Name: "a.rpm", URL: c.supportURL + "/files/a.rpm"}, folder, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	_, err = c.Download(context.Background(), Package{Name: "b.deb", URL: c.supportURL + "/files/b.deb"}, folder, nil)
	if err == nil || !strings.Contains(err.Error(), "expected .deb file header") {
		t.Errorf("bad deb: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "b.deb")); !os.IsNotExist(err) {
		t.Errorf("bad deb was kept: %v", err)
	}
}

func TestLivePlatformDownloads(t *testing.T) {
	if os.Getenv("EPSON_LIVE_TEST") != "1" {
		t.Skip("set EPSON_LIVE_TEST=1 to query Epson's Open Platform download pages")
	}
	d, err := NewClient().PlatformDownloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hasPaperCut, hasFirmware := false, false
	for _, r := range d.Resources {
		hasPaperCut = hasPaperCut || r.Category == "PaperCut software" && r.Kind == "Windows" && r.Version != ""
		hasFirmware = hasFirmware || r.Category == "Firmware"
	}
	if !hasPaperCut || !hasFirmware || len(d.Models) < 10 {
		t.Errorf("unexpected live result: papercut=%v firmware=%v models=%d", hasPaperCut, hasFirmware, len(d.Models))
	}
}
