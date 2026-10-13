//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"epson-downloader/internal/epson"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const (
	keyFolder  = "Open Platform Keys"
	keyLogName = "OpenPlatformKeys.csv"
)

type keyTable struct {
	walk.TableModelBase
	items []epson.ProductKey
}

func (m *keyTable) RowCount() int { return len(m.items) }
func (m *keyTable) Value(row, col int) interface{} {
	if col == 0 {
		return m.items[row].Serial
	}
	return m.items[row].Key
}

type openPlatformUI struct {
	client      *epson.OpenPlatformClient
	options     epson.OpenPlatformOptions
	loaded      bool
	subTabs     *walk.TabWidget
	root        *walk.Composite
	serials     *walk.TextEdit
	serialCount *walk.Label
	area        *walk.ComboBox
	country     *walk.ComboBox
	application *walk.ComboBox
	otherApp    *walk.LineEdit
	saveLog     *walk.CheckBox
	issueButton *walk.PushButton
	copyKey     *walk.PushButton
	copyAll     *walk.PushButton
	keysView    *walk.TableView
	keys        keyTable
	lastCountry string
}

func optionLabels(options []epson.Option) []string {
	labels := make([]string, len(options))
	for i, o := range options {
		labels[i] = o.Label
	}
	return labels
}

func optionIndex(options []epson.Option, value string) int {
	for i, o := range options {
		if o.Value == value {
			return i
		}
	}
	return -1
}

func (a *app) openPlatformPage() TabPage {
	op := &a.op
	return TabPage{
		Title:  "Open Platform",
		Layout: VBox{},
		Children: []Widget{
			TabWidget{
				AssignTo: &op.subTabs, OnCurrentIndexChanged: a.tabChanged,
				Pages: []TabPage{a.keysPage(), a.platformPage(&a.paperCut), a.platformPage(&a.opFiles)},
			},
		},
	}
}

func (a *app) keysPage() TabPage {
	op := &a.op
	return TabPage{
		Title:  "Product keys",
		Layout: VBox{},
		Children: []Widget{
			Composite{
				AssignTo: &op.root, Layout: VBox{MarginsZero: true}, StretchFactor: 1,
				Children: []Widget{
					Label{Text: "Issue Epson Open Platform product keys from printer serial numbers. Up to 150 per request; larger lists are split automatically."},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "Serial numbers (one per line, or separated by commas/spaces):"},
							HSpacer{},
							Label{AssignTo: &op.serialCount, Text: "0 serials"},
							PushButton{Text: "&Import .txt/.csv...", OnClicked: a.importSerials},
							PushButton{Text: "Clear", OnClicked: func() { op.serials.SetText("") }},
						},
					},
					TextEdit{
						AssignTo: &op.serials, VScroll: true, MinSize: Size{Height: 80}, MaxSize: Size{Height: 110},
						OnTextChanged: a.updateSerialCount,
					},
					Composite{
						Layout: Grid{Columns: 5, MarginsZero: true},
						Children: []Widget{
							Label{Text: "Area:"},
							ComboBox{AssignTo: &op.area, MinSize: Size{Width: 190}, OnCurrentIndexChanged: a.areaChanged},
							Label{Text: "Country/Region:"},
							ComboBox{AssignTo: &op.country, MinSize: Size{Width: 190}},
							HSpacer{},
							Label{Text: "Main application:"},
							ComboBox{AssignTo: &op.application, MinSize: Size{Width: 190}, OnCurrentIndexChanged: a.applicationChanged},
							Label{Text: "Application name:"},
							LineEdit{AssignTo: &op.otherApp, Enabled: false, MaxLength: 100, CueBanner: "Required for Other"},
							PushButton{Text: "&Reload lists", OnClicked: a.loadOpenPlatformOptions},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{AssignTo: &op.issueButton, Text: "&Issue product keys", OnClicked: a.issueKeys},
							CheckBox{AssignTo: &op.saveLog, Text: "Append results to " + filepath.Join(keyFolder, keyLogName) + " in the save folder", Checked: true},
							HSpacer{},
						},
					},
					Label{Text: "Epson does not check that a serial belongs to a real printer: a typo still returns a valid-looking key. Double-check serials."},
					TableView{
						AssignTo: &op.keysView, Model: &op.keys, StretchFactor: 1, MinSize: Size{Height: 90},
						Columns:               []TableViewColumn{{Title: "Serial number", Width: 180}, {Title: "Product key", Width: 260}},
						OnCurrentIndexChanged: a.refreshKeyActions,
						OnItemActivated:       a.copySelectedKey,
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{AssignTo: &op.copyKey, Text: "Copy selected &key", Enabled: false, OnClicked: a.copySelectedKey},
							PushButton{AssignTo: &op.copyAll, Text: "Copy &all (serial, key)", Enabled: false, OnClicked: a.copyAllKeys},
							HSpacer{},
						},
					},
					Label{Text: "Register each key in the printer's Web Config (Open Platform / product key settings)."},
				},
			},
		},
	}
}

