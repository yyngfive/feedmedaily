from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
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
    path.write_text(
        json.dumps(
            {"pid": pid, "port": port, "started_at": started_at},
            ensure_ascii=False,
            indent=2,
        ),
        encoding="utf-8",
    )


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
        creationflags = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0) | getattr(
            subprocess,
            "DETACHED_PROCESS",
            0,
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
