//go:build windows

package main

import (
	"runtime"
	"testing"

	"epson-downloader/internal/epson"
)

func TestWindowInitialStateAndFiltering(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a := newApp()
	if err := a.create(); err != nil {
		t.Fatal(err)
	}
	defer a.window.Dispose()
	if a.op.loaded || !a.op.issueButton.Enabled() || a.op.copyAll.Enabled() || !a.op.saveLog.Checked() {
		t.Fatal("unexpected Open Platform startup state")
	}
	a.op.serials.SetText("xaa1234567\r\nXAA1234567, ABCDE12345")
	if a.op.serialCount.Text() != "2 serials" {
		t.Fatalf("serial count = %q", a.op.serialCount.Text())
	}
	a.op.serials.SetText("XAV-1")
	if a.op.serialCount.Text() != "Invalid entry" {
		t.Fatalf("serial count = %q", a.op.serialCount.Text())
	}
	a.showOpenPlatformOptions(epson.OpenPlatformOptions{
		Areas: []epson.Area{
			{Option: epson.Option{Value: "Euro", Label: "Europe"}, Countries: []epson.Option{{Value: "DE", Label: "Germany"}}},
			{Option: epson.Option{Value: "NA", Label: "North America"}, Countries: []epson.Option{{Value: "CA", Label: "Canada"}, {Value: "US", Label: "United States"}}},
		},
		Applications: []epson.Option{{Value: "PaperCutMF", Label: "PaperCut MF(PaperCut)"}, {Value: "Other", Label: "Other"}},
	})
	if a.op.area.Text() != "North America" || a.op.country.Text() != "United States" || a.op.application.Text() != "PaperCut MF(PaperCut)" || a.op.otherApp.Enabled() {
		t.Fatalf("defaults = %q %q %q", a.op.area.Text(), a.op.country.Text(), a.op.application.Text())
	}
	a.op.application.SetCurrentIndex(1)
	a.op.area.SetCurrentIndex(0)
	if !a.op.otherApp.Enabled() || a.op.country.Text() != "Germany" {
		t.Fatal("Other application or area change not handled")
	}
	a.op.serials.SetText("XAA1234567")
	if _, _, _, err := a.keyRequest(); err == nil {
		t.Fatal("Other without an application name must fail")
	}
	a.op.otherApp.SetText("Custom")
	req, country, application, err := a.keyRequest()
	if err != nil || req.Area != "Euro" || req.Country != "DE" || req.Application != "Other" || country != "Germany" || application != "Other: Custom" {
		t.Fatalf("request = %+v %q %q %v", req, country, application, err)
	}
	if a.startVersion.Value() != 570 || a.checkCount.Value() != 10 {
		t.Fatal("incorrect advanced scan defaults")
	}
	if a.adminOS.Text() != "Windows 11" || a.osFilter.Text() != "Windows 11" || a.category.Text() != "All" {
		t.Fatal("incorrect default OS/category")
	}
	if a.busy || a.cancelButton.Enabled() || a.downloadButton.Enabled() || a.loadButton.Enabled() || a.scanUI.Enabled() {
		t.Fatal("unexpected startup action state")
	}
	a.allPackages = []epson.Package{
		{Category: "Drivers", Title: "Driver", URL: "https://ftp.epson.com/drivers/driver.exe"},
		{Category: "Firmware", Title: "Firmware", URL: "https://ftp.epson.com/drivers/FW_test.zip"},
	}
	a.packages.checked[a.allPackages[0].URL] = true
	a.applyCategory()
	if a.packages.RowCount() != 2 || !a.downloadButton.Enabled() {
		t.Fatal("resources did not populate")
	}
	a.category.SetCurrentIndex(2)
	if a.packages.RowCount() != 1 || a.packages.items[0].Category != "Firmware" || !a.downloadButton.Enabled() {
		t.Fatal("filter must preserve checked packages in other categories")
	}
	a.clearPackages()
	if a.packages.RowCount() != 0 || a.downloadButton.Enabled() {
		t.Fatal("old resources or selection survived clearing")
	}

	if a.op.subTabs.Pages().Len() != 3 || a.tabs.Pages().At(openPlatformTab).Title() != "Open Platform" {
		t.Fatal("Open Platform tab must hold the keys, PaperCut and firmware sub-tabs")
	}
	if a.paperCut.download.Enabled() || a.paperCut.agree.Checked() || a.platformLoaded {
		t.Fatal("unexpected PaperCut startup state")
	}
	fw := epson.PlatformResource{
		Package: epson.Package{Category: "Firmware", Kind: "Windows", Title: "Open Platform firmware updater DB30a1", Name: "WF-6590Series_DB30a1.exe", URL: "https://openplatform.epson.biz/download/files/op/WF-6590Series_DB30a1.exe"},
		Version: "DB30a1", Updated: "2026-03-05", Models: []string{"WF-6590 Series"},
	}
	a.showPlatformDownloads(epson.PlatformDownloads{
		Models: []string{"WF-6590 Series", "WF-C5790 Series"},
		Resources: []epson.PlatformResource{
			{Package: epson.Package{Category: "PaperCut software", Kind: "Windows", Title: "Win", Name: "ops.exe", URL: "https://openplatform.epson.biz/download/files/pc/ops.exe"}, Version: "2.7.4"},
			{Package: epson.Package{Category: "PaperCut software", Kind: "macOS", Title: "Mac", Name: "ops.dmg", URL: "https://openplatform.epson.biz/download/files/pc/ops.dmg"}},
			fw,
			{Package: epson.Package{Category: "Manuals", Kind: "Hardware Setup Guide", Title: "Hardware Setup Guide", Name: "c5790.pdf", URL: "https://openplatform.epson.biz/download/files/op/c5790.pdf"}, Models: []string{"WF-C5790 Series"}},
			{Package: epson.Package{Category: "Tools", Kind: "Web tool", Title: "Generator", Name: "(open in browser)", URL: "https://openplatform.epson.biz/download/opg/index.htm", OpenOnly: true}},
		},
	})
	if a.paperCut.table.RowCount() != 2 || a.opFiles.table.RowCount() != 3 {
		t.Fatalf("scoped rows = %d / %d", a.paperCut.table.RowCount(), a.opFiles.table.RowCount())
	}
	if got := a.checkedPlatform(&a.paperCut); len(got) != 1 || got[0].Name != "ops.exe" {
		t.Fatalf("default PaperCut selection = %+v", got)
	}
	if a.paperCut.download.Enabled() {
		t.Fatal("downloads must wait for the license agreement")
	}
	a.paperCut.agree.SetChecked(true)
	if !a.platformAgreed || !a.opFiles.agree.Checked() || !a.paperCut.download.Enabled() || a.opFiles.download.Enabled() {
		t.Fatal("agreement must sync across sub-tabs and enable checked downloads")
	}
	a.opFiles.filter.SetText("WF-6590")
	if a.opFiles.table.RowCount() != 2 {
		t.Fatalf("model filter rows = %d", a.opFiles.table.RowCount())
	}
	a.opFiles.category.SetCurrentIndex(1)
	if a.opFiles.table.RowCount() != 1 || a.opFiles.table.items[0].Name != fw.Name {
		t.Fatal("category filter failed")
	}
	if err := a.opFiles.table.SetChecked(0, true); err != nil || !a.opFiles.download.Enabled() {
		t.Fatal("checking firmware must enable download")
	}
	pkgs := platformPackages(a.platform, "Epson WorkForce Pro WF-6590")
	if len(pkgs) != 1 || pkgs[0].Category != platformFolder || pkgs[0].Title != "Open Platform firmware DB30a1 (Windows, 2026-03-05)" {
		t.Fatalf("model tab packages = %+v", pkgs)
	}
}
