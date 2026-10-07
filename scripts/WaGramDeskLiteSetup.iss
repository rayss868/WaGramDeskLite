; WaGramDeskLite installer script
; Build: ISCC.exe /O"dist" /F"WaGramDeskLiteSetup" scripts\WaGramDeskLiteSetup.iss

[Setup]
AppId={{8F6A9E2C-4B1E-4F3A-9C6D-WaGramDeskLite}
AppName=WaGramDeskLite
AppVersion=1.2.3
; AppVersion only fills the display version. The Setup.exe's own resource comes
; from these; without them Explorer reports FileVersion 0.0.0.0 on the installer.
VersionInfoVersion=1.2.3.0
VersionInfoTextVersion=1.2.3
VersionInfoProductName=WaGram Desk Lite
VersionInfoProductVersion=1.2.3.0
VersionInfoProductTextVersion=1.2.3
AppPublisher=WaGramDeskLite
AppContact=https://github.com/rayss868/WaGramDeskLite
DefaultDirName={localappdata}\Programs\WaGramDeskLite
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=dist
OutputBaseFilename=WaGramDeskLiteSetup
Compression=lzma
SolidCompression=yes
CloseApplications=yes
Uninstallable=yes
UninstallDisplayIcon={app}\icon.ico
SetupIconFile=..\assets\icon.ico

[Files]
Source: "..\dist\WaGramDeskLite.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\assets\icon.ico"; DestDir: "{app}"; Flags: ignoreversion

[InstallDelete]
Type: files; Name: "{autoprograms}\WaGramDeskLite.lnk"
Type: files; Name: "{autodesktop}\WaGramDeskLite.lnk"

[Icons]
Name: "{autoprograms}\WhatsApp"; Filename: "{app}\WaGramDeskLite.exe"; IconFilename: "{app}\icon.ico"; WorkingDir: "{app}"
Name: "{autodesktop}\WhatsApp"; Filename: "{app}\WaGramDeskLite.exe"; IconFilename: "{app}\icon.ico"; WorkingDir: "{app}"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"

[Run]
Filename: "{app}\WaGramDeskLite.exe"; Description: "Launch WaGram Desk Lite"; Flags: nowait postinstall skipifsilent