from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path

from dotenv import dotenv_values, set_key, unset_key

from scirssagent.runtime import (
    APP_PUBLIC_NAME,
    RuntimeMode,
    default_user_data_dir,
    detect_runtime_mode,
    resolve_app_root,
    resolve_web_dist_dir,
)
from scirssagent.secure_store import load_secret_values, store_secret_values


DEFAULT_UPDATE_MANIFEST_URL = (
    "https://github.com/yyngfive/feedmedaily/releases/latest/download/update.json"
)


@dataclass(frozen=True)
class Settings:
    mode: str
    root: Path
    app_dir: Path
    user_data_dir: Path
    config_dir: Path
    settings_store_path: Path | None
    secrets_store_path: Path | None
    runtime_state_path: Path
    web_dist_dir: Path
    feeds_path: Path
    data_dir: Path
    reports_dir: Path
    logs_dir: Path
    database_path: Path
    profile_path: Path
    launch_command_path: Path
    update_manifest_url: str | None
    classifier_api_key: str | None
    classifier_base_url: str
    classifier_model: str
    classifier_thinking: str
    classifier_batch_size: int
    profile_api_key: str | None
    profile_base_url: str
    profile_model: str
    profile_thinking: str
    zotero_api_key: str | None
    zotero_library_type: str
    zotero_library_id: str | None
    zotero_collection_key: str | None
    server_host: str
    server_port: int


@dataclass(frozen=True)
class ConfigOption:
    key: str
    label: str
    description: str
    section: str
    input_type: str
    default: str | None = None
    secret: bool = False
    options: tuple[tuple[str, str], ...] = ()


@dataclass(frozen=True)
class ResolvedConfigValue:
    option: ConfigOption
    value: str | None
    source: str
    stored_in_dotenv: bool
    configured: bool
    storage_label: str | None = None


CONFIG_OPTIONS: tuple[ConfigOption, ...] = (
    ConfigOption(
        key="SCIRSS_CLASSIFIER_API_KEY",
        label="Classifier API key",
        description="Used only for paper classification requests.",
        section="Classifier model",
        input_type="password",
        secret=True,
    ),
    ConfigOption(
        key="SCIRSS_CLASSIFIER_BASE_URL",
        label="Classifier base URL",
        description="Base URL for the classifier model provider.",
        section="Classifier model",
        input_type="url",
        default="https://api.deepseek.com",
    ),
    ConfigOption(
        key="SCIRSS_CLASSIFIER_MODEL",
        label="Classifier model",
        description="Model name used for paper classification.",
        section="Classifier model",
        input_type="text",
        default="deepseek-v4-flash",
    ),
    ConfigOption(
        key="SCIRSS_CLASSIFIER_THINKING",
        label="Classifier thinking",
        description="Whether the classifier role requests provider reasoning mode.",
        section="Classifier model",
        input_type="select",
        default="disabled",
        options=(("disabled", "Disabled"), ("enabled", "Enabled")),
    ),
    ConfigOption(
        key="SCIRSS_CLASSIFIER_BATCH_SIZE",
        label="Classifier batch size",
        description="How many papers are sent to the classifier per batch.",
        section="Classifier model",
        input_type="number",
        default="10",
    ),
    ConfigOption(
        key="SCIRSS_PROFILE_API_KEY",
        label="Profile API key",
        description="Used for onboarding, profile generation, and profile revision.",
        section="Profile model",
        input_type="password",
        secret=True,
    ),
    ConfigOption(
        key="SCIRSS_PROFILE_BASE_URL",
        label="Profile base URL",
        description="Base URL for the profile-generation model provider.",
        section="Profile model",
        input_type="url",
        default="https://api.deepseek.com",
    ),
    ConfigOption(
        key="SCIRSS_PROFILE_MODEL",
        label="Profile model",
        description="Model name used for initial and feedback-driven profile proposals.",
        section="Profile model",
        input_type="text",
        default="deepseek-v4-pro",
    ),
    ConfigOption(
        key="SCIRSS_PROFILE_THINKING",
        label="Profile thinking",
        description="Whether the profile role requests provider reasoning mode.",
        section="Profile model",
        input_type="select",
        default="enabled",
        options=(("disabled", "Disabled"), ("enabled", "Enabled")),
    ),
    ConfigOption(
        key="SCIRSS_PROFILE_PATH",
        label="Profile file path",
        description="Path for the active classification profile file.",
        section="Local files",
        input_type="text",
    ),
    ConfigOption(
        key="SCIRSS_ZOTERO_API_KEY",
        label="Zotero API key",
        description="Used for Zotero collection lookup and paper save operations.",
        section="Zotero",
        input_type="password",
        secret=True,
    ),
    ConfigOption(
        key="SCIRSS_ZOTERO_LIBRARY_TYPE",
        label="Zotero library type",
        description="Select whether Zotero saves target a personal or group library.",
        section="Zotero",
        input_type="select",
        default="user",
        options=(("user", "User"), ("group", "Group")),
    ),
    ConfigOption(
        key="SCIRSS_ZOTERO_LIBRARY_ID",
        label="Zotero library ID",
        description="User ID for personal libraries or group ID for group libraries.",
        section="Zotero",
        input_type="text",
    ),
    ConfigOption(
        key="SCIRSS_ZOTERO_COLLECTION_KEY",
        label="Default Zotero collection key",
        description="Optional default collection for Save to Zotero.",
        section="Zotero",
        input_type="text",
    ),
    ConfigOption(
        key="SCIRSS_SERVER_HOST",
        label="Server host",
        description="Host interface for the local backend service.",
        section="Local app",
        input_type="text",
        default="127.0.0.1",
    ),
    ConfigOption(
        key="SCIRSS_SERVER_PORT",
        label="Server port",
        description="Preferred port for the local backend service.",
        section="Local app",
        input_type="number",
        default="8000",
    ),
    ConfigOption(
        key="FEEDMEDAILY_UPDATE_MANIFEST_URL",
        label="Update manifest URL",
        description="Remote JSON manifest used for in-app update checks.",
        section="Release",
        input_type="url",
        default=DEFAULT_UPDATE_MANIFEST_URL,
    ),
)


