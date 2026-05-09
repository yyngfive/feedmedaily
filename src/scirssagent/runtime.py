from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
import threading
import time
import webbrowser
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path

import httpx

from scirssagent import __version__

APP_PUBLIC_NAME = "FeedMeDaily"
APP_INTERNAL_NAME = "scirssagent"
SCHEDULER_TASK_NAME = f"{APP_PUBLIC_NAME} Daily Sync"


class RuntimeMode(StrEnum):
    SOURCE = "source"
    RELEASE = "release"


@dataclass(frozen=True)
class RuntimeState:
    pid: int | None
    port: int | None
    started_at: str | None = None


class AppOpenTarget(StrEnum):
    DATA_DIR = "data_dir"
    LOGS_DIR = "logs_dir"
    REPORTS_DIR = "reports_dir"
    INSTALL_DIR = "install_dir"
    SERVER_URL = "server_url"
    DOWNLOAD_URL = "download_url"
    RELEASE_NOTES_URL = "release_notes_url"


def package_version() -> str:
    return __version__


def detect_runtime_mode() -> RuntimeMode:
    override = (os.getenv("FEEDMEDAILY_RUNTIME_MODE") or "").strip().lower()
    if override == RuntimeMode.RELEASE.value:
        return RuntimeMode.RELEASE
    if override == RuntimeMode.SOURCE.value:
        return RuntimeMode.SOURCE
    return RuntimeMode.RELEASE if getattr(sys, "frozen", False) else RuntimeMode.SOURCE


def resolve_app_root(root: Path | None = None) -> Path:
    if root is not None:
        return root.resolve()
    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent
    return Path.cwd().resolve()


def default_user_data_dir() -> Path:
    override = os.getenv("FEEDMEDAILY_DATA_ROOT")
    if override:
        return Path(override).expanduser().resolve()
    local_app_data = os.getenv("LOCALAPPDATA")
    if local_app_data:
        return (Path(local_app_data) / APP_PUBLIC_NAME).resolve()
    return (Path.home() / "AppData" / "Local" / APP_PUBLIC_NAME).resolve()


def resolve_web_dist_dir(app_root: Path) -> Path:
    candidates = [
        app_root / "web" / "dist",
        app_root / "dist" / "web",
        app_root / "web_dist",
    ]
    for candidate in candidates:
        if candidate.exists():
            return candidate.resolve()
    return candidates[0].resolve()


def find_available_local_port(host: str, preferred: int) -> int:
    candidates = [preferred] + list(range(preferred + 1, preferred + 20))
    for port in candidates:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                sock.bind((host, port))
            except OSError:
                continue
            return port
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind((host, 0))
        return int(sock.getsockname()[1])


def read_runtime_state(path: Path) -> RuntimeState | None:
    if not path.exists():
        return None
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return RuntimeState(
        pid=int(payload["pid"]) if payload.get("pid") is not None else None,
        port=int(payload["port"]) if payload.get("port") is not None else None,
        started_at=str(payload["started_at"]) if payload.get("started_at") else None,
    )


def write_runtime_state(path: Path, pid: int, port: int, started_at: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(
        {"pid": pid, "port": port, "started_at": started_at},
        ensure_ascii=False,
        indent=2,
    )
    temp_path = path.with_suffix(".tmp")
    temp_path.write_text(payload, encoding="utf-8")
    temp_path.replace(path)


def open_local_path(path: Path) -> None:
    resolved = path.resolve()
    if os.name == "nt":
        os.startfile(str(resolved))  # type: ignore[attr-defined]
        return
    open_browser(resolved.as_uri())


def open_external_target(target: str) -> None:
    if target.startswith("http://") or target.startswith("https://"):
        open_browser(target)
        return
    open_local_path(Path(target))


def schedule_process_exit(runtime_state_path: Path, delay_seconds: float = 0.2) -> None:
    def shutdown() -> None:
        clear_runtime_state(runtime_state_path)
        os._exit(0)

    timer = threading.Timer(delay_seconds, shutdown)
    timer.daemon = True
    timer.start()


def process_is_running(pid: int | None) -> bool:
    if pid is None:
        return False
    try:
        if os.name == "nt":
            completed = subprocess.run(
                ["tasklist", "/FI", f"PID eq {pid}"],
                check=False,
                capture_output=True,
                text=True,
                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
            )
            return str(pid) in completed.stdout
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def clear_runtime_state(path: Path) -> None:
    if path.exists():
        path.unlink()


def wait_for_healthcheck(url: str, timeout_seconds: float = 12.0) -> bool:
    deadline = time.time() + timeout_seconds
    with httpx.Client(timeout=1.0) as client:
        while time.time() < deadline:
            try:
                response = client.get(url)
                if response.is_success:
                    return True
            except httpx.HTTPError:
                pass
            time.sleep(0.25)
    return False


def open_browser(url: str) -> None:
    webbrowser.open(url)


def launch_background_process(command: list[str], cwd: Path) -> subprocess.Popen:
    creationflags = 0
    if os.name == "nt":
        creationflags = (
            getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
            | getattr(subprocess, "DETACHED_PROCESS", 0)
            | getattr(subprocess, "CREATE_NO_WINDOW", 0)
        )
    return subprocess.Popen(
        command,
        cwd=str(cwd),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        stdin=subprocess.DEVNULL,
        close_fds=True,
        creationflags=creationflags,
    )


def parse_version_parts(version: str) -> tuple[int, ...]:
    normalized: list[int] = []
    for part in version.split("."):
        digits = "".join(char for char in part if char.isdigit())
        normalized.append(int(digits or "0"))
    return tuple(normalized)


def is_newer_version(candidate: str, current: str) -> bool:
    return parse_version_parts(candidate) > parse_version_parts(current)
