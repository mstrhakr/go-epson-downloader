//go:build windows

package main

//go:generate go run github.com/akavel/rsrc@v0.10.2 -manifest app.manifest -o rsrc.syso

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"epson-downloader/internal/epson"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/sys/windows"
)

// version is set at release build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

type modelTable struct {
	walk.TableModelBase
	items []epson.Model
}

func (m *modelTable) RowCount() int { return len(m.items) }
func (m *modelTable) Value(row, col int) interface{} {
	if col == 0 {
		return m.items[row].Name
	}
	return m.items[row].URL
}

type packageTable struct {
	walk.TableModelBase
	items   []epson.Package
	checked map[string]bool
	changed func()
	invalid func(error)
}

func (m *packageTable) RowCount() int { return len(m.items) }
func (m *packageTable) Value(row, col int) interface{} {
	switch col {
	case 0:
		return m.items[row].Category
	case 1:
		return m.items[row].Title
	case 2:
		return m.items[row].Name
	default:
		return m.items[row].URL
	}
}
func (m *packageTable) Checked(row int) bool { return m.checked[m.items[row].URL] }
func (m *packageTable) SetChecked(row int, checked bool) error {
	if checked && m.items[row].OpenOnly {
		m.invalid(fmt.Errorf("this is a browser-only resource; use Open selected link instead of downloading it"))
		return nil
	}
	m.checked[m.items[row].URL] = checked
	m.changed()
	return nil
}

const openPlatformTab = 2

type app struct {
	client         *epson.Client
	op             openPlatformUI
	paperCut       platformView
	opFiles        platformView
	platform       epson.PlatformDownloads
	platformLoaded bool
	platformAgreed bool
	window         *walk.MainWindow
	tabs           *walk.TabWidget
	destination    *walk.LineEdit
	destinationUI  *walk.Composite
	adminUI        *walk.Composite
	driverUI       *walk.Composite
	startVersion   *walk.NumberEdit
	checkCount     *walk.NumberEdit
	scanVersions   *walk.CheckBox
	scanUI         *walk.Composite
	adminOS        *walk.ComboBox
	adminVersion   *walk.Label
	query          *walk.LineEdit
	osFilter       *walk.ComboBox
	category       *walk.ComboBox
	modelsView     *walk.TableView
	packagesView   *walk.TableView
	loadButton     *walk.PushButton
	downloadButton *walk.PushButton
	openLinkButton *walk.PushButton
	copyLinkButton *walk.PushButton
	cancelButton   *walk.PushButton
	progress       *walk.ProgressBar
	status         *walk.Label
	logView        *walk.TextEdit
	models         modelTable
	packages       packageTable
	allPackages    []epson.Package
	loadedModelURL string
	updatingOS     bool
	busy           bool
	cancel         context.CancelFunc
	closing        bool
	closed         atomic.Bool
}

func main() {
	a := newApp()
	if err := a.create(); err != nil {
		walk.MsgBox(nil, "Epson Downloader", "Cannot start the application:\n"+err.Error(), walk.MsgBoxIconError)
		os.Exit(1)
	}
	a.window.Run()
}

func newApp() *app {
	a := &app{client: epson.NewClient()}
	a.op.client = epson.NewOpenPlatformClient()
	a.paperCut, a.opFiles = newPlatformViews()
	return a
}

