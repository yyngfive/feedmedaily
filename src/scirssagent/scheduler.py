from __future__ import annotations

import json
import subprocess

from scirssagent.runtime import SCHEDULER_TASK_NAME, RuntimeMode


def scheduler_command(settings) -> dict[str, str]:
    if settings.mode == RuntimeMode.RELEASE.value:
        executable = str(settings.launch_command_path)
        arguments = "run --once"
    else:
        executable = "powershell.exe"
        arguments = (
            f"-NoProfile -ExecutionPolicy Bypass -Command "
            f"\"cd '{settings.root}'; uv run scirssagent run --once\""
        )
    return {
        "execute": executable,
        "arguments": arguments,
        "display": f"{executable} {arguments}".strip(),
    }


def scheduler_status(settings) -> dict[str, object]:
    command = scheduler_command(settings)
    display_command = _escape_ps(command["display"])
    script = f"""
$task = Get-ScheduledTask -TaskName '{SCHEDULER_TASK_NAME}' -ErrorAction SilentlyContinue
if ($null -eq $task) {{
  [pscustomobject]@{{
    installed = $false
    task_name = '{SCHEDULER_TASK_NAME}'
    mode = '{settings.mode}'
    command = '{display_command}'
  }} | ConvertTo-Json -Compress
  exit 0
}}
$info = $task | Get-ScheduledTaskInfo
$trigger = $task.Triggers | Select-Object -First 1
$action = $task.Actions | Select-Object -First 1
function Format-Time($value) {{
  if ($null -eq $value -or $value.Year -lt 1901) {{ return $null }}
  return $value.ToString('o')
}}
if ($trigger -and $trigger.StartBoundary) {{
  $scheduledTime = ([datetime]$trigger.StartBoundary).ToString('HH:mm')
}} else {{
  $scheduledTime = $null
}}
if ($action) {{
  $taskCommand = "$($action.Execute) $($action.Arguments)".Trim()
}} else {{
  $taskCommand = '{display_command}'
}}
[pscustomobject]@{{
  installed = $true
  task_name = $task.TaskName
  mode = '{settings.mode}'
  scheduled_time = $scheduledTime
  state = [string]$task.State
  next_run_time = Format-Time $info.NextRunTime
  last_run_time = Format-Time $info.LastRunTime
  last_result = [int64]$info.LastTaskResult
  command = $taskCommand
}} | ConvertTo-Json -Compress
"""
    completed = _run_powershell(script)
    if completed.returncode != 0:
        print(
            f"Error checking scheduler status: {completed.stderr.strip() or completed.stdout.strip()}"
        )
        return {
            "installed": False,
            "task_name": SCHEDULER_TASK_NAME,
            "mode": settings.mode,
            "scheduled_time": None,
            "state": completed.stderr.strip()
            or completed.stdout.strip()
            or "PowerShell failed",
            "command": command["display"],
            "next_run_time": None,
            "last_run_time": None,
            "last_result": None,
        }

    stdout = completed.stdout.strip()
    if not stdout:
        return {
            "installed": False,
            "task_name": SCHEDULER_TASK_NAME,
            "mode": settings.mode,
            "scheduled_time": None,
            "command": command["display"],
            "state": completed.stderr.strip()
            or completed.stdout.strip()
            or "PowerShell returned empty output",
            "next_run_time": None,
            "last_run_time": None,
            "last_result": None,
        }
    return json.loads(completed.stdout)


def install_scheduler_task(settings, daily_time: str) -> dict[str, object]:
    command = scheduler_command(settings)
    execute = _escape_ps(command["execute"])
    arguments = _escape_ps(command["arguments"])
    script = f"""
$action = New-ScheduledTaskAction -Execute '{execute}' -Argument '{arguments}'
$trigger = New-ScheduledTaskTrigger -Daily -At '{daily_time}'
Register-ScheduledTask `
  -TaskName '{SCHEDULER_TASK_NAME}' `
  -Action $action `
  -Trigger $trigger `
  -Force | Out-Null
"""
    _run_powershell(script)
    return scheduler_status(settings)


def remove_scheduler_task() -> None:
    script = f"""
$task = Get-ScheduledTask -TaskName '{SCHEDULER_TASK_NAME}' -ErrorAction SilentlyContinue
if ($null -ne $task) {{
  Unregister-ScheduledTask -TaskName '{SCHEDULER_TASK_NAME}' -Confirm:$false
}}
"""
    _run_powershell(script)


def _run_powershell(script: str) -> subprocess.CompletedProcess[str]:
    completed = subprocess.run(
        [
            "powershell.exe",
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-Command",
            script,
        ],
        check=False,
        capture_output=True,
        encoding="utf-8",
        errors="replace",
        text=True,
    )
    return completed


def _escape_ps(value: str) -> str:
    return value.replace("'", "''")
