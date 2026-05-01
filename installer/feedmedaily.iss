#define MyAppName "FeedMeDaily"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "FeedMeDaily"
#define MyAppURL "https://example.com/feedmedaily"
#define MyAppExeName "FeedMeDaily.exe"
#define MyBuildDir "..\dist\FeedMeDaily"
#define MyIconFile "..\assets\branding\feedmedaily.ico"

[Setup]
AppId={{2F589BEE-CCAA-4ED0-9EB3-1A3D63FA447C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
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
OutputBaseFilename=FeedMeDaily-Setup
Compression=lzma
SolidCompression=yes
WizardStyle=modern
SetupIconFile={#MyIconFile}
UninstallDisplayIcon={app}\{#MyAppExeName}

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"

[Files]
Source: "{#MyBuildDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "open"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "open"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "open"; Description: "Launch {#MyAppName}"; Flags: nowait postinstall skipifsilent
