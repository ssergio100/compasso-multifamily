#ifndef AppVersion
  #define AppVersion "0.1.0-pilot"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.1.0.0"
#endif
#ifndef BinaryDir
  #define BinaryDir "..\..\dist\windows"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif
#ifndef LicensePath
  #define LicensePath "..\..\LICENSE"
#endif

[Setup]
AppId={{A4B721BD-01AE-4F0C-A17D-EAF1B7C2CC56}
AppName=Compasso Agent
AppVersion={#AppVersion}
AppPublisher=Compasso
DefaultDirName={autopf}\Compasso\Agent
DefaultGroupName=Compasso
DisableProgramGroupPage=yes
DisableDirPage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.22000
PrivilegesRequired=admin
WizardStyle=modern
OutputDir={#OutputDir}
OutputBaseFilename=CompassoAgent-{#AppVersion}-windows-x64
Compression=lzma2/max
SolidCompression=yes
SetupLogging=yes
Uninstallable=yes
UninstallDisplayName=Compasso Agent
VersionInfoVersion={#NumericVersion}
CloseApplications=no
RestartApplications=no
RestartIfNeededByRun=no
LicenseFile={#LicensePath}

[Languages]
Name: "brazilianportuguese"; MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"

[Files]
Source: "service-manager.ps1"; DestName: "compasso-service-manager.tmp.ps1"; Flags: dontcopy noencryption
Source: "{#BinaryDir}\compasso-windows-service.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinaryDir}\compasso-windows-companion.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "service-manager.ps1"; DestDir: "{app}"; Flags: ignoreversion

[Code]
var
  ExistingConfiguration: Boolean;
  PreservePage: TInputOptionWizardPage;
  AccountPage: TInputQueryWizardPage;
  EnrollmentPage: TInputQueryWizardPage;
  ControlledSID: String;
  PreserveDataOnUninstall: Boolean;

function StateDirectory: String;
begin
  Result := ExpandConstant('{commonappdata}\Compasso\Agent');
end;

function ConfigurationPath: String;
begin
  Result := AddBackslash(StateDirectory) + 'config.toml';
end;

function PowerShellQuote(Value: String): String;
begin
  StringChangeEx(Value, '''', '''''', True);
  Result := '''' + Value + '''';
end;

function TomlQuote(Value: String): String;
begin
  StringChangeEx(Value, '\', '\\', True);
  StringChangeEx(Value, '"', '\"', True);
  Result := '"' + Value + '"';
end;

function ResolveAccountSID(AccountName: String; var SID: String): Boolean;
var
  ResultCode: Integer;
  OutputPath: String;
  Command: String;
  RawSID: AnsiString;
begin
  Result := False;
  SID := '';
  OutputPath := ExpandConstant('{tmp}\compasso-account-sid.txt');
  DeleteFile(OutputPath);
  Command :=
    '$ErrorActionPreference=''Stop''; ' +
    '$account=New-Object System.Security.Principal.NTAccount(' + PowerShellQuote(AccountName) + '); ' +
    '$sid=$account.Translate([System.Security.Principal.SecurityIdentifier]).Value; ' +
    '[IO.File]::WriteAllText(' + PowerShellQuote(OutputPath) + ',$sid)';

  if Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
      '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "' + Command + '"',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and
      (ResultCode = 0) and LoadStringFromFile(OutputPath, RawSID) then
  begin
    SID := Trim(String(RawSID));
    Result := Pos('S-1-', SID) = 1;
  end;
  DeleteFile(OutputPath);
end;

procedure InitializeWizard;
begin
  ExistingConfiguration := FileExists(ConfigurationPath);

  PreservePage := CreateInputOptionPage(wpWelcome,
    'Configuração existente',
    'Escolha como tratar a configuração atual.',
    'O banco e a configuração são preservados automaticamente durante atualizações.',
    True, False);
  PreservePage.Add('Manter a configuração e os dados existentes');
  PreservePage.Add('Substituir a configuração (o banco será preservado)');
  PreservePage.SelectedValueIndex := 0;

  AccountPage := CreateInputQueryPage(PreservePage.ID,
    'Usuário controlado',
    'Informe a conta Windows que receberá as políticas.',
    'Use o nome local, por exemplo Sergio, ou COMPUTADOR\Sergio. A conta deve existir nesta máquina.');
  AccountPage.Add('Conta Windows:', False);

  EnrollmentPage := CreateInputQueryPage(AccountPage.ID,
    'Conexão com o Compasso',
    'Informe as credenciais geradas para este dispositivo.',
    'O token será armazenado com acesso restrito a SYSTEM e Administradores.');
  EnrollmentPage.Add('Servidor:', False);
  EnrollmentPage.Add('ID do dispositivo:', False);
  EnrollmentPage.Add('Token do dispositivo:', True);
  EnrollmentPage.Values[0] := 'https://apifamily.smresume.com';
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := False;
  if (PageID = PreservePage.ID) and not ExistingConfiguration then
    Result := True
  else if ((PageID = AccountPage.ID) or (PageID = EnrollmentPage.ID)) and
      ExistingConfiguration and (PreservePage.SelectedValueIndex = 0) then
    Result := True;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  ServerURL: String;
begin
  Result := True;
  if CurPageID = AccountPage.ID then
  begin
    AccountPage.Values[0] := Trim(AccountPage.Values[0]);
    if (AccountPage.Values[0] = '') or
        not ResolveAccountSID(AccountPage.Values[0], ControlledSID) then
    begin
      MsgBox('A conta Windows não existe ou não pôde ser identificada.', mbError, MB_OK);
      Result := False;
    end;
  end
  else if CurPageID = EnrollmentPage.ID then
  begin
    ServerURL := Trim(EnrollmentPage.Values[0]);
    EnrollmentPage.Values[0] := ServerURL;
    EnrollmentPage.Values[1] := Trim(EnrollmentPage.Values[1]);
    EnrollmentPage.Values[2] := Trim(EnrollmentPage.Values[2]);
    if Pos('https://', Lowercase(ServerURL)) <> 1 then
    begin
      MsgBox('O servidor deve usar HTTPS.', mbError, MB_OK);
      Result := False;
    end
    else if (EnrollmentPage.Values[1] = '') or (EnrollmentPage.Values[2] = '') then
    begin
      MsgBox('Informe o ID e o token do dispositivo.', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Result := '';
  ExtractTemporaryFile('compasso-service-manager.tmp.ps1');
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
      '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' +
      ExpandConstant('{tmp}\compasso-service-manager.tmp.ps1') + '" -Action Stop',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
    Result := 'Não foi possível parar a versão anterior do serviço Compasso.';
end;

procedure WriteConfiguration;
var
  Lines: TArrayOfString;
begin
  if ExistingConfiguration and (PreservePage.SelectedValueIndex = 0) then
    Exit;

  if not ForceDirectories(StateDirectory) then
    RaiseException('Não foi possível criar o diretório de dados do Compasso.');

  SetArrayLength(Lines, 7);
  Lines[0] := 'controlled_user = ' + TomlQuote(ControlledSID);
  Lines[1] := 'tick_interval = "1s"';
  Lines[2] := 'checkpoint_interval = "5s"';
  Lines[3] := 'server_url = ' + TomlQuote(EnrollmentPage.Values[0]);
  Lines[4] := 'device_id = ' + TomlQuote(EnrollmentPage.Values[1]);
  Lines[5] := 'device_token = ' + TomlQuote(EnrollmentPage.Values[2]);
  Lines[6] := 'http_timeout = "8s"';

  if not SaveStringsToUTF8FileWithoutBOM(ConfigurationPath, Lines, False) then
    RaiseException('Não foi possível gravar a configuração do Compasso.');
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep <> ssPostInstall then
    Exit;

  WriteConfiguration;
  WizardForm.StatusLabel.Caption := 'Registrando e iniciando o serviço Compasso...';
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
      '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' +
      ExpandConstant('{app}\service-manager.ps1') + '" -Action Install -ExecutablePath "' +
      ExpandConstant('{app}\compasso-windows-service.exe') + '" -StateDirectory "' +
      StateDirectory + '"',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
    RaiseException('O serviço Compasso não pôde ser instalado ou iniciado.');
end;

function HasCommandLineParameter(Name: String): Boolean;
var
  Index: Integer;
begin
  Result := False;
  for Index := 1 to ParamCount do
    if CompareText(ParamStr(Index), Name) = 0 then
    begin
      Result := True;
      Exit;
    end;
end;

function InitializeUninstall: Boolean;
var
  ResultCode: Integer;
begin
  if HasCommandLineParameter('/PURGEDATA') then
    PreserveDataOnUninstall := False
  else
    PreserveDataOnUninstall :=
      SuppressibleMsgBox(
        'Deseja preservar a configuração e o histórico local para uma futura reinstalação?',
        mbConfirmation, MB_YESNO, IDYES) = IDYES;
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' +
    ExpandConstant('{app}\service-manager.ps1') + '" -Action Uninstall',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0);
  if not Result then
    MsgBox('Não foi possível parar e remover o serviço Compasso. A desinstalação foi cancelada.',
      mbError, MB_OK);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if (CurUninstallStep = usPostUninstall) and not PreserveDataOnUninstall then
  begin
    if not DelTree(StateDirectory, True, True, True) then
      MsgBox('Não foi possível remover completamente os dados em ' + StateDirectory + '.',
        mbError, MB_OK)
    else
      RemoveDir(ExtractFileDir(StateDirectory));
  end;
end;
