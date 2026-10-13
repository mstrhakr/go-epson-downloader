//go:build windows

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"epson-downloader/internal/epson"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const (
	keysSubTab     = 0
	platformFolder = "Open Platform"
)

type platformTable struct {
	walk.TableModelBase
	items   []epson.PlatformResource
	checked map[string]bool
	changed func()
	invalid func(error)
}

func (m *platformTable) RowCount() int { return len(m.items) }
func (m *platformTable) Value(row, col int) interface{} {
	r := m.items[row]
	switch col {
	case 0:
		return r.Category
	case 1:
		return r.Title
	case 2:
		return r.Kind
	case 3:
		return r.Version
	case 4:
		return r.Updated
	case 5:
		return r.Size
	case 6:
		if len(r.Models) == 0 {
			return "All"
		}
		return strings.Join(r.Models, ", ")
	default:
		return r.Name
	}
}
func (m *platformTable) Checked(row int) bool { return m.checked[m.items[row].URL] }
func (m *platformTable) SetChecked(row int, checked bool) error {
	if checked && m.items[row].OpenOnly {
		m.invalid(fmt.Errorf("this is a web tool; use Open selected link instead of downloading it"))
		return nil
	}
	m.checked[m.items[row].URL] = checked
	m.changed()
	return nil
}

// platformView is one Open Platform download sub-tab (PaperCut, or firmware & documents).
type platformView struct {
	paperCut   bool
	title      string
	intro      string
	note       string
	categories []string
	root       *walk.Composite
	agree      *walk.CheckBox
	filter     *walk.LineEdit
	category   *walk.ComboBox
	view       *walk.TableView
	table      platformTable
	summary    *walk.Label
	download   *walk.PushButton
	openLink   *walk.PushButton
	copyLink   *walk.PushButton
}

func newPlatformViews() (paperCut, files platformView) {
	paperCut = platformView{
		paperCut: true, title: "PaperCut",
		intro:      "Epson Open Platform Solution for PaperCut: the software PaperCut needs to drive Epson Open Platform printers, plus its guides.",
		note:       "Files go into Open Platform\\<category> in the save folder. Nothing is installed.",
		categories: []string{"All", "PaperCut software", "Manuals"},
	}
	files = platformView{
		title:      "Firmware & documents",
		intro:      "Open Platform firmware updaters, hardware setup and administrator guides, card reader list, and security profile. Filter by model.",
		note:       "Only older models (WF-6590, WF-5690, WF-8590 families...) need these firmware updaters; newer models include Open Platform in standard firmware. Nothing is flashed.",
		categories: []string{"All", "Firmware", "Manuals", "Tools"},
	}
	for _, v := range []*platformView{&paperCut, &files} {
		v.table.checked = make(map[string]bool)
	}
	return paperCut, files
}

func (a *app) platformViews() []*platformView { return []*platformView{&a.paperCut, &a.opFiles} }