func (a *app) updateSerialCount() {
	op := &a.op
	if op.serialCount == nil {
		return
	}
	serials, err := epson.ParseSerials(op.serials.Text())
	switch {
	case strings.TrimSpace(op.serials.Text()) == "":
		op.serialCount.SetText("0 serials")
	case err != nil:
		op.serialCount.SetText("Invalid entry")
	case len(serials) == 1:
		op.serialCount.SetText("1 serial")
	default:
		op.serialCount.SetText(fmt.Sprintf("%d serials", len(serials)))
	}
}

func (a *app) importSerials() {
	dialog := walk.FileDialog{Title: "Import serial numbers", Filter: "Serial lists (*.txt;*.csv)|*.txt;*.csv|All files (*.*)|*.*"}
	ok, err := dialog.ShowOpen(a.window)
	if err != nil || !ok {
		if err != nil {
			a.reportError(err)
		}
		return
	}
	info, err := os.Stat(dialog.FilePath)
	if err == nil && info.Size() > 1<<20 {
		err = fmt.Errorf("serial list is larger than 1 MB")
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(dialog.FilePath)
	}
	if err != nil {
		a.reportError(fmt.Errorf("import serials: %w", err))
		return
	}
	existing := strings.TrimSpace(a.op.serials.Text())
	imported := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n", "\r\n")
	if existing != "" {
		imported = existing + "\r\n" + imported
	}
	a.op.serials.SetText(strings.TrimSpace(imported))
	a.appendLog("Imported serials from " + dialog.FilePath)
}

func (a *app) areaChanged() {
	op := &a.op
	index := op.area.CurrentIndex()
	if op.country == nil || index < 0 || index >= len(op.options.Areas) {
		return
	}
	countries := op.options.Areas[index].Countries
	op.country.SetModel(optionLabels(countries))
	if i := optionIndex(countries, op.lastCountry); i >= 0 {
		op.country.SetCurrentIndex(i)
	} else if len(countries) == 1 {
		op.country.SetCurrentIndex(0)
	}
}

func (a *app) applicationChanged() {
	op := &a.op
	index := op.application.CurrentIndex()
	other := index >= 0 && index < len(op.options.Applications) && op.options.Applications[index].Value == "Other"
	if op.otherApp != nil {
		op.otherApp.SetEnabled(other)
	}
}

func (a *app) refreshKeyActions() {
	op := &a.op
	if op.copyKey == nil || op.issueButton == nil {
		return
	}
	op.issueButton.SetEnabled(!a.busy)
	op.copyKey.SetEnabled(!a.busy && op.keysView.CurrentIndex() >= 0)
	op.copyAll.SetEnabled(!a.busy && len(op.keys.items) > 0)
}

func (a *app) showOpenPlatformOptions(options epson.OpenPlatformOptions) {
	op := &a.op
	op.options, op.loaded = options, true
	areas := make([]epson.Option, len(options.Areas))
	for i, area := range options.Areas {
		areas[i] = area.Option
	}
	op.area.SetModel(optionLabels(areas))
	op.application.SetModel(optionLabels(options.Applications))
	op.lastCountry = "US"
	op.area.SetCurrentIndex(optionIndex(areas, "NA"))
	a.areaChanged()
	op.application.SetCurrentIndex(optionIndex(options.Applications, "PaperCutMF"))
	a.applicationChanged()
}

func (a *app) loadOpenPlatformOptions() {
	a.run("Loading Open Platform choices from Epson...", func(ctx context.Context) (func(), error) {
		options, err := a.op.client.Options(ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			a.showOpenPlatformOptions(options)
			a.status.SetText("Ready to issue Open Platform product keys.")
			a.appendLog(fmt.Sprintf("Loaded %d areas and %d applications from %s", len(options.Areas), len(options.Applications), epson.OpenPlatformURL))
		}, nil
	})
}

func (a *app) tabChanged() {
	if a.tabs == nil || a.tabs.CurrentIndex() != openPlatformTab || a.busy || a.op.subTabs == nil {
		return
	}
	switch a.op.subTabs.CurrentIndex() {
	case keysSubTab:
		if !a.op.loaded {
			a.loadOpenPlatformOptions()
		}
	default:
		if !a.platformLoaded {
			a.loadPlatformDownloads()
		}
	}
}

