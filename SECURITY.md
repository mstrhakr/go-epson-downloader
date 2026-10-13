# Security policy

Report vulnerabilities privately through
[GitHub security advisories](https://github.com/mstrhakr/go-epson-downloader/security/advisories/new)
rather than a public issue.

Only the latest release is supported. Releases from v0.1.1 include `SHA256SUMS.txt`
and GitHub build provenance; verify a download with:

```powershell
gh attestation verify EpsonDownloader-v0.1.1-windows-amd64.exe --repo mstrhakr/go-epson-downloader
```

This tool downloads installers and firmware from Epson but never runs or flashes
them. It only contacts Epson HTTPS hosts and does not store credentials.
