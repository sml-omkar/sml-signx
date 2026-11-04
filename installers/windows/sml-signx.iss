#define AppName "SML SignX"
#define AppVersion "0.1.0"
#define AppPublisher "SML"

[Setup]
AppId={{8A6C1E42-6F4B-4B7E-9C2A-4E6D1F8A9B31}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={autopf}\SML SignX
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=..\..\dist
OutputBaseFilename=SML-SignX-Setup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
PrivilegesRequired=admin
UninstallDisplayIcon={app}\sml-signx.exe
UninstallDisplayName={#AppName}

[Files]
Source: "..\..\dist\sml-signx-windows-amd64.exe"; DestDir: "{app}"; DestName: "sml-signx.exe"; Flags: ignoreversion

[Icons]
Name: "{group}\SML SignX Status"; Filename: "http://localhost:7070/op/status"
Name: "{group}\SML SignX Data Folder"; Filename: "{userappdata}\SMLSignX"
Name: "{group}\Uninstall {#AppName}"; Filename: "{uninstallexe}"

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "SMLSignX"; ValueData: """{app}\sml-signx.exe"""; Flags: uninsdeletevalue

[Run]
Filename: "{app}\sml-signx.exe"; Description: "Start SML SignX now"; Flags: nowait postinstall skipifsilent
