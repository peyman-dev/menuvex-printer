; Inno Setup script for the Novex Printer Agent (Windows installer).
; Build: place NovexPrinterAgent.exe next to this file, open in Inno Setup, Compile.
; Result: dist/NovexPrinterAgent-Setup-1.0.0.exe (user-level install, no admin).

#define MyAppName "Novex Printer Agent"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "MenuVex"
#define MyAppExeName "NovexPrinterAgent.exe"

[Setup]
AppId={{9E8C7B6A-5D4C-3F2E-1A0B-9D8C7B6A5D4C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={localappdata}\NovexPrinterAgent
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename=NovexPrinterAgent-Setup-{#MyAppVersion}
Compression=lzma
SolidCompression=yes
UninstallDisplayIcon={app}\{#MyAppExeName}

[Files]
Source: "NovexPrinterAgent.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"

[Run]
; Register start-on-login via the agent itself (writes the Startup .vbs).
Filename: "{app}\{#MyAppExeName}"; Parameters: "--install-autostart"; Flags: runhidden; Description: "Start Novex Printer Agent on login"
; Launch the agent right after install.
Filename: "{app}\{#MyAppExeName}"; Description: "Launch {#MyAppName} now"; Flags: nowait postinstall skipifsilent runhidden

[UninstallRun]
Filename: "{app}\{#MyAppExeName}"; Parameters: "--uninstall-autostart"; Flags: runhidden