func (a *app) platformPage(v *platformView) TabPage {
	v.table.changed = func() { a.refreshPlatformActions(v) }
	v.table.invalid = a.reportError
	filterRow := []Widget{}
	if !v.paperCut {
		filterRow = append(filterRow,
			Label{Text: "Model or keyword:"},
			LineEdit{AssignTo: &v.filter, CueBanner: "WF-6590 or AM-C4000", MinSize: Size{Width: 200}, OnTextChanged: func() { a.applyPlatform(v) }},
		)
	}
	filterRow = append(filterRow,
		Label{Text: "Show:"},
		ComboBox{AssignTo: &v.category, Model: v.categories, CurrentIndex: 0, OnCurrentIndexChanged: func() { a.applyPlatform(v) }},
		PushButton{Text: "&Reload", OnClicked: a.loadPlatformDownloads},
		HSpacer{},
		Label{AssignTo: &v.summary, Text: "Not loaded"},
	)
	return TabPage{
		Title:  v.title,
		Layout: VBox{},
		Children: []Widget{
			Composite{
				AssignTo: &v.root, Layout: VBox{MarginsZero: true}, StretchFactor: 1,
				Children: []Widget{
					Label{Text: v.intro},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							CheckBox{
								AssignTo: &v.agree, Text: "I agree to Epson's license terms",
								OnCheckedChanged: func() { a.setPlatformAgreement(v.agree.Checked()) },
							},
							LinkLabel{
								Text:            fmt.Sprintf(`(<a href="%s">End User Software License Agreement</a>, <a href="%s">security profile use conditions</a>)`, epson.PlatformEULAURL, epson.PlatformProfileTerms),
								OnLinkActivated: func(link *walk.LinkLabelLink) { a.openURL(link.URL()) },
							},
							HSpacer{},
						},
					},
					Composite{Layout: HBox{MarginsZero: true}, Children: filterRow},
					TableView{
						AssignTo: &v.view, Model: &v.table, CheckBoxes: true, StretchFactor: 1, MinSize: Size{Height: 120},
						Columns: []TableViewColumn{
							{Title: "Category", Width: 105}, {Title: "Item", Width: 260}, {Title: "Type", Width: 120},
							{Title: "Version", Width: 75}, {Title: "Updated", Width: 75}, {Title: "Size", Width: 55},
							{Title: "Models", Width: 220}, {Title: "Filename", Width: 260},
						},
						OnCurrentIndexChanged: func() { a.refreshPlatformActions(v) },
						OnItemActivated:       func() { a.openPlatformLink(v) },
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{AssignTo: &v.download, Text: "&Download checked (0)", Enabled: false, OnClicked: func() { a.downloadPlatform(v) }},
							PushButton{AssignTo: &v.openLink, Text: "Open selected &link", Enabled: false, OnClicked: func() { a.openPlatformLink(v) }},
							PushButton{AssignTo: &v.copyLink, Text: "Cop&y link", Enabled: false, OnClicked: func() { a.copyPlatformLink(v) }},
							HSpacer{},
						},
					},
					Label{Text: v.note},
				},
			},
		},
	}
}

func (a *app) openURL(link string) {
	if err := a.open(link); err != nil {
		a.reportError(err)
	}
}

func (a *app) setPlatformAgreement(agreed bool) {
	a.platformAgreed = agreed
	for _, v := range a.platformViews() {
		if v.agree != nil && v.agree.Checked() != agreed {
			v.agree.SetChecked(agreed)
		}
		a.refreshPlatformActions(v)
	}
}

func (v *platformView) scoped(all []epson.PlatformResource) []epson.PlatformResource {
	var out []epson.PlatformResource
	for _, r := range all {
		if r.PaperCut() == v.paperCut {
			out = append(out, r)
		}
	}
	return out
}

func (a *app) applyPlatform(v *platformView) {
	if v.category == nil || v.view == nil {
		return
	}
	term := ""
	if v.filter != nil {
		term = v.filter.Text()
	}
	scoped := v.scoped(a.platform.Resources)
	v.table.items = epson.PlatformDownloads{Resources: scoped}.Filter(term, v.category.Text())
	v.table.PublishRowsReset()
	if v.summary != nil && a.platformLoaded {
		v.summary.SetText(fmt.Sprintf("Showing %d of %d", len(v.table.items), len(scoped)))
	}
	a.refreshPlatformActions(v)
}

func (a *app) checkedPlatform(v *platformView) []epson.PlatformResource {
	var out []epson.PlatformResource
	for _, r := range v.scoped(a.platform.Resources) {
		if v.table.checked[r.URL] {
			out = append(out, r)
		}
	}
	return out
}

func (a *app) refreshPlatformActions(v *platformView) {
	if v.download == nil || v.openLink == nil || v.copyLink == nil || v.view == nil {
		return
	}
	count := len(a.checkedPlatform(v))
	v.download.SetText(fmt.Sprintf("&Download checked (%d)", count))
	v.download.SetEnabled(!a.busy && a.platformAgreed && count > 0)
	selected := !a.busy && v.view.CurrentIndex() >= 0
	v.copyLink.SetEnabled(selected)
	r, ok := a.selectedPlatform(v)
	v.openLink.SetEnabled(selected && ok && (r.OpenOnly || a.platformAgreed))
}