func (a *app) create() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home directory: %w", err)
	}
	a.packages.changed = a.refreshActions
	a.packages.invalid = a.reportError
	a.packages.checked = make(map[string]bool)
	err = (MainWindow{
		AssignTo: &a.window,
		Title:    "Epson Downloader " + version,
		Size:     Size{Width: 920, Height: 730},
		MinSize:  Size{Width: 760, Height: 620},
		Font:     Font{Family: "Segoe UI", PointSize: 9},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				AssignTo: &a.destinationUI,
				Layout:   HBox{},
				Children: []Widget{
					Label{Text: "Save to:"},
					LineEdit{AssignTo: &a.destination, Text: filepath.Join(home, "Downloads", "Epson"), StretchFactor: 1},
					PushButton{Text: "&Browse...", OnClicked: a.browse},
					PushButton{Text: "&Open folder", OnClicked: a.openFolder},
				},
			},
			TabWidget{
				AssignTo:              &a.tabs,
				StretchFactor:         1,
				OnCurrentIndexChanged: a.tabChanged,
				Pages: []TabPage{
					{
						Title:  "Device Admin",
						Layout: VBox{},
						Children: []Widget{
							Composite{
								AssignTo: &a.adminUI,
								Layout:   VBox{},
								Children: []Widget{
									Label{Text: "Use Epson's published version to download the direct file. No email form or version scanning is needed."},
									Composite{
										Layout: HBox{},
										Children: []Widget{
											Label{Text: "Operating system:"},
											ComboBox{
												AssignTo: &a.adminOS, Model: []string{"Windows 11"}, CurrentIndex: 0, MinSize: Size{Width: 210},
												OnCurrentIndexChanged: func() {
													if !a.updatingOS && a.adminVersion != nil {
														a.adminVersion.SetText("OS changed. Check the published version again.")
													}
												},
											},
											PushButton{Text: "&Check published version", OnClicked: a.checkAdmin},
											HSpacer{},
										},
									},
									Label{AssignTo: &a.adminVersion, Text: "Check the version to load Epson's available OS choices. Windows 11 is the default."},
									CheckBox{
										AssignTo: &a.scanVersions, Text: "Advanced: scan a version range instead of using the published version",
										OnCheckedChanged: func() {
											if a.scanUI != nil {
												a.scanUI.SetEnabled(a.scanVersions.Checked())
											}
										},
									},
									Composite{
										AssignTo: &a.scanUI, Enabled: false, Layout: HBox{},
										Children: []Widget{
											Label{Text: "Starting version:"},
											NumberEdit{AssignTo: &a.startVersion, Value: float64(epson.DefaultAdminVersion), MinValue: 1, MaxValue: 998999, Decimals: 0, SpinButtonsVisible: true, MinSize: Size{Width: 100}},
											Label{Text: "Checks ahead:"},
											NumberEdit{AssignTo: &a.checkCount, Value: float64(10), MinValue: 1, MaxValue: 100, Decimals: 0, SpinButtonsVisible: true, MinSize: Size{Width: 100}},
											HSpacer{},
										},
									},
									Label{Text: "Advanced scan only: steps of 10, using local packages and latest_version.txt as hints. It is not a global latest-version check."},
									Composite{
										Layout: HBox{},
										Children: []Widget{
											PushButton{Text: "&Download Device Admin", OnClicked: a.downloadAdmin},
											HSpacer{},
										},
									},
								},
							},
							VSpacer{},
						},
					},
					{
						Title:  "Model downloads",
						Layout: VBox{},
						Children: []Widget{
							Composite{
								AssignTo:      &a.driverUI,
								Layout:        VBox{},
								StretchFactor: 1,
								Children: []Widget{
									Label{Text: "Search once for drivers, firmware, utilities, and public manuals. Select a printer and load its downloads."},
									Composite{
										Layout: HBox{},
										Children: []Widget{
											Label{Text: "Model:"},
											LineEdit{
												AssignTo: &a.query, CueBanner: "WF-C5790 or AM-C4000", StretchFactor: 1,
												OnKeyDown: func(key walk.Key) {
													if key == walk.KeyReturn && !a.busy {
														a.searchModels()
													}
												},
											},
											PushButton{Text: "&Search", OnClicked: a.searchModels},
										},
									},
									TableView{
										AssignTo: &a.modelsView, Model: &a.models, StretchFactor: 1,
										MinSize: Size{Height: 95},
										Columns: []TableViewColumn{{Title: "Matching printer", Width: 340}, {Title: "Support page", Width: 460}},
										OnCurrentIndexChanged: func() {
											a.clearPackages()
											a.loadedModelURL = ""
											a.refreshActions()
										},
										OnItemActivated: a.loadPackages,
									},
									Composite{
										Layout: HBox{},
										Children: []Widget{
											Label{Text: "Operating system:"},
											ComboBox{
												AssignTo: &a.osFilter, Model: []string{"Windows 11"}, CurrentIndex: 0, MinSize: Size{Width: 210},
												OnCurrentIndexChanged: func() {
													if !a.updatingOS && a.loadedModelURL != "" && !a.busy {
														a.loadPackages()
													}
												},
											},
											PushButton{AssignTo: &a.loadButton, Text: "&Load downloads", Enabled: false, OnClicked: a.loadPackages},
											Label{Text: "Show:"},
											ComboBox{
												AssignTo: &a.category, Model: []string{"All", "Drivers", "Firmware", "Manuals", "Utilities", platformFolder}, CurrentIndex: 0,
												OnCurrentIndexChanged: a.applyCategory,
											},
											HSpacer{},
										},
									},
									TableView{
										AssignTo: &a.packagesView, Model: &a.packages, CheckBoxes: true, StretchFactor: 1,
										MinSize: Size{Height: 95},
										Columns: []TableViewColumn{
											{Title: "Category", Width: 90}, {Title: "Description", Width: 300},
											{Title: "Filename", Width: 270}, {Title: "Link", Width: 400},
										},
										OnCurrentIndexChanged: a.refreshActions,
										OnItemActivated:       a.openSelectedLink,
									},
									Composite{
										Layout: HBox{},
										Children: []Widget{
											PushButton{AssignTo: &a.downloadButton, Text: "&Download checked (0)", Enabled: false, OnClicked: a.downloadPackages},
											PushButton{AssignTo: &a.openLinkButton, Text: "Open selected &link", Enabled: false, OnClicked: a.openSelectedLink},
											PushButton{AssignTo: &a.copyLinkButton, Text: "Cop&y link", Enabled: false, OnClicked: a.copySelectedLink},
											HSpacer{},
										},
									},
									Label{Text: "Selections are kept across category filters. Files go into category subfolders; nothing is installed or flashed."},
								},
							},
						},
					},
					a.openPlatformPage(),
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					ProgressBar{AssignTo: &a.progress, MaxValue: 100, StretchFactor: 1},
					PushButton{AssignTo: &a.cancelButton, Text: "&Cancel", Enabled: false, OnClicked: a.cancelWork},
				},
			},
			Label{AssignTo: &a.status, Text: "Ready. Choose a save folder and a task."},
			TextEdit{
				AssignTo: &a.logView, ReadOnly: true, VScroll: true,
				MinSize: Size{Height: 135}, MaxSize: Size{Height: 135},
			},
		},
	}).Create()
	if err != nil {
		return err
	}
	a.window.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if a.busy {
			*canceled = true
			if !a.closing && walk.MsgBox(a.window, "Close Epson Downloader", "Cancel the current task and close after cleanup?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
				a.closing = true
				a.cancelWork()
			}
			return
		}
		a.closed.Store(true)
	})
	a.appendLog("Ready. Downloads use Epson's public US support pages and direct HTTPS files.")
	return nil
}

