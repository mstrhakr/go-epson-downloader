package epson

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeInputPage = `<html><body>
<div class="errors">%s</div>
<form id="uploadForm" name="_id79:uploadForm" method="post" enctype="multipart/form-data" action="/license-op/inputInformation.html;jsessionid=ABC">
<select id="selectLocale" name="_id79:uploadForm:selectLocale"><option value="ja" selected="selected">日本語</option><option value="en">English</option></select>
<input type="hidden" id="selectInputSerialNoItemsSave" name="_id79:uploadForm:selectInputSerialNoItemsSave" value="" />
<input type="radio" id="selectInputSerialNo-1" name="_id79:uploadForm:selectInputSerialNo" checked="checked" value="0" />
<input type="text" id="serialNoText" name="_id79:uploadForm:serialNoText" value="" />
<input type="radio" id="selectInputSerialNo-2" name="_id79:uploadForm:selectInputSerialNo" value="1" />
<input type="file" id="serialNoFile" style="display:none;" />
<input type="file" id="serialNoFile" name="_id79:uploadForm:serialNoFile" />
<input type="hidden" id="areaSelectItemsSave" name="_id79:uploadForm:areaSelectItemsSave" value="rO0AB" />
<select id="areaSelect" name="_id79:uploadForm:areaSelect"><option value="0">--Select Area--</option><option value="Euro">Europe</option><option value="NA">North America</option></select>
<select id="firstSelect" name="_id79:uploadForm:firstSelect"><option value="0">--</option></select>
<select id="europeSelect" name="_id79:uploadForm:europeSelect"><option value="0">--</option><option value="DE">Germany</option></select>
<select id="northAmericaSelect" name="_id79:uploadForm:northAmericaSelect"><option value="0">--</option><option value="CA">Canada</option><option value="US">United States</option></select>
<select id="applicationSelect" name="_id79:uploadForm:applicationSelect"><option value="0">--</option><option value="PaperCutMF">PaperCut MF(PaperCut)</option><option value="Other">Other</option></select>
<input type="text" id="applicationNameText" name="_id79:uploadForm:applicationNameText" value="" disabled="disabled" />
<input type="submit" id="doNext" name="_id79:uploadForm:doNext" value="Next" />
<input type="hidden" name="_id79:uploadForm/inputInformation.html" value="_id79:uploadForm" />
</form></body></html>`

const fakeConfirmPage = `<html><body><form id="form" name="_id79:form" method="post" action="/license-op/confirmation.html">
<select id="selectLocale" name="_id79:form:selectLocale"><option value="ja" selected="selected">日本語</option><option value="en">English</option></select>
<input type="submit" id="doChangeLocale" name="_id79:form:doChangeLocale" />
<input type="submit" id="doOnceIssue" name="_id79:form:doOnceIssue" value="Issue" />
<input type="hidden" name="org.seasar.teeda.extension.util.TransactionTokenUtil.TOKEN" value="tok123" />
<input type="submit" id="doBack" name="_id79:form:doBack" value="Back" />
<input type="hidden" name="_id79:form/confirmation.html" value="_id79:form" />
</form></body></html>`

type fakeLicenseSite struct {
	t            *testing.T
	requests     int
	lastSerials  []string
	lastFields   map[string]string
	rejectSerial string
}

