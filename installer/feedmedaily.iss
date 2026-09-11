#define MyAppName "FeedMeDaily"
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

#define MyAppVersion AppVersion
#define MyAppPublisher "FeedMeDaily"
#define MyAppURL "https://github.com/yyngfive/feedmedaily/releases/latest"
#define MyTrayExeName "FeedMeDailyTray.exe"
#define MyDaemonExeName "feedmedailyd.exe"
#define MyShutdownExeName "FeedMeDailyShutdown.exe"
#define MyBuildDir "..\dist\FeedMeDaily"
#define MyIconFile "..\assets\branding\feedmedaily.ico"

[Setup]
AppId={{2F589BEE-CCAA-4ED0-9EB3-1A3D63FA447C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
VersionInfoVersion={#MyAppVersion}
VersionInfoTextVersion={#MyAppVersion}
OutputBaseFilename=FeedMeDaily-v{#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableDirPage=no
DisableProgramGroupPage=yes
AllowNoIcons=yes
OutputDir=..\dist\installer
Compression=lzma
SolidCompression=yes
WizardStyle=modern
SetupIconFile={#MyIconFile}
UninstallDisplayIcon={app}\{#MyTrayExeName}
CloseApplications=yes
RestartApplications=no

[Code]
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ShutdownExe: String;
  DataRoot: String;
  Parameters: String;
  ResultCode: Integer;
begin
  Result := '';
  NeedsRestart := False;
  ShutdownExe := ExpandConstant('{app}\{#MyTrayExeName}');
  if not FileExists(ShutdownExe) then
    Exit;

  DataRoot := ExpandConstant('{param:FEEDMEDAILY_DATA_ROOT|}');
  if DataRoot = '' then
    DataRoot := ExpandConstant('{%FEEDMEDAILY_DATA_ROOT|{localappdata}\FeedMeDaily}');

  ExtractTemporaryFile('{#MyShutdownExeName}');
  ShutdownExe := ExpandConstant('{tmp}\{#MyShutdownExeName}');
  Parameters := '--shutdown --root "' + ExpandConstant('{app}') +
    '" --data-root "' + DataRoot + '"';
  if not Exec(ShutdownExe, Parameters, ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := 'FeedMeDaily could not be prepared for update. Please close FeedMeDaily manually and try again.';
    Exit;
  end;
  if ResultCode <> 0 then
    Result := 'FeedMeDaily is still running. Please close FeedMeDaily manually and try again.';
end;

[InstallDelete]
Type: filesandordirs; Name: "{app}\_internal"
Type: filesandordirs; Name: "{app}\web"
Type: files; Name: "{app}\FeedMeDaily.exe"
Type: files; Name: "{app}\FeedMeDailyTray.exe"
Type: files; Name: "{app}\feedmedailyd.exe"
Type: files; Name: "{app}\FeedMeDailyVerifier.exe"
Type: filesandordirs; Name: "{app}\FeedMeDailyACSVerifier"
Type: filesandordirs; Name: "{app}\FeedMeDailyProtectedVerifier"
Type: files; Name: "{app}\feedmedaily.ico"
Type: files; Name: "{app}\tray-settings.json"

[UninstallDelete]
Type: files; Name: "{app}\FeedMeDaily.exe"
Type: files; Name: "{app}\tray-settings.json"

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"

[Files]
Source: "{#MyBuildDir}\FeedMeDailyTray.exe"; DestDir: "{tmp}"; DestName: "{#MyShutdownExeName}"; Flags: dontcopy
Source: "{#MyBuildDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyTrayExeName}"; Parameters: "--root ""{app}"""
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyTrayExeName}"; Parameters: "--root ""{app}"""; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyTrayExeName}"; Parameters: "--root ""{app}"""; Description: "Launch {#MyAppName} tray"; Flags: nowait postinstall skipifsilent
