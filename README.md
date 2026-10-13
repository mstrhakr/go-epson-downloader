# Epson Downloader

[![CI](https://github.com/mstrhakr/go-epson-downloader/actions/workflows/ci.yml/badge.svg)](https://github.com/mstrhakr/go-epson-downloader/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mstrhakr/go-epson-downloader)](https://github.com/mstrhakr/go-epson-downloader/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A small Windows desktop utility written in Go with Walk (native Win32 controls).
No PowerShell, Excel modules, Node.js, WebView2, or administrator rights are needed
to run it. Distribute just `EpsonDownloader.exe`.

Download the latest build from
[Releases](https://github.com/mstrhakr/go-epson-downloader/releases/latest).
Each release includes standalone amd64 and arm64 `.exe` files, `.zip` files with
the README and license, and `SHA256SUMS.txt`. Verify that a download was built by
this repository's release workflow with:

```powershell
gh attestation verify EpsonDownloader-v0.1.1-windows-amd64.exe --repo mstrhakr/go-epson-downloader
```

## Current features

- **Device Admin tab:** reads the published version from Epson's support page
  (for example, `5.9.0` maps to `EDA_590.exe`), verifies the direct download
  with one HEAD request, and downloads from Epson's FTP HTTPS endpoint without
  submitting an email form. **Check published version** also loads the page's OS
  choices. Windows 11 is the default. Changing OS requires a fresh check; an
  unavailable release or failed request is reported, not silently substituted.
  The optional **Advanced** scan preserves the old bounded search: version 570,
  ten checks ahead, increments of ten, with local files and `latest_version.txt`
  as hints. It is OS-independent and does not guarantee the latest global release.
  The remembered version is written only after a successful download or a
  matching-size existing file; it does not override the normal page-based lookup.
- **Model downloads tab:** search a printer once and load its drivers, firmware,
  utilities, and public manuals together. OS choices come from that product's
  actual page, including older Windows, Server, and macOS versions where offered.
  Windows 11 is the initial default; if the product does not offer it, the first
  offered OS is selected and displayed. An explicitly selected unsupported OS
  always produces an error. Changing OS reloads resources without searching again.
- Filter the resource list by category, check files to download, or open/copy
  the selected link. Online HTML manuals open in a browser; PDFs are downloadable.
  Epson's Linux driver/manual portals are shown as browser-only links.
  Files go into `Drivers`, `Firmware`, `Manuals`, or `Utilities` subfolders.
  Checked selections survive category filtering, and the download button shows
  their total count. Firmware and manuals are never checked automatically.
- The first Standard and Standard Core drivers are checked by default; Lite
  replaces Standard when it is absent. For other driver formats, the first
  generic driver is checked when neither Standard nor Lite is present.
- **Open Platform tab** has three sub-tabs: **Product keys**, **PaperCut**, and
  **Firmware & documents**. Download lists load from Epson's public Open Platform
  download pages the first time either download sub-tab opens (**Reload**
  refreshes them).
- **PaperCut sub-tab:** Epson Open Platform Solution for PaperCut (Windows,
  macOS, `.rpm`, `.deb`), its release history, and the PaperCut setup manuals.
  The Windows installer is checked by default.
- **Firmware & documents sub-tab:** Open Platform firmware updaters, per-model
  hardware setup and administrator guides, general Open Platform manuals, and
  the browser-only security-profile web tool. Type a model (for example `WF-6590`)
  to show that model's items plus general documents.
- Open Platform downloads require ticking **I agree to Epson's license terms**
  ([EULA](https://support.epson.net/eula/) and the security-profile use
  conditions); the checkbox is shared by both download sub-tabs. Files go into
  `Open Platform\<Category>`.
- The Open Platform firmware is **not** the same as epson.com firmware. For
  example, WF-6590 Open Platform firmware is `DB30a1`, while epson.com only offers
  recovery firmware; WF-5690 is `GB30UA` versus `GC30KA`; and the WF-8590 /
  WF-R8590 / WF-R5690 updaters are not offered on epson.com at all. Newer models
  have Open Platform built in and only list setup guides.
- The Model downloads tab also appends matching Open Platform items under the
  **Open Platform** category (these are never checked automatically).
- **Product keys sub-tab:** issues Epson Open Platform product keys through
  Epson's public license form with plain HTTPS requests (no browser or Selenium).
  Paste serials (lines, commas, or spaces) or import a `.txt`/`.csv`; entries are
  uppercased, de-duplicated, and checked locally (Epson accepts 5–15 letters or
  digits). Area, country/region, and application lists load from the live form
  when the tab opens; the defaults are North America / United States / PaperCut MF.
  Lists over 150 serials are split into multiple Epson requests. Results can be
  copied individually or all at once and are appended by default to
  `Open Platform Keys\OpenPlatformKeys.csv` in the save folder. Epson generates
  keys without checking that a serial is a real printer, so a typo produces a
  valid-looking but useless key. Keys are registered in the printer's Web Config.
- Shared folder selection, progress, cancellation, and an on-screen log.
  Closing during a task cancels it and waits for temporary-file cleanup.
- Downloads are written to unique temporary files in the destination and renamed
  only after completion. Existing files are kept: a nonempty regular file with
  the same size as the server's package is reused; an unverifiable or mismatched
  file produces an error. Size matching is **not** cryptographic verification.
  Interrupted downloads are removed rather than resumed.

Installers are **downloaded, not executed**; firmware is **not flashed**.
EXE, ZIP, PDF, RPM, and DEB downloads have their file signatures checked.
Downloaded Epson packages may need
administrator rights when installed separately. This is an unofficial utility,
not affiliated with Epson.

## Build and run

On Windows with Go 1.26 or newer (`go.mod` pins the `go1.26.9` toolchain, which Go
downloads automatically):

```powershell
.\build.cmd
.\bin\EpsonDownloader.exe
```

The first build needs internet access to restore Go dependencies and the pinned
[go-winres](https://github.com/tc-hib/go-winres) resource generator. Resources live
in `cmd/epson-downloader/winres/`: the app icon, the Common Controls 6 / DPI /
non-elevated manifest, and version info shown in the exe's Properties → Details.
The result is a stripped GUI executable (no console window). No C compiler is needed.

- The version defaults to `git describe --tags --always --dirty` (for example
  `v0.1.0` or `v0.1.0-2-gfe60839-dirty`) and appears in the window title and
  file properties. Override it with `-Version v1.2.3`.
- `-Arch arm64` builds `bin\EpsonDownloader-arm64.exe`; amd64 is the default and
  the tested target.
- `build.cmd` runs `build.ps1` with `-ExecutionPolicy Bypass` (preferring `pwsh`)
  for machines that block unsigned scripts. Policies enforced by Group Policy
  still apply.

For development:

```powershell
go generate ./cmd/epson-downloader   # once; tests need the manifest resource
go test ./...
go vet ./...
```

Optional read-only live checks (no installer payloads are downloaded):

```powershell
$env:EPSON_LIVE_TEST = '1'
go test ./internal/epson -run TestLive -v
Remove-Item Env:\EPSON_LIVE_TEST
```

An optional real-download check retrieves a public PDF into a temporary test
folder, verifies it, and removes it afterward:

```powershell
$env:EPSON_LIVE_DOWNLOAD_TEST = '1'
go test ./internal/epson -run '^TestLiveManualDownload$' -v
Remove-Item Env:\EPSON_LIVE_DOWNLOAD_TEST
```

The `-run TestLive` command above also reads the live Open Platform option lists
and download pages without issuing a key or downloading files. To issue a real key end to end, use a serial you own
(North America / US / PaperCut MF):

```powershell
$env:EPSON_LIVE_KEY_SERIAL = 'XAV0000000'
go test ./internal/epson -run '^TestLiveOpenPlatformIssue$' -v
Remove-Item Env:\EPSON_LIVE_KEY_SERIAL
```

## CI and releases

- **CI** (`.github/workflows/ci.yml`) runs on pushes to `main` and pull requests on
  `windows-latest`: `gofmt`, `go mod tidy -diff`, `go vet`,
  [golangci-lint](https://golangci-lint.run/) (config in `.golangci.yml`),
  [govulncheck](https://go.dev/doc/security/vuln/), `go test`, and a build uploaded
  as a workflow artifact. govulncheck fails the build when a known vulnerability is
  reachable; standard-library findings are fixed by bumping `toolchain` in `go.mod`.
- **Release** (`.github/workflows/release.yml`) runs when a `v*.*.*` tag is pushed,
  tests and builds amd64 and arm64 executables with that version, attests their
  build provenance, and publishes a GitHub release with generated notes (grouped
  by label via `.github/release.yml`). Tags containing `-` (for example
  `v0.2.0-rc1`) are marked prerelease.

```powershell
git tag v0.1.0
git push origin v0.1.0
```

- **Dependabot** checks Go modules and GitHub Actions weekly.

## Preserved Epson endpoints

These are the US endpoints used by the original working scripts:

| Purpose | Endpoint / page contract |
| --- | --- |
| Published Device Admin version | `https://epson.com/Support/Other-Products/Epson-Software/Epson-Device-Admin/s/SPT_DEVICEADMIN-NS?review-filter=Windows+11`, anchor title `Epson Device Admin v<major>.<minor>.<patch>` |
| Device Admin | `https://ftp.epson.com/drivers/EDA_<version>.exe` |
| Printer search | `https://epson.com/supportsearch?searchText=<URL-encoded-model>` |
| Support page | Printer anchors with a matching `title` and `/Support/Printers/...` path |
| Available operating systems | `<select id="review-filter">` option values on the product page |
| OS selection | Support page query `review-filter=<URL-encoded-option-value>` |
| Packages | Anchors with `data-dl_name`, `data-dl_title`, `data-dl_type`, and direct `href`; Standard / Core / Lite filenames remain recognized |
| Firmware | `icc_firmware` metadata or `FW_` filenames; EXE and ZIP packages are supported without extraction or execution |
| Public manuals | PDF / HTML guide links inside the product page's `manuals` section, commonly on `https://files.support.epson.com/docid/...` |
| Open Platform form | `https://openplatform.epson.biz/license-op/inputInformation.html`: session cookie, multipart `uploadForm` with `selectLocale=en`, file mode (`selectInputSerialNo-2`), `serialNoFile` (one serial per line, max 150), `areaSelect` + per-area country select, `applicationSelect` (+ `applicationNameText` for Other), `doNext` |
| Open Platform issue | Confirmation form `doOnceIssue` with Teeda transaction token; keys in `tbody#productKeyListItems` rows (`span#serialNo`, `span#productKey`); errors in `ul#allMessages` or `span#errorMsg` |
| Open Platform downloads | `https://openplatform.epson.biz/download/pc/` (PaperCut) and `/download/op/en/` (general): static tables; device tables have a `Firmware` header, models split by `<br>`, dates `dd/mm/yyyy`. Files are direct `https://openplatform.epson.biz/download/files/{pc,op}/<file>` links; the EULA checkbox is client-side only |
The app parses HTML structurally, deduplicates links, and permits only the
configured Epson HTTPS origins (including redirects). All network work runs
off the UI thread. A changed Epson page or failed request produces an explicit
error rather than a false success.

## Scope and extension points

The first version intentionally supports manual driver searches, not the
PowerShell script's `*Devices*.xlsx` fleet import or `Driver-Files.txt` inventory.
Authenticated partner/service manuals are not included, and credentials from
existing scripts are not embedded. Fleet import, persistent preferences,
firmware installation, and installer execution can be added later.

`internal/epson` owns search, discovery, and downloading independently of the UI.
`cmd/epson-downloader` owns the Walk window and tabs. New functions can share the
same download/progress/cancellation services without adding runtime scripts.

## License

[MIT](LICENSE). Epson and related product names are trademarks of Seiko Epson Corporation; this project is not affiliated with Epson.

