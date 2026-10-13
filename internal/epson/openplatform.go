package epson

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	OpenPlatformURL = "https://openplatform.epson.biz/license-op/inputInformation.html"
	// MaxSerialsPerRequest is the Epson license site's per-file limit.
	MaxSerialsPerRequest = 150
)

var serialPattern = regexp.MustCompile(`^[A-Z0-9]{5,15}$`)

// The license site shows an area dropdown plus one country dropdown per area, in this order.
var areaCountrySelects = map[string]string{
	"Asia": "asiaSelect", "Euro": "europeSelect", "MEA": "meAfricaSelect",
	"NA": "northAmericaSelect", "LA": "latinAmericaSelect", "Oceania": "oceaniaSelect",
}

type Option struct {
	Value string
	Label string
}

type Area struct {
	Option
	Countries []Option
}

type OpenPlatformOptions struct {
	Areas        []Area
	Applications []Option
}

type KeyRequest struct {
	Serials     []string
	Area        string
	Country     string
	Application string
	// OtherApplication is required by Epson when Application is "Other".
	OtherApplication string
}

type ProductKey struct {
	Serial string
	Key    string
}

type OpenPlatformClient struct {
	address   string
	timeout   time.Duration
	transport http.RoundTripper
}

func NewOpenPlatformClient() *OpenPlatformClient {
	return &OpenPlatformClient{address: OpenPlatformURL, timeout: 45 * time.Second}
}

// ParseSerials accepts serials separated by lines, spaces, commas, semicolons, or tabs.
// It uppercases and de-duplicates them while preserving order.
func ParseSerials(text string) ([]string, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';' || r == '\t' || r == '\r' || r == '\n' || r == ' ' || r == '\uFEFF'
	})
	var serials, invalid []string
	seen := make(map[string]bool)
	for _, field := range fields {
		serial := strings.ToUpper(strings.Trim(field, `"'`))
		if serial == "" || seen[serial] {
			continue
		}
		seen[serial] = true
		if serialPattern.MatchString(serial) {
			serials = append(serials, serial)
		} else {
			invalid = append(invalid, field)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid serial number(s): %s; Epson accepts 5-15 letters or digits", strings.Join(invalid, ", "))
	}
	if len(serials) == 0 {
		return nil, fmt.Errorf("enter at least one printer serial number")
	}
	return serials, nil
}

func (c *OpenPlatformClient) newHTTP() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(c.address)
	if err != nil {
		return nil, err
	}
	transport := c.transport
	if transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.ResponseHeaderTimeout = 30 * time.Second
		transport = t
	}
	return &http.Client{
		Jar: jar, Transport: transport, Timeout: c.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return sameOrigin(base, req.URL)
		},
	}, nil
}

func sameOrigin(base, u *url.URL) error {
	if u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil {
		return fmt.Errorf("untrusted Epson Open Platform URL: %s", u.Redacted())
	}
	return nil
}

type formPage struct {
	url string
	doc *html.Node
}

func (c *OpenPlatformClient) do(ctx context.Context, client *http.Client, method, address, contentType string, body io.Reader, referer string) (formPage, error) {
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return formPage{}, err
	}
	base, err := url.Parse(c.address)
	if err != nil {
		return formPage{}, err
	}
	if err := sameOrigin(base, req.URL); err != nil {
		return formPage{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) EpsonDownloader/0.1")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return formPage{}, fmt.Errorf("contact Epson license site: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return formPage{}, fmt.Errorf("Epson license site returned HTTP %d", resp.StatusCode)
	}
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return formPage{}, fmt.Errorf("read Epson license site: %w", err)
	}
	if len(data) > limit {
		return formPage{}, fmt.Errorf("Epson license page exceeds %d bytes", limit)
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return formPage{}, err
	}
	return formPage{url: resp.Request.URL.String(), doc: doc}, nil
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && match(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := find(child, match); found != nil {
			return found
		}
	}
	return nil
}

func findAll(n *html.Node, match func(*html.Node) bool, out *[]*html.Node) {
	if n.Type == html.ElementNode && match(n) {
		*out = append(*out, n)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		findAll(child, match, out)
	}
}

