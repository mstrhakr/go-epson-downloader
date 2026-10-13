package epson

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

type ProductDownloads struct {
	OperatingSystems []string
	SelectedOS       string
	Packages         []Package
}

func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode && attribute(n, "id") == id {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := nodeByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func operatingSystems(doc *html.Node) ([]string, error) {
	selectNode := nodeByID(doc, "review-filter")
	if selectNode == nil || selectNode.Data != "select" {
		return nil, fmt.Errorf("Epson's operating-system selector was not found")
	}
	var systems []string
	for n := selectNode.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.ElementNode && n.Data == "option" {
			value := strings.TrimSpace(attribute(n, "value"))
			if value != "" && !slices.Contains(systems, value) {
				systems = append(systems, value)
			}
		}
	}
	if len(systems) == 0 {
		return nil, fmt.Errorf("Epson's operating-system selector is empty")
	}
	return systems, nil
}

func (c *Client) productPage(ctx context.Context, address, requestedOS string) (*html.Node, string, []string, error) {
	selectedOS := requestedOS
	if selectedOS == "" {
		selectedOS = "Windows 11"
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, "", nil, err
	}
	q := u.Query()
	q.Set("review-filter", selectedOS)
	u.RawQuery = q.Encode()
	doc, err := c.page(ctx, u.String())
	if err != nil {
		return nil, "", nil, err
	}
	systems, err := operatingSystems(doc)
	if err != nil {
		return nil, "", nil, err
	}
	if !slices.Contains(systems, selectedOS) {
		if requestedOS != "" {
			return nil, "", nil, fmt.Errorf("%q is not offered for this Epson product", requestedOS)
		}
		// Only the initial, unspecified OS can fall back, and the selected OS
		// is returned to the UI so it is visible rather than silently substituted.
		selectedOS = systems[0]
		q.Set("review-filter", selectedOS)
		u.RawQuery = q.Encode()
		doc, err = c.page(ctx, u.String())
		if err != nil {
			return nil, "", nil, err
		}
	}
	return doc, selectedOS, systems, nil
}

func cleanTitle(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func packageKind(name, metadata string) (category, kind string) {
	upper := strings.ToUpper(name)
	switch {
	case strings.Contains(strings.ToLower(metadata), "firmware") || strings.HasPrefix(upper, "FW_"):
		return "Firmware", "Firmware"
	case strings.Contains(upper, "STD_CORE"):
		return "Drivers", "Standard Core"
	case strings.Contains(upper, "STD") && !strings.Contains(upper, "CORE"):
		return "Drivers", "Standard"
	case strings.Contains(upper, "LITE") && !strings.Contains(upper, "CORE"):
		return "Drivers", "Lite"
	case strings.Contains(metadata, "drivers") || strings.Contains(metadata, "recommended"):
		return "Drivers", "Driver"
	case strings.Contains(metadata, "utilities") || strings.Contains(metadata, "admin"):
		return "Utilities", "Utility"
	default:
		return "", ""
	}
}

func (c *Client) Downloads(ctx context.Context, model Model, requestedOS string) (ProductDownloads, error) {
	doc, selectedOS, systems, err := c.productPage(ctx, model.URL, requestedOS)
	if err != nil {
		return ProductDownloads{}, err
	}
	result := ProductDownloads{OperatingSystems: systems, SelectedOS: selectedOS}
	seen := make(map[string]bool)
	anchors(doc, func(a map[string]string) {
		name := strings.TrimSpace(a["data-dl_name"])
		link, err := resolve(model.URL, a["href"])
		if err != nil || c.checkURL(link) != nil || seen[link.String()] {
			return
		}
		if name == "" {
			if link.Host == "download.ebz.epson.net" {
				var category, title string
				switch {
				case strings.HasPrefix(link.Path, "/dsc/search/"):
					category, title = "Drivers", "Linux driver download portal"
				case strings.HasPrefix(link.Path, "/man/linux/"):
					category, title = "Manuals", "Linux online manuals"
				}
				if category != "" {
					seen[link.String()] = true
					result.Packages = append(result.Packages, Package{
						Category: category, Kind: "Online resource", Title: title,
						Name: "(open in browser)", URL: link.String(), OpenOnly: true,
					})
				}
			}
			return
		}
		category, kind := packageKind(name, strings.ToLower(a["data-dl_type"]))
		if category == "" {
			return
		}
		title := cleanTitle(a["data-dl_title"])
		if title == "" {
			title = kind
		}
		seen[link.String()] = true
		result.Packages = append(result.Packages, Package{
			Category: category, Kind: kind, Title: title, Name: name, URL: link.String(),
		})
	})
	if manuals := nodeByID(doc, "manuals"); manuals != nil {
		anchors(manuals, func(a map[string]string) {
			link, err := resolve(model.URL, a["href"])
			if err != nil || c.checkURL(link) != nil || seen[link.String()] {
				return
			}
			title := cleanTitle(a["title"])
			isPDF := strings.HasSuffix(strings.ToLower(link.Path), ".pdf")
			isHTML := strings.Contains(title, "_html")
			if !isPDF && !isHTML {
				return
			}
			title = strings.ReplaceAll(strings.ReplaceAll(title, "_pdf", ""), "_html", "")
			if title == "" {
				title = path.Base(link.Path)
			}
			name := path.Base(link.Path)
			kind := "PDF"
			if isHTML {
				kind, name = "Online guide", "(open in browser)"
			}
			seen[link.String()] = true
			result.Packages = append(result.Packages, Package{
				Category: "Manuals", Kind: kind, Title: title, Name: name,
				URL: link.String(), OpenOnly: isHTML,
			})
		})
	}
	if len(result.Packages) == 0 {
		return ProductDownloads{}, fmt.Errorf("no downloadable packages or public manuals found for %s [%s]; Epson may have changed its page", model.Name, selectedOS)
	}
	return result, nil
}
