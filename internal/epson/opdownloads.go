package epson

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

const (
	PlatformDownloadURL  = "https://openplatform.epson.biz"
	PlatformEULAURL      = "https://support.epson.net/eula/"
	PlatformProfileTerms = "https://openplatform.epson.biz/download/op/en/agreement.html"
	platformPaperCut     = "/download/pc/"
	platformGeneral      = "/download/op/en/"
)

// PlatformResource is one file or web tool from Epson's Open Platform download pages.
// Models is empty for items that apply to every Open Platform printer.
type PlatformResource struct {
	Package
	Version string
	Updated string
	Size    string
	Models  []string
}

// PaperCut reports whether the item belongs to the PaperCut solution rather than
// general Open Platform firmware and documents.
func (r PlatformResource) PaperCut() bool {
	return strings.Contains(r.URL, "/download/files/pc/")
}

type PlatformDownloads struct {
	Resources []PlatformResource
	Models    []string
}

var (
	platformDate    = regexp.MustCompile(`\b(\d{2})/(\d{2})/(\d{4})\b`)
	platformVersion = regexp.MustCompile(`Version:\s*([A-Za-z0-9._-]+)`)
	platformSize    = regexp.MustCompile(`\(?\b(\d+(?:\.\d+)?\s*[KMG]B)\)?`)
)

// PlatformPages returns the Open Platform download page addresses, PaperCut first.
func (c *Client) PlatformPages() []string {
	return []string{c.platformURL + platformPaperCut, c.platformURL + platformGeneral}
}

func (c *Client) PlatformDownloads(ctx context.Context) (PlatformDownloads, error) {
	var result PlatformDownloads
	index := make(map[string]int)
	for _, address := range c.PlatformPages() {
		doc, err := c.page(ctx, address)
		if err != nil {
			return PlatformDownloads{}, fmt.Errorf("%s: %w", address, err)
		}
		for _, r := range c.parsePlatformPage(doc, address) {
			if i, ok := index[r.URL]; ok {
				existing := &result.Resources[i]
				for _, m := range r.Models {
					if !slices.Contains(existing.Models, m) {
						existing.Models = append(existing.Models, m)
					}
				}
				continue
			}
			index[r.URL] = len(result.Resources)
			result.Resources = append(result.Resources, r)
		}
	}
	if len(result.Resources) == 0 {
		return PlatformDownloads{}, fmt.Errorf("no Open Platform downloads found; Epson may have changed its pages")
	}
	seen := make(map[string]bool)
	for _, r := range result.Resources {
		for _, m := range r.Models {
			if !seen[m] {
				seen[m] = true
				result.Models = append(result.Models, m)
			}
		}
	}
	slices.Sort(result.Models)
	slices.SortStableFunc(result.Resources, func(x, y PlatformResource) int {
		return platformCategoryRank(x.Category) - platformCategoryRank(y.Category)
	})
	return result, nil
}

func platformCategoryRank(category string) int {
	switch category {
	case "PaperCut software":
		return 0
	case "Firmware":
		return 1
	case "Manuals":
		return 2
	default:
		return 3
	}
}

func isoDate(text string) string {
	if m := platformDate.FindStringSubmatch(text); m != nil {
		return m[3] + "-" + m[2] + "-" + m[1]
	}
	return ""
}

func sizeOf(text string) string {
	if m := platformSize.FindStringSubmatch(text); m != nil {
		return strings.ReplaceAll(m[1], " ", "")
	}
	return ""
}

// cellLines returns the text of n split at <br> elements.
func cellLines(n *html.Node) []string {
	var lines []string
	var current strings.Builder
	flush := func() {
		if line := strings.Join(strings.Fields(current.String()), " "); line != "" {
			lines = append(lines, line)
		}
		current.Reset()
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			current.WriteString(n.Data + " ")
		case n.Type == html.ElementNode && n.Data == "br":
			flush()
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(n)
	flush()
	return lines
}

func children(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			out = append(out, child)
		}
	}
	return out
}

func tableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	findAll(table, func(n *html.Node) bool { return n.Data == "tr" }, &rows)
	return rows
}

func firstLink(n *html.Node) string {
	if a := find(n, func(n *html.Node) bool { return n.Data == "a" }); a != nil {
		href, _ := attr(a, "href")
		return href
	}
	return ""
}