func (a *app) keyRequest() (epson.KeyRequest, string, string, error) {
	op := &a.op
	serials, err := epson.ParseSerials(op.serials.Text())
	if err != nil {
		return epson.KeyRequest{}, "", "", err
	}
	if !op.loaded {
		return epson.KeyRequest{}, "", "", fmt.Errorf("Epson's area and application lists are not loaded; click Reload lists")
	}
	ai, ci, pi := op.area.CurrentIndex(), op.country.CurrentIndex(), op.application.CurrentIndex()
	if ai < 0 || ai >= len(op.options.Areas) || ci < 0 || ci >= len(op.options.Areas[ai].Countries) {
		return epson.KeyRequest{}, "", "", fmt.Errorf("choose the area and country/region where the printer is installed")
	}
	if pi < 0 || pi >= len(op.options.Applications) {
		return epson.KeyRequest{}, "", "", fmt.Errorf("choose the main application that will use Open Platform")
	}
	area, country, application := op.options.Areas[ai], op.options.Areas[ai].Countries[ci], op.options.Applications[pi]
	appLabel := application.Label
	if application.Value == "Other" {
		if strings.TrimSpace(op.otherApp.Text()) == "" {
			return epson.KeyRequest{}, "", "", fmt.Errorf("enter the application name when the main application is Other")
		}
		appLabel = "Other: " + strings.TrimSpace(op.otherApp.Text())
	}
	req := epson.KeyRequest{
		Serials: serials, Area: area.Value, Country: country.Value,
		Application: application.Value, OtherApplication: op.otherApp.Text(),
	}
	return req, country.Label, appLabel, nil
}

func (a *app) issueKeys() {
	req, countryLabel, appLabel, err := a.keyRequest()
	if err != nil {
		a.reportError(err)
		return
	}
	logPath := ""
	if a.op.saveLog.Checked() {
		folder, err := a.folder()
		if err != nil {
			a.reportError(err)
			return
		}
		logPath = filepath.Join(folder, keyFolder, keyLogName)
	}
	for _, serial := range req.Serials {
		if len(serial) != 10 {
			a.appendLog(fmt.Sprintf("Note: %s is %d characters; Epson printer serials are usually 10.", serial, len(serial)))
		}
	}
	a.op.lastCountry = req.Country
	a.op.keys.items = nil
	a.op.keys.PublishRowsReset()
	a.run(fmt.Sprintf("Issuing %d Open Platform product key(s)...", len(req.Serials)), func(ctx context.Context) (func(), error) {
		keys, err := a.op.client.IssueKeys(ctx, req, func(done, total int) {
			a.dispatch(func() {
				a.progress.SetMarqueeMode(false)
				a.progress.SetValue(done * 100 / total)
				a.status.SetText(fmt.Sprintf("Issued %d of %d product keys...", done, total))
			})
		})
		var saveErr error
		if len(keys) > 0 && logPath != "" {
			saveErr = epson.AppendKeyLog(logPath, keys, countryLabel, appLabel, time.Now())
		}
		show := func() {
			a.op.keys.items = keys
			a.op.keys.PublishRowsReset()
			for _, key := range keys {
				a.appendLog(key.Serial + "  " + key.Key)
			}
			if len(keys) > 0 {
				a.op.keysView.SetCurrentIndex(0)
			}
			a.refreshKeyActions()
		}
		if err != nil {
			// Keep keys from completed batches visible even when a later batch fails.
			a.dispatch(show)
			if saveErr != nil {
				err = fmt.Errorf("%w; also failed to save completed keys: %v", err, saveErr)
			}
			return nil, err
		}
		if saveErr != nil {
			return nil, saveErr
		}
		return func() {
			show()
			a.progress.SetValue(100)
			message := fmt.Sprintf("Issued %d product key(s).", len(keys))
			if logPath != "" {
				message += " Saved to " + logPath
			}
			a.status.SetText(message)
			a.appendLog(message)
		}, nil
	})
}

func (a *app) copySelectedKey() {
	index := a.op.keysView.CurrentIndex()
	if index < 0 || index >= len(a.op.keys.items) {
		return
	}
	if err := walk.Clipboard().SetText(a.op.keys.items[index].Key); err != nil {
		a.reportError(err)
		return
	}
	a.status.SetText("Product key for " + a.op.keys.items[index].Serial + " copied.")
}

func (a *app) copyAllKeys() {
	var b strings.Builder
	for _, key := range a.op.keys.items {
		b.WriteString(key.Serial + "\t" + key.Key + "\r\n")
	}
	if err := walk.Clipboard().SetText(b.String()); err != nil {
		a.reportError(err)
		return
	}
	a.status.SetText(fmt.Sprintf("Copied %d serial/key pair(s).", len(a.op.keys.items)))
}