func byID(n *html.Node, id string) *html.Node {
	return find(n, func(n *html.Node) bool { v, _ := attr(n, "id"); return v == id })
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func selectOptions(sel *html.Node) []Option {
	var nodes []*html.Node
	findAll(sel, func(n *html.Node) bool { return n.Data == "option" }, &nodes)
	var options []Option
	for _, n := range nodes {
		value, _ := attr(n, "value")
		if value == "" || value == "0" {
			continue
		}
		options = append(options, Option{Value: value, Label: text(n)})
	}
	return options
}

// formField is one successful form control, kept in document order like a browser submission.
type formField struct {
	id, name, value string
}

func formFields(form *html.Node) []formField {
	var controls []*html.Node
	findAll(form, func(n *html.Node) bool { return n.Data == "input" || n.Data == "select" }, &controls)
	var fields []formField
	for _, n := range controls {
		name, ok := attr(n, "name")
		if !ok || name == "" {
			continue
		}
		if _, disabled := attr(n, "disabled"); disabled {
			continue
		}
		id, _ := attr(n, "id")
		if n.Data == "select" {
			var options []*html.Node
			findAll(n, func(n *html.Node) bool { return n.Data == "option" }, &options)
			value := ""
			for i, o := range options {
				v, _ := attr(o, "value")
				if _, selected := attr(o, "selected"); selected || i == 0 {
					value = v
				}
			}
			fields = append(fields, formField{id, name, value})
			continue
		}
		kind, _ := attr(n, "type")
		value, _ := attr(n, "value")
		switch strings.ToLower(kind) {
		case "submit", "button", "file", "image", "reset":
			continue
		case "radio", "checkbox":
			if _, checked := attr(n, "checked"); !checked {
				continue
			}
		}
		fields = append(fields, formField{id, name, value})
	}
	return fields
}

func controlName(form *html.Node, id string) (string, error) {
	var nodes []*html.Node
	findAll(form, func(n *html.Node) bool { v, _ := attr(n, "id"); return v == id }, &nodes)
	if len(nodes) == 0 {
		return "", fmt.Errorf("Epson license page changed: missing %q", id)
	}
	for _, n := range nodes {
		if name, _ := attr(n, "name"); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("Epson license page changed: %q has no name", id)
}

func pageErrors(doc *html.Node) string {
	var messages []string
	for _, id := range []string{"allMessages", "errorMsg"} {
		if n := byID(doc, id); n != nil {
			if t := text(n); t != "" {
				messages = append(messages, t)
			}
		}
	}
	return strings.Join(messages, " ")
}

func (c *OpenPlatformClient) inputForm(ctx context.Context, client *http.Client) (formPage, *html.Node, error) {
	page, err := c.do(ctx, client, http.MethodGet, c.address, "", nil, "")
	if err != nil {
		return page, nil, err
	}
	form := byID(page.doc, "uploadForm")
	if form == nil {
		return page, nil, fmt.Errorf("Epson license page changed: input form not found")
	}
	return page, form, nil
}

// Options loads the current area, country, and application choices from Epson's license site.
func (c *OpenPlatformClient) Options(ctx context.Context) (OpenPlatformOptions, error) {
	client, err := c.newHTTP()
	if err != nil {
		return OpenPlatformOptions{}, err
	}
	_, form, err := c.inputForm(ctx, client)
	if err != nil {
		return OpenPlatformOptions{}, err
	}
	return parseOpenPlatformOptions(form)
}

func parseOpenPlatformOptions(form *html.Node) (OpenPlatformOptions, error) {
	var result OpenPlatformOptions
	area := byID(form, "areaSelect")
	apps := byID(form, "applicationSelect")
	if area == nil || apps == nil {
		return result, fmt.Errorf("Epson license page changed: area or application list not found")
	}
	for _, option := range selectOptions(area) {
		id, ok := areaCountrySelects[option.Value]
		if !ok {
			continue
		}
		countries := byID(form, id)
		if countries == nil {
			continue
		}
		result.Areas = append(result.Areas, Area{Option: option, Countries: selectOptions(countries)})
	}
	result.Applications = selectOptions(apps)
	if len(result.Areas) == 0 || len(result.Applications) == 0 {
		return result, fmt.Errorf("Epson license page changed: no areas or applications found")
	}
	return result, nil
}

// IssueKeys requests product keys in batches of up to MaxSerialsPerRequest serials.
// Epson generates keys without confirming that a serial belongs to a real printer,
// so a mistyped serial yields a valid-looking but useless key.
func (c *OpenPlatformClient) IssueKeys(ctx context.Context, req KeyRequest, progress func(done, total int)) ([]ProductKey, error) {
	if len(req.Serials) == 0 {
		return nil, fmt.Errorf("enter at least one printer serial number")
	}
	for _, serial := range req.Serials {
		if !serialPattern.MatchString(serial) {
			return nil, fmt.Errorf("invalid serial number %q", serial)
		}
	}
	if req.Area == "" || req.Country == "" || req.Application == "" {
		return nil, fmt.Errorf("choose an area, country/region, and main application")
	}
	if _, ok := areaCountrySelects[req.Area]; !ok {
		return nil, fmt.Errorf("unknown area %q", req.Area)
	}
	if req.Application == "Other" && strings.TrimSpace(req.OtherApplication) == "" {
		return nil, fmt.Errorf("enter the application name when the main application is Other")
	}
	var keys []ProductKey
	for start := 0; start < len(req.Serials); start += MaxSerialsPerRequest {
		end := min(start+MaxSerialsPerRequest, len(req.Serials))
		batch := req
		batch.Serials = req.Serials[start:end]
		issued, err := c.issueBatch(ctx, batch)
		if err != nil {
			if start > 0 {
				return keys, fmt.Errorf("serials %d-%d: %w", start+1, end, err)
			}
			return keys, err
		}
		keys = append(keys, issued...)
		if progress != nil {
			progress(len(keys), len(req.Serials))
		}
	}
	return keys, nil
}

func (c *OpenPlatformClient) issueBatch(ctx context.Context, req KeyRequest) ([]ProductKey, error) {
	client, err := c.newHTTP()
	if err != nil {
		return nil, err
	}
	page, form, err := c.inputForm(ctx, client)
	if err != nil {
		return nil, err
	}
	options, err := parseOpenPlatformOptions(form)
	if err != nil {
		return nil, err
	}
	if !hasCountry(options, req.Area, req.Country) {
		return nil, fmt.Errorf("Epson does not list country/region %q in area %q", req.Country, req.Area)
	}
	if !hasOption(options.Applications, req.Application) {
		return nil, fmt.Errorf("Epson does not list application %q", req.Application)
	}

	overrides := map[string]string{
		"selectLocale": "en",
		"areaSelect":   req.Area, areaCountrySelects[req.Area]: req.Country,
		"applicationSelect": req.Application,
	}
	for _, id := range areaCountrySelects {
		if id != areaCountrySelects[req.Area] {
			overrides[id] = "0"
		}
	}
	fileMode := byID(form, "selectInputSerialNo-2")
	if fileMode == nil {
		return nil, fmt.Errorf("Epson license page changed: file upload option not found")
	}
	fileModeName, _ := attr(fileMode, "name")
	fileModeValue, _ := attr(fileMode, "value")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range formFields(form) {
		if field.id == "serialNoText" || field.id == "applicationNameText" {
			continue
		}
		name, value := field.name, field.value
		if field.name == fileModeName {
			value = fileModeValue
		} else if v, ok := overrides[field.id]; ok {
			value = v
		}
		if err := writer.WriteField(name, value); err != nil {
			return nil, err
		}
	}
	if req.Application == "Other" {
		name, err := controlName(form, "applicationNameText")
		if err != nil {
			return nil, err
		}
		if err := writer.WriteField(name, strings.TrimSpace(req.OtherApplication)); err != nil {
			return nil, err
		}
	}
	nextName, err := controlName(form, "doNext")
	if err != nil {
		return nil, err
	}
	if err := writer.WriteField(nextName, "Next"); err != nil {
		return nil, err
	}
	fileName, err := controlName(form, "serialNoFile")
	if err != nil {
		return nil, err
	}
	file, err := writer.CreateFormFile(fileName, "serials.txt")
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(file, strings.Join(req.Serials, "\r\n")+"\r\n"); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	action, _ := attr(form, "action")
	target, err := resolve(page.url, action)
	if err != nil {
		return nil, err
	}

	confirm, err := c.do(ctx, client, http.MethodPost, target.String(), writer.FormDataContentType(), &body, page.url)
	if err != nil {
		return nil, err
	}
	issueButton := byID(confirm.doc, "doOnceIssue")
	if issueButton == nil {
		return nil, siteError(confirm.doc, "the serial numbers were not accepted")
	}
	confirmForm := find(confirm.doc, func(n *html.Node) bool {
		return n.Data == "form" && byID(n, "doOnceIssue") != nil
	})
	values := url.Values{}
	for _, field := range formFields(confirmForm) {
		if field.id == "selectLocale" {
			field.value = "en"
		}
		values.Add(field.name, field.value)
	}
	issueName, _ := attr(issueButton, "name")
	issueValue, _ := attr(issueButton, "value")
	values.Add(issueName, issueValue)
	action, _ = attr(confirmForm, "action")
	target, err = resolve(confirm.url, action)
	if err != nil {
		return nil, err
	}

	result, err := c.do(ctx, client, http.MethodPost, target.String(), "application/x-www-form-urlencoded", strings.NewReader(values.Encode()), confirm.url)
	if err != nil {
		return nil, err
	}
	keys, err := parseProductKeys(result.doc)
	if err != nil {
		return nil, err
	}
	if len(keys) != len(req.Serials) {
		return keys, fmt.Errorf("Epson returned %d product key(s) for %d serial number(s)", len(keys), len(req.Serials))
	}
	for i, key := range keys {
		if key.Serial != req.Serials[i] {
			return keys, fmt.Errorf("Epson returned a key for %s when %s was expected", key.Serial, req.Serials[i])
		}
	}
	return keys, nil
}

func siteError(doc *html.Node, fallback string) error {
	if message := pageErrors(doc); message != "" {
		return errors.New("Epson license site: " + message)
	}
	return errors.New("Epson license site: " + fallback)
}

func parseProductKeys(doc *html.Node) ([]ProductKey, error) {
	body := byID(doc, "productKeyListItems")
	if body == nil {
		return nil, siteError(doc, "no product keys were returned")
	}
	var rows []*html.Node
	findAll(body, func(n *html.Node) bool { return n.Data == "tr" }, &rows)
	var keys []ProductKey
	for _, row := range rows {
		serial := byID(row, "serialNo")
		key := byID(row, "productKey")
		if serial == nil || key == nil {
			continue
		}
		pk := ProductKey{Serial: strings.ToUpper(text(serial)), Key: text(key)}
		if pk.Serial != "" && pk.Key != "" {
			keys = append(keys, pk)
		}
	}
	if len(keys) == 0 {
		return nil, siteError(doc, "no product keys were returned")
	}
	return keys, nil
}

// AppendKeyLog appends issued keys to a CSV file, writing a header when the file is new.
func AppendKeyLog(path string, keys []ProductKey, country, application string, issued time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create key log folder: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open key log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w := csv.NewWriter(file)
	if info.Size() == 0 {
		w.Write([]string{"Issued", "Serial Number", "Product Key", "Country/Region", "Application"})
	}
	stamp := issued.Format("2006-01-02 15:04:05")
	for _, key := range keys {
		w.Write([]string{stamp, key.Serial, key.Key, csvSafe(country), csvSafe(application)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return fmt.Errorf("write key log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close key log: %w", err)
	}
	return nil
}

// csvSafe keeps spreadsheet apps from treating free text as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func hasOption(options []Option, value string) bool {
	for _, o := range options {
		if o.Value == value {
			return true
		}
	}
	return false
}

func hasCountry(options OpenPlatformOptions, area, country string) bool {
	for _, a := range options.Areas {
		if a.Value == area {
			return hasOption(a.Countries, country)
		}
	}
	return false
}