func (c *Client) platformLink(base, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	link, err := resolve(base, href)
	if err != nil || c.checkURL(link) != nil {
		return "", false
	}
	platform, err := resolve(c.platformURL, "/")
	if err != nil || link.Host != platform.Host {
		return "", false
	}
	return link.String(), true
}

func fileKind(name string) (category, kind string) {
	switch strings.ToLower(path.Ext(name)) {
	case ".exe":
		return "PaperCut software", "Windows"
	case ".dmg":
		return "PaperCut software", "macOS"
	case ".rpm":
		return "PaperCut software", "Linux (rpm)"
	case ".deb":
		return "PaperCut software", "Linux (deb)"
	case ".pdf":
		return "Manuals", "PDF"
	default:
		return "Tools", strings.ToUpper(strings.TrimPrefix(path.Ext(name), "."))
	}
}

func languageSuffix(name string) string {
	if strings.HasSuffix(strings.ToLower(strings.TrimSuffix(name, path.Ext(name))), "_ja") {
		return " (Japanese)"
	}
	return ""
}

func (c *Client) parsePlatformPage(doc *html.Node, base string) []PlatformResource {
	var resources []PlatformResource
	seen := make(map[string]int)
	add := func(r PlatformResource) {
		if i, ok := seen[r.URL]; ok {
			for _, m := range r.Models {
				if !slices.Contains(resources[i].Models, m) {
					resources[i].Models = append(resources[i].Models, m)
				}
			}
			return
		}
		seen[r.URL] = len(resources)
		resources = append(resources, r)
	}
	var tables []*html.Node
	findAll(doc, func(n *html.Node) bool { return n.Data == "table" }, &tables)
	for _, table := range tables {
		var headers []string
		device := false
		var data []*html.Node
		for _, row := range tableRows(table) {
			if th := children(row, "th"); len(th) > 0 {
				headers = headers[:0]
				for _, cell := range th {
					label := strings.Join(cellLines(cell), " ")
					device = device || strings.Contains(strings.ToLower(label), "firmware")
					headers = append(headers, label)
				}
				continue
			}
			data = append(data, row)
		}
		for _, row := range data {
			cells := children(row, "td")
			if len(cells) < 2 {
				continue
			}
			if device {
				c.parseDeviceRow(cells, headers, base, add)
			} else {
				c.parseFileRow(cells, headers, base, add)
			}
		}
	}
	// Links outside tables, such as the release history and the parameter generator.
	anchors(doc, func(a map[string]string) {
		link, ok := c.platformLink(base, a["href"])
		if _, dup := seen[link]; !ok || dup {
			return
		}
		u, _ := resolve(link, "")
		switch {
		case strings.HasPrefix(u.Path, "/download/files/"):
			name := path.Base(u.Path)
			category, kind := fileKind(name)
			title := "Release history"
			if !strings.Contains(strings.ToLower(name), "releasehistory") {
				title = strings.TrimSuffix(name, path.Ext(name))
			}
			add(PlatformResource{
				Package: Package{Category: category, Kind: kind, Title: title, Name: name, URL: link},
				Updated: dateFromName(name),
			})
		case strings.HasPrefix(u.Path, "/download/opg/"):
			add(PlatformResource{Package: Package{
				Category: "Tools", Kind: "Web tool", Title: "Operational parameter generator (card reader area)",
				Name: "(open in browser)", URL: link, OpenOnly: true,
			}})
		}
	})
	return resources
}

var nameDate = regexp.MustCompile(`_(20\d{2})(\d{2})(\d{2})\b`)

func dateFromName(name string) string {
	if m := nameDate.FindStringSubmatch(strings.TrimSuffix(name, path.Ext(name))); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3]
	}
	return ""
}