CONFIG_OPTIONS_BY_KEY = {item.key: item for item in CONFIG_OPTIONS}


def project_env_path(root: Path | None = None) -> Path:
    return (resolve_app_root(root) / ".env").resolve()


def release_settings_path(root: Path | None = None) -> Path:
    return (default_user_data_dir() / "config" / "settings.json").resolve()


def release_secrets_path(root: Path | None = None) -> Path:
    return (default_user_data_dir() / "config" / "secrets.json").resolve()


def local_env_values(root: Path | None = None) -> dict[str, str]:
    env_path = project_env_path(root)
    if not env_path.exists():
        return {}
    parsed = dotenv_values(env_path)
    return {key: str(value) for key, value in parsed.items() if value is not None}


def local_release_values(root: Path | None = None) -> tuple[dict[str, str], dict[str, str]]:
    settings_path = release_settings_path(root)
    secrets_path = release_secrets_path(root)
    settings_values: dict[str, str] = {}
    if settings_path.exists():
        payload = json.loads(settings_path.read_text(encoding="utf-8"))
        settings_values = {
            str(key): str(value)
            for key, value in (payload.get("values") or {}).items()
            if value is not None
        }
    secret_values = load_secret_values(secrets_path) if secrets_path.exists() else {}
    return settings_values, secret_values


def resolved_config_values(root: Path | None = None) -> list[ResolvedConfigValue]:
    app_root = resolve_app_root(root)
    mode = detect_runtime_mode()
    dotenv_items = local_env_values(app_root) if mode == RuntimeMode.SOURCE else {}
    release_items, release_secret_items = (
        local_release_values(app_root) if mode == RuntimeMode.RELEASE else ({}, {})
    )
    resolved: list[ResolvedConfigValue] = []
    for option in CONFIG_OPTIONS:
        default_value = _default_value_for_option(option, app_root, mode)
        if option.key in os.environ:
            value = os.environ[option.key]
            source = "environment"
            storage_label = "System environment"
            stored_locally = (
                option.key in dotenv_items
                or option.key in release_items
                or option.key in release_secret_items
            )
        elif mode == RuntimeMode.SOURCE and option.key in dotenv_items:
            value = dotenv_items[option.key]
            source = "dotenv"
            storage_label = ".env"
            stored_locally = True
        elif mode == RuntimeMode.RELEASE and option.secret and option.key in release_secret_items:
            value = release_secret_items[option.key]
            source = "secret_store"
            storage_label = f"{APP_PUBLIC_NAME} secure store"
            stored_locally = True
        elif mode == RuntimeMode.RELEASE and option.key in release_items:
            value = release_items[option.key]
            source = "settings"
            storage_label = f"{APP_PUBLIC_NAME} settings.json"
            stored_locally = True
        else:
            value = default_value
            source = "default" if default_value is not None else "unset"
            storage_label = "Built-in default" if default_value is not None else None
            stored_locally = False
        resolved.append(
            ResolvedConfigValue(
                option=option,
                value=value,
                source=source,
                stored_in_dotenv=stored_locally,
                configured=bool(value),
                storage_label=storage_label,
            )
        )
    return resolved