func (a *app) appendLog(text string) {
	a.logView.AppendText(time.Now().Format("15:04:05") + "  " + text + "\r\n")
}

func (a *app) dispatch(f func()) {
	if !a.closed.Load() {
		a.window.Synchronize(func() {
			if !a.closed.Load() {
				f()
			}
		})
	}
}

func (a *app) reportError(err error) {
	a.appendLog("ERROR: " + err.Error())
	a.status.SetText("Failed. See the log for details.")
	walk.MsgBox(a.window, "Epson Downloader", err.Error(), walk.MsgBoxIconError)
}

func (a *app) refreshActions() {
	if a.loadButton == nil || a.downloadButton == nil || a.modelsView == nil {
		return
	}
	a.loadButton.SetEnabled(!a.busy && a.modelsView.CurrentIndex() >= 0)
	checkedCount := 0
	for _, checked := range a.packages.checked {
		if checked {
			checkedCount++
		}
	}
	a.downloadButton.SetEnabled(!a.busy && checkedCount > 0)
	a.downloadButton.SetText(fmt.Sprintf("&Download checked (%d)", checkedCount))
	if a.openLinkButton != nil && a.copyLinkButton != nil && a.packagesView != nil {
		hasSelection := !a.busy && a.packagesView.CurrentIndex() >= 0
		a.openLinkButton.SetEnabled(hasSelection)
		a.copyLinkButton.SetEnabled(hasSelection)
	}
}

