#ifndef SourceExe
  #error SourceExe must be provided by build-portable-installer.ps1
#endif
#ifndef SetupIcon
  #error SetupIcon must be provided by build-portable-installer.ps1
#endif
#ifndef OutputDir
  #error OutputDir must be provided by build-portable-installer.ps1
#endif
#ifndef MyAppVersion
  #define MyAppVersion "0.1.0"
#endif

[Setup]
AppId={{D48A1D5B-598A-4DAE-80E6-E13020DAE21A}
AppName=Compasso
AppVersion={#MyAppVersion}
AppPublisher=Compasso
DefaultDirName={autopf}\Compasso
DefaultGroupName=Compasso
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename=CompassoSetup
SetupIconFile={#SetupIcon}
UninstallDisplayIcon={app}\Compasso-{#MyAppVersion}.ico
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
UsePreviousAppDir=yes
UsePreviousTasks=yes
SetupLogging=yes
VersionInfoVersion={#MyAppVersion}
VersionInfoCompany=Compasso
VersionInfoDescription=Instalador do Compasso
VersionInfoProductName=Compasso
VersionInfoProductVersion={#MyAppVersion}

[Languages]
Name: "brazilianportuguese"; MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"

[Tasks]
Name: "desktopicon"; Description: "Criar um ícone na área de trabalho"; GroupDescription: "Atalhos adicionais:"; Flags: unchecked

[InstallDelete]
Type: filesandordirs; Name: "{app}\*"
Type: filesandordirs; Name: "{commonprograms}\Compasso"

[UninstallDelete]
Type: filesandordirs; Name: "{app}"

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "Compasso.exe"; Flags: ignoreversion
Source: "{#SetupIcon}"; DestDir: "{app}"; DestName: "Compasso-{#MyAppVersion}.ico"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Compasso"; Filename: "{app}\Compasso.exe"; WorkingDir: "{app}"; Comment: "Compasso"; IconFilename: "{app}\Compasso-{#MyAppVersion}.ico"
Name: "{autodesktop}\Compasso"; Filename: "{app}\Compasso.exe"; WorkingDir: "{app}"; Comment: "Compasso"; IconFilename: "{app}\Compasso-{#MyAppVersion}.ico"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Compasso"; Flags: deletekey

[Run]
Filename: "{app}\Compasso.exe"; Description: "Abrir o Compasso"; Flags: nowait postinstall skipifsilent
