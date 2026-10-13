package epson

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const DefaultAdminVersion = 570

var adminFilename = regexp.MustCompile(`^EDA_(\d+)\.exe$`)
var adminTitle = regexp.MustCompile(`(?i)^Epson Device Admin\s+v(\d+)\.(\d+)\.(\d+)\s*$`)

type AdminRelease struct {
	Package          Package
	Version          int
	DisplayVersion   string
	OperatingSystems []string
	SelectedOS       string
}

func (c *Client) PublishedAdmin(ctx context.Context, requestedOS string) (AdminRelease, error) {
	doc, selectedOS, systems, err := c.productPage(ctx, c.supportURL+AdminPath, requestedOS)
	if err != nil {
		return AdminRelease{}, err
	}
	return c.parseAdminRelease(doc, selectedOS, systems)
}

func (c *Client) parseAdminRelease(doc *html.Node, selectedOS string, systems []string) (AdminRelease, error) {
	var matches []string
	anchors(doc, func(a map[string]string) {
		if match := adminTitle.FindStringSubmatch(cleanTitle(a["title"])); match != nil {
			matches = match
		}
	})
	if matches == nil {
		return AdminRelease{}, fmt.Errorf("no published Device Admin version found for %s; try another OS, or use the explicit advanced version scan", selectedOS)
	}
	version, err := strconv.Atoi(matches[1] + matches[2] + matches[3])
	if err != nil || version < 1 || version > 999999 {
		return AdminRelease{}, fmt.Errorf("cannot map the published Device Admin version to a download filename")
	}
	display := strings.Join(matches[1:4], ".")
	name := fmt.Sprintf("EDA_%d.exe", version)
	return AdminRelease{
		Package: Package{Category: "Device Admin", Kind: "Device Admin", Title: "Epson Device Admin v" + display, Name: name, URL: c.downloadURL + name},
		Version: version, DisplayVersion: display, OperatingSystems: systems, SelectedOS: selectedOS,
	}, nil
}

func (c *Client) VerifyPackage(ctx context.Context, pkg Package) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := c.request(ctx, http.MethodHead, pkg.URL)
	if err != nil {
		return fmt.Errorf("verify %s: %w", pkg.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength == 0 {
		return fmt.Errorf("verify %s: HTTP %d, size %d; the direct file may not match the published version", pkg.Name, resp.StatusCode, resp.ContentLength)
	}
	return nil
}

func AdminStartVersion(folder string, seed int) (int, error) {
	if seed < 1 || seed > 999999 {
		return 0, fmt.Errorf("starting version must be between 1 and 999999")
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return 0, fmt.Errorf("read destination: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(folder, "latest_version.txt"))
	if err == nil {
		first, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
		version, parseErr := strconv.Atoi(strings.TrimSpace(first))
		if parseErr != nil || version < 1 || version > 999999 {
			return 0, fmt.Errorf("latest_version.txt contains an invalid version; remove or correct it")
		}
		seed = max(seed, version)
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("read remembered Device Admin version: %w", err)
	}
	for _, entry := range entries {
		match := adminFilename.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		version, err := strconv.Atoi(match[1])
		if err == nil && version <= 999999 && info.Mode().IsRegular() && info.Size() > 0 {
			seed = max(seed, version)
		}
	}
	return seed, nil
}

func (c *Client) AdminPackage(ctx context.Context, seed, checks int, log func(string)) (Package, int, error) {
	if seed < 1 || checks < 1 || checks > 100 || seed+checks*10 > 999999 {
		return Package{}, 0, fmt.Errorf("invalid Device Admin search range (1-100 checks, versions up to 999999)")
	}
	// The script probes in steps of ten. Include the seed itself and verify it
	// rather than assuming a remembered version is still available.
	for version := seed + checks*10; version >= seed; version -= 10 {
		name := fmt.Sprintf("EDA_%d.exe", version)
		address := c.downloadURL + name
		if log != nil {
			log("Checking " + address)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		resp, err := c.request(probeCtx, http.MethodHead, address)
		if err != nil {
			cancel()
			return Package{}, 0, fmt.Errorf("check %s: %w", name, err)
		}
		resp.Body.Close()
		cancel()
		switch resp.StatusCode {
		case http.StatusOK:
			if resp.ContentLength == 0 {
				return Package{}, 0, fmt.Errorf("%s is empty on the server", name)
			}
			return Package{Category: "Device Admin", Kind: "Device Admin", Name: name, URL: address}, version, nil
		case http.StatusNotFound, http.StatusGone:
			continue
		default:
			return Package{}, 0, fmt.Errorf("check %s: HTTP %d", name, resp.StatusCode)
		}
	}
	return Package{}, 0, fmt.Errorf("no Device Admin package found between EDA_%d and EDA_%d (step 10); adjust the starting version or check count", seed, seed+checks*10)
}

func RememberAdminVersion(folder string, version int) error {
	if version < 1 || version > 999999 {
		return fmt.Errorf("invalid Device Admin version")
	}
	if err := os.WriteFile(filepath.Join(folder, "latest_version.txt"), []byte(fmt.Sprintf("%d\n", version)), 0o644); err != nil {
		return fmt.Errorf("save Device Admin version: %w", err)
	}
	return nil
}
