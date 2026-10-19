# MenuVex Offline packaging

Real installers for the Go/HTMX desktop app in `offline-menuvex/`.

| Script | Output | Host requirements |
|--------|--------|-------------------|
| `linux/build-deb.sh [amd64\|arm64]` | `dist/menuvex-offline_<version>_<arch>.deb` | Go, `dpkg-deb` ≥ 1.19 |
| `linux/build-tarball.sh [amd64\|arm64]` | `dist/MenuVex-Offline-<version>-linux-<arch>.tar.gz` | Go, tar |
| `appimage/build-appimage.sh [x86_64\|aarch64]` | `dist/MenuVex-Offline-<version>-<arch>.AppImage` | Go, `appimagetool`, `mksquashfs`, optional `RUNTIME_FILE` for offline builds |
| `windows/build-nsis.sh` | `dist/MenuVex-Offline-Setup-<version>-x64.exe` | Go, `makensis` (`MAKENSIS=`), `NSISDIR=` when using a Linux-built makensis |
| `macos/build-dmg.sh [arm64\|amd64]` | `dist/MenuVex-Offline-macOS-<arch>.dmg` | macOS: Go + Xcode CLT + `hdiutil`/`codesign`. Linux: Go + `newfs_hfs` + `hdutil` + `dmg` (`NEWFS_HFS=`, `HDUTIL=`, `DMG_TOOL=`) |

Common environment variables:

- `VERSION` — defaults to `packaging/VERSION`.
- `APPIMAGETOOL` / `RUNTIME_FILE` — AppImage tooling; `RUNTIME_FILE` lets a build run
  fully offline with the standard type-2 runtime.
- `PYTHON_BIN` is auto-detected (`python3`, then `python`).

Validation performed by the scripts:

- `.deb`: control fields, architecture, installed file list.
- `.exe`: PE32 NSIS stub, GUI subsystem, x86-64 GUI payload, and a verification build
  proving the payload bytes are embedded verbatim.
- `.AppImage`: ELF + `AI\x02` magic, squashfs superblock at the report offset, payload
  architecture, `--appimage-extract` (or `unsquashfs -o` for foreign architectures).
- `.dmg`: UDIF `koly` trailer, data-fork bounds, Info.plist keys, and (on Linux) a full
  DMG → HFS+ → file round-trip compared with `cmp`.

The same scripts run unchanged in `packaging/ci/offline-installers.yml`
(Ubuntu 22.04, Windows 2022, macOS 14 / 15 Intel), which also performs real
install/upgrade/uninstall tests on those runners. See `docs/offline/installers.md`
for the results and the current GitHub permissions blocker.