func (f *fakeLicenseSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests++
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/license-op/inputInformation.html":
		http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "S1", Path: "/license-op"})
		fmt.Fprintf(w, fakeInputPage, "")
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/license-op/inputInformation.html"):
		if c, err := r.Cookie("JSESSIONID"); err != nil || c.Value != "S1" {
			http.Error(w, "no session", http.StatusForbidden)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			f.t.Errorf("parse multipart: %v", err)
			return
		}
		f.lastFields = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			f.lastFields[k] = strings.Join(v, "|")
		}
		file, _, err := r.FormFile("_id79:uploadForm:serialNoFile")
		if err != nil {
			f.t.Errorf("missing serial file: %v", err)
			return
		}
		defer file.Close()
		var data strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := file.Read(buf)
			data.Write(buf[:n])
			if err != nil {
				break
			}
		}
		f.lastSerials = strings.Fields(data.String())
		if f.rejectSerial != "" && strings.Contains(data.String(), f.rejectSerial) {
			fmt.Fprintf(w, fakeInputPage, `<ul id="allMessages"><li>Invalid serial number. Check the content of the file.</li></ul>`)
			return
		}
		fmt.Fprint(w, fakeConfirmPage)
	case r.Method == http.MethodPost && r.URL.Path == "/license-op/confirmation.html":
		r.ParseForm()
		if r.PostForm.Get("org.seasar.teeda.extension.util.TransactionTokenUtil.TOKEN") != "tok123" ||
			r.PostForm.Get("_id79:form:doOnceIssue") != "Issue" || r.PostForm.Get("_id79:form:selectLocale") != "en" ||
			r.PostForm.Has("_id79:form:doBack") {
			fmt.Fprint(w, `<span id="errorMsg">A system error has occurred.</span>`)
			return
		}
		var rows strings.Builder
		for _, s := range f.lastSerials {
			fmt.Fprintf(&rows, `<tr><td><span id="serialNo">%s</span></td><td><span id="productKey">key-%s</span></td></tr>`, strings.ToUpper(s), strings.ToLower(s))
		}
		fmt.Fprintf(w, `<table><tbody id="productKeyListItems">%s</tbody><tbody><tr><td>&nbsp;</td><td>&nbsp;</td></tr></tbody></table>`, rows.String())
	default:
		http.NotFound(w, r)
	}
}

func newFakeOpenPlatform(t *testing.T, site *fakeLicenseSite) *OpenPlatformClient {
	server := httptest.NewTLSServer(site)
	t.Cleanup(server.Close)
	return &OpenPlatformClient{
		address:   server.URL + "/license-op/inputInformation.html",
		timeout:   10 * time.Second,
		transport: server.Client().Transport,
	}
}

func TestParseSerials(t *testing.T) {
	serials, err := ParseSerials("\uFEFFxaa1234567\r\nXAA1234567, ABCDE;\t\"ZZZ123456789012\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(serials, ","); got != "XAA1234567,ABCDE,ZZZ123456789012" {
		t.Fatalf("serials = %s", got)
	}
	for _, bad := range []string{"", " \n ", "ABCD", "ABCDEFGHIJKLMNOP", "XAV-001001"} {
		if _, err := ParseSerials(bad); err == nil {
			t.Errorf("ParseSerials(%q) succeeded", bad)
		}
	}
}

func TestOpenPlatformOptions(t *testing.T) {
	c := newFakeOpenPlatform(t, &fakeLicenseSite{t: t})
	opts, err := c.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Areas) != 2 || opts.Areas[1].Value != "NA" || len(opts.Areas[1].Countries) != 2 || opts.Areas[1].Countries[1] != (Option{"US", "United States"}) {
		t.Fatalf("areas = %+v", opts.Areas)
	}
	if len(opts.Applications) != 2 || opts.Applications[0].Label != "PaperCut MF(PaperCut)" {
		t.Fatalf("applications = %+v", opts.Applications)
	}
}

