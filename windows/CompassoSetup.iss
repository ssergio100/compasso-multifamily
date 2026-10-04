#ifndef SourceExe
  #error SourceExe must be provided by build-portable-installer.ps1
#endif
#ifndef SetupIcon
  #error SetupIcon must be provided by build-portable-installer.ps1
#endif
#ifndef SourceAgent
  #error SourceAgent must be provided by build-portable-installer.ps1
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
Type: filesandordirs; Name: "{commonprograms}\Compasso"

[UninstallDelete]
Type: filesandordirs; Name: "{app}"
Type: filesandordirs; Name: "{commonappdata}\Compasso"

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "Compasso.exe"; Flags: ignoreversion
Source: "{#SourceAgent}"; DestDir: "{app}"; DestName: "CompassoAgent.exe"; Flags: ignoreversion restartreplace uninsrestartdelete
Source: "{#SetupIcon}"; DestDir: "{app}"; DestName: "Compasso-{#MyAppVersion}.ico"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Compasso"; Filename: "{app}\Compasso.exe"; WorkingDir: "{app}"; Comment: "Compasso"; IconFilename: "{app}\Compasso-{#MyAppVersion}.ico"
Name: "{autodesktop}\Compasso"; Filename: "{app}\Compasso.exe"; WorkingDir: "{app}"; Comment: "Compasso"; IconFilename: "{app}\Compasso-{#MyAppVersion}.ico"; Tasks: desktopicon

[Registry]
Root: HKLM; Subkey: "SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Compasso"; Flags: deletekey

[Run]
Filename: "{app}\CompassoAgent.exe"; Parameters: "install"; StatusMsg: "Registrando o serviço do Compasso..."; Flags: runhidden waituntilterminated
Filename: "{app}\CompassoAgent.exe"; Parameters: "start-if-configured"; StatusMsg: "Iniciando o serviço do Compasso..."; Flags: runhidden waituntilterminated
Filename: "{app}\Compasso.exe"; Parameters: "--settings"; Description: "Configurar o Compasso"; Flags: nowait postinstall skipifsilent runasoriginaluser

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/F /T /IM Compasso.exe"; RunOnceId: "CloseCompassoUI"; Flags: runhidden waituntilterminated
Filename: "{app}\CompassoAgent.exe"; Parameters: "uninstall"; RunOnceId: "RemoveCompassoAgentService"; Flags: runhidden waituntilterminated skipifdoesntexist

[Code]
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  AgentPath: String;
  ResultCode: Integer;
begin
  Result := '';
  { Restart Manager cannot close the Wails process when Setup and the app are
    attached to different interactive sessions. Close it explicitly before any
    executable is replaced; exit code 128 simply means it was not running. }
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM Compasso.exe', '',
    SW_HIDE, ewWaitUntilTerminated, ResultCode);
  AgentPath := ExpandConstant('{app}\CompassoAgent.exe');
  if FileExists(AgentPath) then
    Exec(AgentPath, 'stop', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;
