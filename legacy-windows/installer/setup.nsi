Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinVer.nsh"
!include "x64.nsh"
!ifndef ARCH
 !error "Define ARCH=x86 or x64"
!endif
!ifndef BINARY
 !error "Define BINARY path"
!endif
!ifndef OUTPUT
 !error "Define OUTPUT installer path"
!endif
Name "MenuVex Printer Agent Legacy (test candidate)"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\MenuVexPrinterLegacy"
RequestExecutionLevel user
SetCompressor /SOLID lzma
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\menuvex-printer-legacy.exe"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
Function CheckRunning
 System::Call 'kernel32::OpenMutexW(i 0x100000, i 0, w "Local\MenuVexPrinterLegacy.SingleInstance") p.r0'
 ${If} $0 != 0
  System::Call 'kernel32::CloseHandle(p r0)'
  MessageBox MB_OK|MB_ICONEXCLAMATION "Quit MenuVex Legacy from its system tray menu before installing. Do not interrupt active prints."
  Abort
 ${EndIf}
FunctionEnd
Function .onInit
 ${IfNot} ${AtLeastWin7}
  MessageBox MB_OK|MB_ICONSTOP "Requires Windows 7 SP1 or later. Windows XP/Vista are not supported."
  Abort
 ${EndIf}
 ${If} ${IsWin7}
  ${IfNot} ${AtLeastServicePack} 1
   MessageBox MB_OK|MB_ICONSTOP "Install Windows 7 Service Pack 1 before using this Agent."
   Abort
  ${EndIf}
 ${EndIf}
 !if "${ARCH}" == "x64"
 ${IfNot} ${RunningX64}
  MessageBox MB_OK|MB_ICONSTOP "This is the 64-bit installer. Download the x86 installer for 32-bit Windows."
  Abort
 ${EndIf}
 !endif
 Call CheckRunning
FunctionEnd
Section "Install"
 SetShellVarContext current
 SetOutPath "$INSTDIR"
 File "${BINARY}"
 File "..\..\src-tauri\assets\OFL-NotoSansArabic.txt"
 File "..\THIRD-PARTY-NOTICES.md"
 File "${LICENSES}"
 WriteUninstaller "$INSTDIR\Uninstall.exe"
 CreateDirectory "$SMPROGRAMS\MenuVex Printer Agent Legacy"
 CreateShortcut "$SMPROGRAMS\MenuVex Printer Agent Legacy\MenuVex Printer Agent Legacy.lnk" "$INSTDIR\menuvex-printer-legacy.exe"
 CreateShortcut "$SMPROGRAMS\MenuVex Printer Agent Legacy\Uninstall.lnk" "$INSTDIR\Uninstall.exe"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexPrinterLegacy" "DisplayName" "MenuVex Printer Agent Legacy"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexPrinterLegacy" "DisplayVersion" "1.0.0"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexPrinterLegacy" "UninstallString" '"$INSTDIR\Uninstall.exe"'
 WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexPrinterLegacy" "NoModify" 1
 WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexPrinterLegacy" "NoRepair" 1
SectionEnd
Function un.onInit
 System::Call 'kernel32::OpenMutexW(i 0x100000, i 0, w "Local\MenuVexPrinterLegacy.SingleInstance") p.r0'
 ${If} $0 != 0
  System::Call 'kernel32::CloseHandle(p r0)'
  MessageBox MB_OK|MB_ICONEXCLAMATION "Quit MenuVex Legacy from the tray before uninstalling."
  Abort
 ${EndIf}
FunctionEnd
Section "Uninstall"
 SetShellVarContext current
 DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "MenuVexPrinterLegacy"
 DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\MenuVexPrinterLegacy"
 Delete "$SMPROGRAMS\MenuVex Printer Agent Legacy\MenuVex Printer Agent Legacy.lnk"
 Delete "$SMPROGRAMS\MenuVex Printer Agent Legacy\Uninstall.lnk"
 RMDir "$SMPROGRAMS\MenuVex Printer Agent Legacy"
 Delete "$INSTDIR\menuvex-printer-legacy.exe"
 Delete "$INSTDIR\OFL-NotoSansArabic.txt"
 Delete "$INSTDIR\THIRD-PARTY-NOTICES.md"
 Delete "$INSTDIR\THIRD-PARTY-LICENSES.txt"
 Delete "$INSTDIR\Uninstall.exe"
 RMDir "$INSTDIR"
 ; Preserve receipt DB and Credential Manager entry to avoid losing dedup history on reinstall.
SectionEnd