func TestIssueKeys(t *testing.T) {
	site := &fakeLicenseSite{t: t}
	c := newFakeOpenPlatform(t, site)
	keys, err := c.IssueKeys(context.Background(), KeyRequest{
		Serials: []string{"XAA1234567", "ABCDE12345"}, Area: "NA", Country: "US", Application: "PaperCutMF",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != (ProductKey{"XAA1234567", "key-xaa1234567"}) {
		t.Fatalf("keys = %+v", keys)
	}
	p := "_id79:uploadForm:"
	want := map[string]string{
		p + "selectLocale": "en", p + "selectInputSerialNo": "1", p + "areaSelect": "NA",
		p + "northAmericaSelect": "US", p + "europeSelect": "0", p + "applicationSelect": "PaperCutMF",
		p + "doNext": "Next", "_id79:uploadForm/inputInformation.html": "_id79:uploadForm",
	}
	for k, v := range want {
		if site.lastFields[k] != v {
			t.Errorf("field %s = %q, want %q", k, site.lastFields[k], v)
		}
	}
	for _, absent := range []string{p + "serialNoText", p + "applicationNameText"} {
		if _, ok := site.lastFields[absent]; ok {
			t.Errorf("unexpected field %s", absent)
		}
	}
}

func TestIssueKeysOtherApplicationAndBatches(t *testing.T) {
	site := &fakeLicenseSite{t: t}
	c := newFakeOpenPlatform(t, site)
	var serials []string
	for i := range MaxSerialsPerRequest + 1 {
		serials = append(serials, fmt.Sprintf("SN%08d", i))
	}
	var calls []int
	keys, err := c.IssueKeys(context.Background(), KeyRequest{
		Serials: serials, Area: "Euro", Country: "DE", Application: "Other", OtherApplication: "Custom App",
	}, func(done, total int) { calls = append(calls, done) })
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != len(serials) || fmt.Sprint(calls) != "[150 151]" {
		t.Fatalf("keys=%d progress=%v", len(keys), calls)
	}
	if site.lastFields["_id79:uploadForm:applicationNameText"] != "Custom App" || site.lastFields["_id79:uploadForm:northAmericaSelect"] != "0" {
		t.Fatalf("fields = %v", site.lastFields)
	}
}

func TestIssueKeysErrors(t *testing.T) {
	site := &fakeLicenseSite{t: t, rejectSerial: "BAD0000000"}
	c := newFakeOpenPlatform(t, site)
	ctx := context.Background()
	_, err := c.IssueKeys(ctx, KeyRequest{Serials: []string{"BAD0000000"}, Area: "NA", Country: "US", Application: "PaperCutMF"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Invalid serial number. Check the content") {
		t.Fatalf("site rejection err = %v", err)
	}
	cases := []KeyRequest{
		{Area: "NA", Country: "US", Application: "PaperCutMF"},
		{Serials: []string{"AB"}, Area: "NA", Country: "US", Application: "PaperCutMF"},
		{Serials: []string{"XAA1234567"}, Area: "NA", Country: "DE", Application: "PaperCutMF"},
		{Serials: []string{"XAA1234567"}, Area: "Mars", Country: "US", Application: "PaperCutMF"},
		{Serials: []string{"XAA1234567"}, Area: "NA", Country: "US", Application: "Other"},
		{Serials: []string{"XAA1234567"}, Area: "NA", Country: "US", Application: "Nope"},
	}
	before := site.requests
	for i, req := range cases {
		if _, err := c.IssueKeys(ctx, req, nil); err == nil {
			t.Errorf("case %d succeeded", i)
		}
	}
	// Only the two cases that need Epson's current option lists may contact the site, and neither may submit.
	if site.requests-before != 2 {
		t.Errorf("site requests = %d", site.requests-before)
	}
}

func TestOpenPlatformRejectsForeignRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/", http.StatusFound)
	}))
	defer server.Close()
	c := &OpenPlatformClient{address: server.URL + "/license-op/inputInformation.html", timeout: 5 * time.Second, transport: server.Client().Transport}
	if _, err := c.Options(context.Background()); err == nil || !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppendKeyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "OpenPlatformKeys.csv")
	when := time.Date(2026, 10, 13, 8, 0, 0, 0, time.Local)
	for range 2 {
		if err := AppendKeyLog(path, []ProductKey{{"XAA1234567", "abcd-efgh"}}, "United States", "=cmd", when); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][1] != "Serial Number" || rows[2][2] != "abcd-efgh" || rows[1][4] != "'=cmd" {
		t.Fatalf("rows = %v", rows)
	}
}

// TestLiveOpenPlatformOptions reads Epson's live option lists without issuing a key.
func TestLiveOpenPlatformOptions(t *testing.T) {
	if os.Getenv("EPSON_LIVE_TEST") != "1" {
		t.Skip("set EPSON_LIVE_TEST=1")
	}
	opts, err := NewOpenPlatformClient().Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasCountry(opts, "NA", "US") || !hasOption(opts.Applications, "PaperCutMF") || !hasOption(opts.Applications, "Other") {
		t.Fatalf("options = %+v", opts)
	}
	t.Logf("%d areas, %d applications", len(opts.Areas), len(opts.Applications))
}

// TestLiveOpenPlatformIssue issues a real key; set EPSON_LIVE_KEY_SERIAL to a serial you own.
func TestLiveOpenPlatformIssue(t *testing.T) {
	serial := os.Getenv("EPSON_LIVE_KEY_SERIAL")
	if serial == "" {
		t.Skip("set EPSON_LIVE_KEY_SERIAL")
	}
	keys, err := NewOpenPlatformClient().IssueKeys(context.Background(), KeyRequest{
		Serials: []string{strings.ToUpper(serial)}, Area: "NA", Country: "US", Application: "PaperCutMF",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s -> %s", keys[0].Serial, keys[0].Key)
}