func (c *Client) parseFileRow(cells []*html.Node, headers []string, base string, add func(PlatformResource)) {
	link, ok := c.platformLink(base, firstLink(cells[len(cells)-1]))
	if !ok {
		return
	}
	u, _ := resolve(link, "")
	name := path.Base(u.Path)
	r := PlatformResource{Package: Package{Title: strings.Join(cellLines(cells[0]), " "), Name: name, URL: link}}
	r.Category, r.Kind = fileKind(name)
	if r.Category == "Tools" && strings.Contains(strings.ToLower(r.Title), "profile") {
		r.Kind = "Security profile"
	}
	for i, cell := range cells[1 : len(cells)-1] {
		value := strings.Join(cellLines(cell), " ")
		header := ""
		if i+1 < len(headers) {
			header = strings.ToLower(headers[i+1])
		}
		switch {
		case strings.Contains(header, "version"):
			r.Version = value
		case strings.Contains(header, "updated") || isoDate(value) != "" && r.Updated == "":
			r.Updated = isoDate(value)
		case strings.Contains(header, "size") || sizeOf(value) != "" && r.Size == "":
			r.Size = sizeOf(value)
		}
	}
	if r.Title == "" {
		r.Title = name
	}
	add(r)
}

func (c *Client) parseDeviceRow(cells []*html.Node, headers []string, base string, add func(PlatformResource)) {
	models := cellLines(cells[0])
	if len(models) == 0 {
		return
	}
	// The last header row names the data columns (Windows, Mac, Hardware Setup Guide, ...).
	columns := headers
	if len(columns) == len(cells) {
		columns = columns[1:]
	}
	for i, cell := range cells[1:] {
		link, ok := c.platformLink(base, firstLink(cell))
		if !ok {
			continue
		}
		u, _ := resolve(link, "")
		name := path.Base(u.Path)
		info := strings.Join(cellLines(cell), " ")
		r := PlatformResource{
			Package: Package{Name: name, URL: link},
			Updated: isoDate(info), Size: sizeOf(info),
			Models: slices.Clone(models),
		}
		if m := platformVersion.FindStringSubmatch(info); m != nil {
			r.Version = m[1]
		}
		column := ""
		if i < len(columns) {
			column = strings.TrimSpace(strings.ReplaceAll(columns[i], "(updated)", ""))
		}
		switch ext := strings.ToLower(path.Ext(name)); ext {
		case ".exe", ".dmg":
			r.Category, r.Kind = "Firmware", "Windows"
			if ext == ".dmg" {
				r.Kind = "macOS"
			}
			r.Title = "Open Platform firmware updater"
			if r.Version != "" {
				r.Title += " " + r.Version
			}
		default:
			r.Category, r.Kind = fileKind(name)
			if column != "" && r.Category == "Manuals" {
				r.Kind = column
			}
			r.Title = r.Kind + languageSuffix(name)
		}
		add(r)
	}
}

// PlatformModelKey reduces a model name to a comparable key: "WF-6590 Series" and
// "Epson WorkForce Pro WF-6590" both become "wf6590".
func PlatformModelKey(name string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, " series") {
		name = name[:len(name)-len(" series")]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(normalizeModel(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ForModel returns the device-specific resources for a printer (no general items).
func (d PlatformDownloads) ForModel(model string) []PlatformResource {
	key := PlatformModelKey(model)
	if key == "" {
		return nil
	}
	var out []PlatformResource
	for _, r := range d.Resources {
		for _, m := range r.Models {
			if PlatformModelKey(m) == key {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// Filter returns resources matching a model or keyword. When the term matches a
// printer model, the general items (PaperCut software, common documents) are kept too.
func (d PlatformDownloads) Filter(term, category string) []PlatformResource {
	term = strings.TrimSpace(term)
	needle := strings.ToLower(term)
	key := PlatformModelKey(term)
	matchesModel := func(models []string) bool {
		for _, m := range models {
			mk := PlatformModelKey(m)
			if key != "" && strings.Contains(mk, key) || strings.Contains(strings.ToLower(m), needle) {
				return true
			}
		}
		return false
	}
	modelHit := false
	if term != "" {
		for _, r := range d.Resources {
			if matchesModel(r.Models) {
				modelHit = true
				break
			}
		}
	}
	var out []PlatformResource
	for _, r := range d.Resources {
		if category != "" && category != "All" && r.Category != category {
			continue
		}
		keep := term == "" || matchesModel(r.Models) || modelHit && len(r.Models) == 0 ||
			strings.Contains(strings.ToLower(r.Title+" "+r.Name+" "+r.Kind), needle)
		if keep {
			out = append(out, r)
		}
	}
	return out
}
