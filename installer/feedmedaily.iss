#define MyAppName "FeedMeDaily"
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

#define MyAppVersion AppVersion
#define MyAppPublisher "FeedMeDaily"
#define MyAppURL "https://example.com/feedmedaily"
#define MyTrayExeName "FeedMeDailyTray.exe"
#define MyDaemonExeName "feedmedailyd.exe"
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

[InstallDelete]
Type: filesandordirs; Name: "{app}\_internal"
Type: filesandordirs; Name: "{app}\web"
Type: files; Name: "{app}\FeedMeDailyTray.exe"
Type: files; Name: "{app}\feedmedailyd.exe"
Type: files; Name: "{app}\feedmedaily.ico"

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"

[Files]
Source: "{#MyBuildDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyTrayExeName}"; Parameters: "--root ""{app}"""
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyTrayExeName}"; Parameters: "--root ""{app}"""; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyTrayExeName}"; Parameters: "--root ""{app}"""; Description: "Launch {#MyAppName} tray"; Flags: nowait postinstall skipifsilent
