package epson

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	SupportURL  = "https://epson.com"
	DownloadURL = "https://ftp.epson.com/drivers/"
	AdminPath   = "/Support/Other-Products/Epson-Software/Epson-Device-Admin/s/SPT_DEVICEADMIN-NS"
)

type Client struct {
	http        *http.Client
	supportURL  string
	downloadURL string
	platformURL string
}

type Model struct {
	Name string
	URL  string
}

type Package struct {
	Category string
	Kind     string
	Title    string
	Name     string
	URL      string
	OpenOnly bool
}

type Progress struct {
	Name  string
	Bytes int64
	Total int64
}

func NewClient() *Client {
	c := &Client{supportURL: SupportURL, downloadURL: DownloadURL, platformURL: PlatformDownloadURL}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	c.http = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return c.checkURL(req.URL)
		},
	}
	return c
}

func (c *Client) checkURL(u *url.URL) error {
	for _, base := range []string{
		c.supportURL, c.downloadURL, c.platformURL,
		"https://files.support.epson.com", "https://download4.epson.biz",
		"https://download.ebz.epson.net",
	} {
		b, err := url.Parse(base)
		if err != nil {
			return err
		}
		if u.Scheme == b.Scheme && u.Host == b.Host && u.User == nil {
			return nil
		}
	}
	return fmt.Errorf("untrusted Epson URL: %s", u.Redacted())
}

func (c *Client) request(ctx context.Context, method, address string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if err := c.checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "EpsonDownloader/0.1")
	return c.http.Do(req)
}

func (c *Client) page(ctx context.Context, address string) (*html.Node, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resp, err := c.request(ctx, http.MethodGet, address)
	if err != nil {
		return nil, fmt.Errorf("fetch Epson page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Epson page returned HTTP %d", resp.StatusCode)
	}
	const limit = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read Epson page: %w", err)
	}
	if len(body) > limit {
		return nil, fmt.Errorf("Epson page exceeds %d bytes", limit)
	}
	return html.Parse(strings.NewReader(string(body)))
}

func anchors(n *html.Node, visit func(map[string]string)) {
	if n.Type == html.ElementNode && n.Data == "a" {
		attrs := make(map[string]string, len(n.Attr))
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		visit(attrs)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		anchors(child, visit)
	}
}

func resolve(base, ref string) (*url.URL, error) {
	b, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	return b.ResolveReference(u), nil
}

func normalizeModel(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, word := range []string{
		"all-in-one", "enterprise", "workforce", "surecolor", "perfection",
		"expression", "projector", "printer", "scanner", "stylus", "epson", "photo", "copy", "fax",
	} {
		text = strings.ReplaceAll(text, word, " ")
	}
	var parts []string
	for _, part := range strings.Fields(text) {
		if part != "pro" {
			parts = append(parts, part)
		}
	}
	text = strings.Join(parts, "")
	if !strings.Contains(text, "-") {
		for _, prefix := range []string{"wf", "am", "et", "xp", "sc", "px", "lq", "lx", "fx"} {
			if strings.HasPrefix(text, prefix) && len(text) > len(prefix) {
				return prefix + "-" + text[len(prefix):]
			}
		}
		for i, r := range text {
			if r >= '0' && r <= '9' {
				if i > 1 {
					return text[:i] + "-" + text[i:]
				}
				return text
			}
		}
	}
	return text
}

func (c *Client) SearchModels(ctx context.Context, query string) ([]Model, error) {
	query = normalizeModel(query)
	if query == "" {
		return nil, fmt.Errorf("enter a printer model, for example WF-C5790")
	}
	address := c.supportURL + "/supportsearch?searchText=" + url.QueryEscape(query)
	support, err := url.Parse(c.supportURL)
	if err != nil {
		return nil, err
	}
	doc, err := c.page(ctx, address)
	if err != nil {
		return nil, err
	}
	var models []Model
	seen := make(map[string]bool)
	anchors(doc, func(a map[string]string) {
		name := strings.TrimSpace(a["title"])
		u, err := resolve(c.supportURL, a["href"])
		if err != nil || name == "" || !strings.HasPrefix(u.Path, "/Support/Printers/") {
			return
		}
		if c.checkURL(u) != nil || u.Host != support.Host {
			return
		}
		if !strings.Contains(strings.ReplaceAll(normalizeModel(name), "-", ""), strings.ReplaceAll(query, "-", "")) {
			return
		}
		u.RawQuery, u.Fragment = "", ""
		if !seen[u.String()] {
			seen[u.String()] = true
			models = append(models, Model{Name: name, URL: u.String()})
		}
	})
	if len(models) == 0 {
		return nil, fmt.Errorf("no matching Epson printer pages found for %q; try the full model such as WF-C5790 or AM-C4000", query)
	}
	return models, nil
}