func (a *app) clearPackages() {
	a.allPackages, a.packages.items = nil, nil
	a.packages.checked = make(map[string]bool)
	a.packages.PublishRowsReset()
	a.refreshActions()
}

func (a *app) folder() (string, error) {
	folder := strings.TrimSpace(a.destination.Text())
	if folder == "" || !filepath.IsAbs(folder) {
		return "", fmt.Errorf("choose an absolute save folder")
	}
	folder = filepath.Clean(folder)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", fmt.Errorf("create save folder: %w", err)
	}
	return folder, nil
}

func (a *app) browse() {
	dialog := walk.FileDialog{Title: "Choose download folder", FilePath: a.destination.Text()}
	ok, err := dialog.ShowBrowseFolder(a.window)
	if err != nil {
		a.reportError(err)
	} else if ok {
		a.destination.SetText(dialog.FilePath)
	}
}

func (a *app) openFolder() {
	folder, err := a.folder()
	if err == nil {
		err = a.open(folder)
	}
	if err != nil {
		a.reportError(err)
	}
}

func (a *app) open(target string) error {
	path, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	return windows.ShellExecute(windows.Handle(a.window.Handle()), verb, path, nil, nil, windows.SW_SHOWNORMAL)
}

func (a *app) selectedLink() string {
	index := a.packagesView.CurrentIndex()
	if index < 0 || index >= len(a.packages.items) {
		return ""
	}
	return a.packages.items[index].URL
}

func (a *app) openSelectedLink() {
	if link := a.selectedLink(); link != "" {
		if err := a.open(link); err != nil {
			a.reportError(err)
		}
	}
}

func (a *app) copySelectedLink() {
	if link := a.selectedLink(); link != "" {
		if err := walk.Clipboard().SetText(link); err != nil {
			a.reportError(err)
		} else {
			a.status.SetText("Download link copied.")
		}
	}
}

func (a *app) applyCategory() {
	if a.category == nil {
		return
	}
	a.packages.items = nil
	category := a.category.Text()
	for _, pkg := range a.allPackages {
		if category == "All" || category == pkg.Category {
			a.packages.items = append(a.packages.items, pkg)
		}
	}
	a.packages.PublishRowsReset()
	a.refreshActions()
}

func (a *app) setOperatingSystems(combo *walk.ComboBox, systems []string, selected string) {
	a.updatingOS = true
	defer func() { a.updatingOS = false }()
	combo.SetModel(systems)
	combo.SetCurrentIndex(slices.Index(systems, selected))
}
func (a *app) setBusy(busy bool) {
	a.busy = busy
	a.destinationUI.SetEnabled(!busy)
	a.adminUI.SetEnabled(!busy)
	a.scanUI.SetEnabled(!busy && a.scanVersions.Checked())
	a.driverUI.SetEnabled(!busy)
	a.op.root.SetEnabled(!busy)
	for _, v := range a.platformViews() {
		if v.root != nil {
			v.root.SetEnabled(!busy)
		}
		a.refreshPlatformActions(v)
	}
	a.cancelButton.SetEnabled(busy)
	a.refreshActions()
	a.refreshKeyActions()
}

