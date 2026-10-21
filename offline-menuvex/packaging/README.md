# MenuVex Offline packaging

Real installers for the Go/HTMX desktop app in `offline-menuvex/`.

## Production / no dummy data

Every release packaging script builds with the Go build tag `production`.
The rich Iranian café dataset is kept only in
`internal/store/seed_fixture.go` with `//go:build !production`, so it is
available to unit tests but is **not compiled into release binaries**.

A newly installed application therefore starts with a genuinely empty local
store. It does not call a seed function, ship a pre-filled database, expose a
`/local/seed-demo` route, or show a "load sample menu" button. The operator
registers/signs in and enters or hydrates the real café data.

The local database remains outside the installation directory and is never
created from a package fixture:

- Linux: `~/.config/CoffishDesktop/coffish-desktop.db`
- Windows: `%APPDATA%\\CoffishDesktop\\coffish-desktop.db`
- macOS: `~/Library/Application Support/CoffishDesktop/coffish-desktop.db`

| Script | Output | Host requirements |
|--------|--------|-------------------|
| `linux/build-deb.sh [amd64|arm64]` | `dist/menuvex-offline_<version>_<arch>.deb` | Go, `dpkg-deb` ≥ 1.19 |
| `linux/build-tarball.sh [amd64|arm64]` | `dist/MenuVex-Offline-<version>-linux-<arch>.tar.gz` | Go, tar |
| `appimage/build-appimage.sh [x86_64|aarch64]` | `dist/MenuVex-Offline-<version>-<arch>.AppImage` | Go, `appimagetool`, `mksquashfs`, optional `RUNTIME_FILE` for offline builds |
| `windows/build-nsis.sh` | `dist/MenuVex-Offline-Setup-<version>-x64.exe` | Go, `makensis` (`MAKENSIS=`) |
| `macos/build-dmg.sh [arm64|amd64]` | `dist/MenuVex-Offline-macOS-<arch>.dmg` | macOS: Go + Xcode CLT + `hdiutil`/`codesign`; Linux cross-packaging needs `newfs_hfs`, `hdutil`, `dmg` |

Common environment variables:

- `VERSION` — defaults to `packaging/VERSION`.
- `APPIMAGETOOL` / `RUNTIME_FILE` — AppImage tooling; `RUNTIME_FILE` allows fully offline AppImage packaging.
- `PYTHON_BIN` — auto-detected as `python3`, then `python`.

All release scripts use `-tags production`. Do not remove that flag when
building a customer installer.

## Validation

The scripts validate the package/container structure:

- `.deb`: control fields, architecture, installed file list.
- `.exe`: PE32 NSIS stub, GUI subsystem, x86-64 payload, and embedded payload verification.
- `.AppImage`: ELF + `AI\\x02` magic, squashfs superblock, payload architecture, and extract mode.
- `.dmg`: UDIF `koly` trailer, data-fork bounds, `Info.plist`, and round-trip checks on Linux.

`packaging/verify-artifacts.sh <dir>` re-validates checksums and the available
package headers. It does not claim that a Windows/macOS GUI was executed on a
Linux host.

The same scripts are used by `packaging/ci/offline-installers.yml`, which runs
on native Ubuntu, Windows, and macOS GitHub runners. The workflow file is kept
outside `.github/workflows/` because this integration cannot modify workflow
files without the repository owner's `workflows` permission; see
`docs/offline/installers.md`.
