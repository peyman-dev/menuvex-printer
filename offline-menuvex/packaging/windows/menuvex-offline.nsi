; MenuVex Offline — Windows installer (NSIS 3.x, Unicode)
;
; Build inputs (all passed with -D from packaging/windows/build-nsis.sh):
;   VERSION        product version, e.g. 1.0.0
;   APP_EXE        path to the built GUI executable (windows/amd64)
;   APP_ICON       path to icon.ico
;   OUT_FILE       output installer path
;   APP_SIZE_KB    installed size in KiB (Add/Remove Programs)
;
; Data safety: the database, license and uploads live in
; %APPDATA%\CoffishDesktop (os.UserConfigDir), never in $INSTDIR, so both
; upgrade and uninstall keep the operator's data. Uninstall never deletes it.

Unicode true

!include "MUI2.nsh"
!include "x64.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !define VERSION "1.0.0"
!endif
!ifndef APP_EXE
  !error "APP_EXE define is required"
!endif
!ifndef OUT_FILE
  !error "OUT_FILE define is required"
!endif
!ifndef APP_SIZE_KB
  !define APP_SIZE_KB "20000"
!endif

Name "MenuVex Offline"
OutFile "${OUT_FILE}"
InstallDir "$PROGRAMFILES64\MenuVex Offline"
InstallDirRegKey HKLM "Software\MenuVex\Offline" "InstallDir"
RequestExecutionLevel admin
!ifdef UNCOMPRESSED_PAYLOAD
  ; Verification build only (see build-nsis.sh): keep the payload byte-exact.
  SetCompressor zlib
!else
  SetCompressor /SOLID lzma
  SetCompressorDictSize 32
!endif
BrandingText "MenuVex Offline ${VERSION}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "MenuVex Offline"
VIAddVersionKey /LANG=1033 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "FileDescription" "MenuVex Offline installer"
VIAddVersionKey /LANG=1033 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "CompanyName" "MenuVex"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Copyright (c) MenuVex"

!define MUI_ABORTWARNING
!define MUI_ICON "${APP_ICON}"
!define MUI_UNICON "${APP_ICON}"
!define MUI_FINISHPAGE_RUN "$INSTDIR\MenuVexOffline.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Run MenuVex Offline"
!define MUI_FINISHPAGE_LINK "menuvex.ir"
!define MUI_FINISHPAGE_LINK_LOCATION "https://menuvex.ir"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Farsi"

; Stop a running instance so files can be replaced during an upgrade.
Function CloseRunningApp
  DetailPrint "Closing running MenuVex Offline (if any)..."
  nsExec::Exec '"$SYSDIR\taskkill.exe" /IM MenuVexOffline.exe /F'
  Pop $0
  Sleep 700
FunctionEnd

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "MenuVex Offline requires 64-bit Windows."
    Abort
  ${EndIf}
  SetRegView 64
FunctionEnd

Section "MenuVex Offline" SecMain
  SectionIn RO
  Call CloseRunningApp

  SetOutPath "$INSTDIR"
  SetOverwrite on
!ifdef UNCOMPRESSED_PAYLOAD
  ; Verification build only: embeds the payload verbatim so the build script
  ; can prove that the exact executable is inside the installer.
  SetCompress off
!endif
  File "/oname=MenuVexOffline.exe" "${APP_EXE}"

  ; Static assets are embedded in the executable: no data files are installed,
  ; so the application has no dependency on its working directory.
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  CreateDirectory "$SMPROGRAMS\MenuVex Offline"
  CreateShortcut "$SMPROGRAMS\MenuVex Offline\MenuVex Offline.lnk" "$INSTDIR\MenuVexOffline.exe" "" "$INSTDIR\MenuVexOffline.exe" 0
  CreateShortcut "$SMPROGRAMS\MenuVex Offline\Uninstall MenuVex Offline.lnk" "$INSTDIR\Uninstall.exe"
  CreateShortcut "$DESKTOP\MenuVex Offline.lnk" "$INSTDIR\MenuVexOffline.exe" "" "$INSTDIR\MenuVexOffline.exe" 0

  WriteRegStr HKLM "Software\MenuVex\Offline" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\MenuVex\Offline" "Version" "${VERSION}"

  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "DisplayName" "MenuVex Offline"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "Publisher" "MenuVex"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "DisplayIcon" "$INSTDIR\MenuVexOffline.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "NoRepair" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline" "EstimatedSize" ${APP_SIZE_KB}

  ; Refresh the shell icon/association caches.
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, i 0, i 0)'
SectionEnd

Section "Uninstall"
  Call un.CloseRunningApp

  Delete "$SMPROGRAMS\MenuVex Offline\MenuVex Offline.lnk"
  Delete "$SMPROGRAMS\MenuVex Offline\Uninstall MenuVex Offline.lnk"
  RMDir "$SMPROGRAMS\MenuVex Offline"
  Delete "$DESKTOP\MenuVex Offline.lnk"

  ; The program files are removed, the operator's data is not:
  ; %APPDATA%\CoffishDesktop (offline.sqlite, license, uploads) stays intact.
  Delete "$INSTDIR\MenuVexOffline.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexOffline"
  DeleteRegKey HKLM "Software\MenuVex\Offline"
  DeleteRegKey /ifempty HKLM "Software\MenuVex"

  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, i 0, i 0)'
SectionEnd

Function un.CloseRunningApp
  nsExec::Exec '"$SYSDIR\taskkill.exe" /IM MenuVexOffline.exe /F'
  Pop $0
  Sleep 700
FunctionEnd

Function un.onInit
  SetRegView 64
FunctionEnd