def update_local_settings(
    root: Path | None,
    updates: dict[str, dict[str, object]],
) -> list[ResolvedConfigValue]:
    app_root = resolve_app_root(root)
    mode = detect_runtime_mode()
    if mode == RuntimeMode.RELEASE:
        ordinary_values, secret_values = local_release_values(app_root)
        ordinary_path = release_settings_path(app_root)
        secrets_path = release_secrets_path(app_root)
        for key, raw_update in updates.items():
            option = CONFIG_OPTIONS_BY_KEY.get(key)
            if option is None:
                raise ValueError(f"Unsupported setting: {key}")
            clear = bool(raw_update.get("clear", False))
            raw_value = raw_update.get("value")
            normalized = _normalize_setting_value(
                option,
                None if raw_value is None else str(raw_value),
            )
            if option.secret:
                if normalized is not None:
                    secret_values[key] = normalized
                elif clear:
                    secret_values.pop(key, None)
                continue
            if normalized is None:
                ordinary_values.pop(key, None)
            else:
                ordinary_values[key] = normalized
        _write_release_settings(ordinary_path, ordinary_values)
        store_secret_values(secrets_path, secret_values)
        load_settings(app_root)
        return resolved_config_values(app_root)

    env_path = project_env_path(app_root)
    env_path.parent.mkdir(parents=True, exist_ok=True)
    for key, raw_update in updates.items():
        option = CONFIG_OPTIONS_BY_KEY.get(key)
        if option is None:
            raise ValueError(f"Unsupported setting: {key}")
        clear = bool(raw_update.get("clear", False))
        raw_value = raw_update.get("value")
        normalized = _normalize_setting_value(option, None if raw_value is None else str(raw_value))
        if option.secret:
            if normalized is not None:
                _write_env_key(env_path, key, normalized)
            elif clear:
                _remove_env_key(env_path, key)
            continue
        if normalized is None:
            _remove_env_key(env_path, key)
        else:
            _write_env_key(env_path, key, normalized)
    load_settings(app_root)
    return resolved_config_values(app_root)