func (a *app) run(title string, work func(context.Context) (func(), error)) {
	if a.busy {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.setBusy(true)
	a.progress.SetValue(0)
	a.progress.SetMarqueeMode(true)
	a.status.SetText(title)
	a.appendLog(title)
	go func() {
		finish, err := work(ctx)
		cancel()
		a.dispatch(func() {
			a.progress.SetMarqueeMode(false)
			a.setBusy(false)
			a.cancel = nil
			switch {
			case errors.Is(err, context.Canceled) && !errors.Is(err, epson.ErrDownloadCleanup):
				a.status.SetText("Cancelled. Incomplete downloads were removed.")
				a.appendLog("Cancelled.")
			case err != nil:
				a.reportError(err)
			default:
				if finish != nil {
					finish()
				}
			}
			if a.closing {
				a.window.Close()
			}
		})
	}()
}

func (a *app) cancelWork() {
	if a.cancel != nil {
		a.cancel()
		a.cancelButton.SetEnabled(false)
		a.status.SetText("Cancelling and cleaning up...")
	}
}

func (a *app) log(text string) {
	a.dispatch(func() { a.appendLog(text) })
}

func (a *app) showProgress(p epson.Progress) {
	a.dispatch(func() {
		if p.Total > 0 {
			a.progress.SetMarqueeMode(false)
			a.progress.SetValue(int(float64(p.Bytes) / float64(p.Total) * 100))
			a.status.SetText(fmt.Sprintf("%s: %.1f / %.1f MB", p.Name, float64(p.Bytes)/(1<<20), float64(p.Total)/(1<<20)))
		} else {
			a.progress.SetMarqueeMode(true)
			a.status.SetText(fmt.Sprintf("%s: %.1f MB", p.Name, float64(p.Bytes)/(1<<20)))
		}
	})
}

func (a *app) showAdminRelease(release epson.AdminRelease) {
	a.setOperatingSystems(a.adminOS, release.OperatingSystems, release.SelectedOS)
	a.adminVersion.SetText(fmt.Sprintf("Published version: %s -> %s [%s]", release.DisplayVersion, release.Package.Name, release.SelectedOS))
}

func (a *app) checkAdmin() {
	filter := a.adminOS.Text()
	a.run("Checking the published Device Admin version...", func(ctx context.Context) (func(), error) {
		a.log("Version hint: " + epson.SupportURL + epson.AdminPath)
		release, err := a.client.PublishedAdmin(ctx, filter)
		if err != nil {
			return nil, err
		}
		a.log("Verifying direct file: " + release.Package.URL)
		if err := a.client.VerifyPackage(ctx, release.Package); err != nil {
			return nil, err
		}
		return func() {
			a.showAdminRelease(release)
			a.status.SetText("Published Device Admin version verified. Ready to download.")
			a.appendLog(a.adminVersion.Text())
		}, nil
	})
}

func (a *app) downloadAdmin() {
	folder, err := a.folder()
	if err != nil {
		a.reportError(err)
		return
	}
	seed, checks := int(a.startVersion.Value()), int(a.checkCount.Value())
	advanced, filter := a.scanVersions.Checked(), a.adminOS.Text()
	a.run("Finding Device Admin...", func(ctx context.Context) (func(), error) {
		var pkg epson.Package
		var version int
		var release epson.AdminRelease
		if advanced {
			a.log("Advanced scan is OS-independent; confirm compatibility before installing.")
			start, err := epson.AdminStartVersion(folder, seed)
			if err != nil {
				return nil, err
			}
			a.log(fmt.Sprintf("Search range: %d-%d, step 10.", start, start+checks*10))
			pkg, version, err = a.client.AdminPackage(ctx, start, checks, a.log)
			if err != nil {
				return nil, err
			}
			a.log("Highest available package in the checked range: " + pkg.Name)
		} else {
			var err error
			release, err = a.client.PublishedAdmin(ctx, filter)
			if err != nil {
				return nil, err
			}
			pkg, version = release.Package, release.Version
			a.log("Published version " + release.DisplayVersion + ": " + pkg.URL)
			if err := a.client.VerifyPackage(ctx, pkg); err != nil {
				return nil, err
			}
		}
		path, err := a.client.Download(ctx, pkg, folder, a.showProgress)
		if err != nil {
			return nil, err
		}
		if err := epson.RememberAdminVersion(folder, version); err != nil {
			return nil, fmt.Errorf("package saved to %s, but %w", path, err)
		}
		return func() {
			if !advanced {
				a.showAdminRelease(release)
			}
			a.progress.SetValue(100)
			a.status.SetText("Device Admin ready: " + pkg.Name)
			a.appendLog("Ready (downloaded or existing size matched): " + path)
		}, nil
	})
}

func (a *app) searchModels() {
	query := a.query.Text()
	a.clearPackages()
	a.models.items = nil
	a.models.PublishRowsReset()
	a.run("Searching Epson printer support...", func(ctx context.Context) (func(), error) {
		models, err := a.client.SearchModels(ctx, query)
		return func() {
			a.models.items = models
			a.models.PublishRowsReset()
			a.modelsView.SetCurrentIndex(0)
			a.refreshActions()
			a.status.SetText(fmt.Sprintf("%d matching printer(s). Select one and load its packages.", len(models)))
			a.appendLog(a.status.Text())
		}, err
	})
}

func (a *app) loadPackages() {
	index := a.modelsView.CurrentIndex()
	if a.busy || index < 0 || index >= len(a.models.items) {
		return
	}
	model, filter := a.models.items[index], a.osFilter.Text()
	if a.loadedModelURL != model.URL {
		filter = ""
	}
	a.clearPackages()
	cached, haveCache := a.platform, a.platformLoaded
	a.run("Loading downloads for "+model.Name+"...", func(ctx context.Context) (func(), error) {
		a.log("Support page: " + model.URL)
		downloads, err := a.client.Downloads(ctx, model, filter)
		if err != nil {
			return nil, err
		}
		if !haveCache {
			if fetched, perr := a.client.PlatformDownloads(ctx); perr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				a.log("Open Platform resources unavailable (continuing): " + perr.Error())
			} else {
				cached, haveCache = fetched, true
			}
		}
		extra := platformPackages(cached, model.Name)
		if len(extra) > 0 {
			downloads.Packages = append(downloads.Packages, extra...)
			a.log(fmt.Sprintf("Added %d Open Platform item(s); downloading them accepts Epson's EULA: %s", len(extra), epson.PlatformEULAURL))
		}
		return func() {
			if haveCache && !a.platformLoaded {
				a.showPlatformDownloads(cached)
			}
			a.loadedModelURL = model.URL
			a.setOperatingSystems(a.osFilter, downloads.OperatingSystems, downloads.SelectedOS)
			a.allPackages = downloads.Packages
			hasStandard, hasLite := false, false
			for _, pkg := range downloads.Packages {
				hasStandard = hasStandard || pkg.Kind == "Standard"
				hasLite = hasLite || pkg.Kind == "Lite"
			}
			selected := make(map[string]bool)
			for _, pkg := range downloads.Packages {
				kind := pkg.Kind
				if !pkg.OpenOnly && !selected[kind] && (kind == "Standard" || kind == "Standard Core" || kind == "Lite" && !hasStandard || kind == "Driver" && !hasStandard && !hasLite) {
					a.packages.checked[pkg.URL] = true
					selected[kind] = true
				}
			}
			a.applyCategory()
			a.status.SetText("Downloads loaded. Review checked files or select a link to open.")
			a.appendLog(fmt.Sprintf("Found %d resources for %s [%s], %d OS choices.", len(downloads.Packages), model.Name, downloads.SelectedOS, len(downloads.OperatingSystems)))
		}, err
	})
}

func (a *app) downloadPackages() {
	folder, err := a.folder()
	if err != nil {
		a.reportError(err)
		return
	}
	var packages []epson.Package
	for _, pkg := range a.allPackages {
		if a.packages.checked[pkg.URL] {
			packages = append(packages, pkg)
		}
	}
	if len(packages) == 0 {
		a.reportError(fmt.Errorf("check at least one downloadable file"))
		return
	}
	a.run("Downloading checked files...", func(ctx context.Context) (func(), error) {
		for i, pkg := range packages {
			a.log(fmt.Sprintf("Package %d/%d: %s", i+1, len(packages), pkg.Name))
			path, err := a.client.Download(ctx, pkg, filepath.Join(folder, pkg.Category), a.showProgress)
			if err != nil {
				return nil, err
			}
			a.log("Ready (downloaded or existing size matched): " + path)
		}
		return func() {
			a.progress.SetValue(100)
			a.status.SetText(fmt.Sprintf("%d file(s) ready in the save folder's category subfolders.", len(packages)))
			a.appendLog(a.status.Text())
		}, nil
	})
}