func (a *app) selectedPlatform(v *platformView) (epson.PlatformResource, bool) {
	index := v.view.CurrentIndex()
	if index < 0 || index >= len(v.table.items) {
		return epson.PlatformResource{}, false
	}
	return v.table.items[index], true
}

func (a *app) openPlatformLink(v *platformView) {
	r, ok := a.selectedPlatform(v)
	if !ok {
		return
	}
	if !r.OpenOnly && !a.platformAgreed {
		a.reportError(fmt.Errorf("tick \"I agree to Epson's license terms\" before opening Epson download links"))
		return
	}
	a.openURL(r.URL)
}

func (a *app) copyPlatformLink(v *platformView) {
	if r, ok := a.selectedPlatform(v); ok {
		if err := walk.Clipboard().SetText(r.URL); err != nil {
			a.reportError(err)
		} else {
			a.status.SetText("Link copied.")
		}
	}
}

func (a *app) showPlatformDownloads(d epson.PlatformDownloads) {
	a.platform, a.platformLoaded = d, true
	for _, v := range a.platformViews() {
		if v.paperCut && len(a.checkedPlatform(v)) == 0 {
			for _, r := range v.scoped(d.Resources) {
				if r.Category == "PaperCut software" && r.Kind == "Windows" {
					v.table.checked[r.URL] = true
				}
			}
		}
		a.applyPlatform(v)
	}
}

func paperCutSummary(d epson.PlatformDownloads) string {
	var parts []string
	for _, r := range d.Resources {
		if r.Category == "PaperCut software" {
			parts = append(parts, fmt.Sprintf("%s %s (%s)", r.Kind, r.Version, r.Updated))
		}
	}
	return strings.Join(parts, ", ")
}

func (a *app) loadPlatformDownloads() {
	a.run("Loading Open Platform and PaperCut downloads...", func(ctx context.Context) (func(), error) {
		d, err := a.client.PlatformDownloads(ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			a.showPlatformDownloads(d)
			a.status.SetText(fmt.Sprintf("Loaded %d Open Platform resources for %d models.", len(d.Resources), len(d.Models)))
			a.appendLog(a.status.Text())
			if s := paperCutSummary(d); s != "" {
				a.appendLog("Epson Open Platform Solution for PaperCut: " + s)
			}
		}, nil
	})
}

func (a *app) downloadPlatform(v *platformView) {
	if !a.platformAgreed {
		a.reportError(fmt.Errorf("tick \"I agree to Epson's license terms\" before downloading"))
		return
	}
	folder, err := a.folder()
	if err != nil {
		a.reportError(err)
		return
	}
	items := a.checkedPlatform(v)
	if len(items) == 0 {
		a.reportError(fmt.Errorf("check at least one file"))
		return
	}
	a.run("Downloading Open Platform files...", func(ctx context.Context) (func(), error) {
		for i, r := range items {
			a.log(fmt.Sprintf("File %d/%d: %s", i+1, len(items), r.Name))
			path, err := a.client.Download(ctx, r.Package, filepath.Join(folder, platformFolder, r.Category), a.showProgress)
			if err != nil {
				return nil, err
			}
			a.log("Ready (downloaded or existing size matched): " + path)
		}
		return func() {
			a.progress.SetValue(100)
			a.status.SetText(fmt.Sprintf("%d file(s) ready in %s.", len(items), filepath.Join(folder, platformFolder)))
			a.appendLog(a.status.Text())
		}, nil
	})
}

// platformPackages converts device-specific Open Platform resources for the Model downloads tab.
func platformPackages(d epson.PlatformDownloads, model string) []epson.Package {
	var out []epson.Package
	for _, r := range d.ForModel(model) {
		pkg := r.Package
		pkg.Category = platformFolder
		pkg.Title = fmt.Sprintf("Open Platform %s (%s)", r.Title, r.Updated)
		if r.Category == "Firmware" {
			pkg.Title = fmt.Sprintf("Open Platform firmware %s (%s, %s)", r.Version, r.Kind, r.Updated)
		}
		out = append(out, pkg)
	}
	return out
}