def load_settings(root: Path | None = None) -> Settings:
    app_root = resolve_app_root(root)
    mode = detect_runtime_mode()
    if mode == RuntimeMode.RELEASE:
        user_data_dir = default_user_data_dir()
        config_dir = user_data_dir / "config"
        data_dir = user_data_dir / "data"
        reports_dir = user_data_dir / "reports"
        logs_dir = user_data_dir / "logs"
        settings_store_path = release_settings_path(app_root)
        secrets_store_path = release_secrets_path(app_root)
    else:
        user_data_dir = app_root
        config_dir = app_root
        data_dir = app_root / "data"
        reports_dir = app_root / "reports"
        logs_dir = app_root / "logs"
        settings_store_path = None
        secrets_store_path = None

    config_dir.mkdir(parents=True, exist_ok=True)
    data_dir.mkdir(parents=True, exist_ok=True)
    reports_dir.mkdir(parents=True, exist_ok=True)
    logs_dir.mkdir(parents=True, exist_ok=True)

    runtime_state_path = config_dir / "runtime.json"
    web_dist_dir = resolve_web_dist_dir(app_root)
    values = {item.option.key: item.value for item in resolved_config_values(app_root)}
    profile_value = values.get("SCIRSS_PROFILE_PATH") or _default_profile_path(
        mode,
        data_dir,
    )
    profile_path = Path(profile_value)
    if not profile_path.is_absolute():
        profile_path = (app_root / profile_path).resolve()

    return Settings(
        mode=mode.value,
        root=app_root,
        app_dir=app_root,
        user_data_dir=user_data_dir.resolve(),
        config_dir=config_dir.resolve(),
        settings_store_path=settings_store_path.resolve() if settings_store_path else None,
        secrets_store_path=secrets_store_path.resolve() if secrets_store_path else None,
        runtime_state_path=runtime_state_path.resolve(),
        web_dist_dir=web_dist_dir.resolve(),
        feeds_path=(data_dir / "rss_feeds.json").resolve(),
        data_dir=data_dir.resolve(),
        reports_dir=reports_dir.resolve(),
        logs_dir=logs_dir.resolve(),
        database_path=(data_dir / "literature.sqlite").resolve(),
        profile_path=profile_path.resolve(),
        launch_command_path=Path(sys.executable).resolve(),
        update_manifest_url=(
            _optional_value(values.get("FEEDMEDAILY_UPDATE_MANIFEST_URL"))
            or DEFAULT_UPDATE_MANIFEST_URL
        ),
        classifier_api_key=_optional_value(values.get("SCIRSS_CLASSIFIER_API_KEY")),
        classifier_base_url=str(values["SCIRSS_CLASSIFIER_BASE_URL"]),
        classifier_model=str(values["SCIRSS_CLASSIFIER_MODEL"]),
        classifier_thinking=str(values["SCIRSS_CLASSIFIER_THINKING"]).strip().lower(),
        classifier_batch_size=max(1, int(str(values["SCIRSS_CLASSIFIER_BATCH_SIZE"]))),
        profile_api_key=_optional_value(values.get("SCIRSS_PROFILE_API_KEY")),
        profile_base_url=str(values["SCIRSS_PROFILE_BASE_URL"]),
        profile_model=str(values["SCIRSS_PROFILE_MODEL"]),
        profile_thinking=str(values["SCIRSS_PROFILE_THINKING"]).strip().lower(),
        zotero_api_key=_optional_value(values.get("SCIRSS_ZOTERO_API_KEY")),
        zotero_library_type=str(values["SCIRSS_ZOTERO_LIBRARY_TYPE"]).strip().lower(),
        zotero_library_id=_optional_value(values.get("SCIRSS_ZOTERO_LIBRARY_ID")),
        zotero_collection_key=_optional_value(values.get("SCIRSS_ZOTERO_COLLECTION_KEY")),
        server_host=str(values["SCIRSS_SERVER_HOST"]),
        server_port=max(1, int(str(values["SCIRSS_SERVER_PORT"]))),
    )


def _default_value_for_option(
    option: ConfigOption,
    root: Path,
    mode: RuntimeMode,
) -> str | None:
    if option.key == "SCIRSS_PROFILE_PATH":
        data_dir = (
            default_user_data_dir() / "data"
            if mode == RuntimeMode.RELEASE
            else root / "data"
        )
        return _default_profile_path(mode, data_dir)
    return option.default


def _default_profile_path(mode: RuntimeMode, data_dir: Path) -> str:
    if mode == RuntimeMode.RELEASE:
        return str((data_dir / "classification_profile.json").resolve())
    return "data/classification_profile.json"


def _write_release_settings(path: Path, values: dict[str, str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps({"values": values}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def _optional_value(value: str | None) -> str | None:
    if value is None:
        return None
    clean = value.strip()
    return clean or None


def _normalize_setting_value(option: ConfigOption, value: str | None) -> str | None:
    if value is None:
        return None
    clean = value.strip()
    if not clean:
        return None
    if option.input_type == "number":
        numeric = int(clean)
        if numeric < 1:
            raise ValueError(f"{option.label} must be at least 1.")
        return str(numeric)
    if option.input_type == "url":
        if not (clean.startswith("http://") or clean.startswith("https://")):
            raise ValueError(f"{option.label} must start with http:// or https://.")
        return clean
    if option.input_type == "select":
        normalized = clean.lower()
        allowed = {item for item, _label in option.options}
        if normalized not in allowed:
            raise ValueError(f"{option.label} must be one of: {', '.join(sorted(allowed))}.")
        return normalized
    return clean


def _write_env_key(env_path: Path, key: str, value: str) -> None:
    set_key(str(env_path), key, value)


def _remove_env_key(env_path: Path, key: str) -> None:
    if env_path.exists():
        unset_key(str(env_path), key)
