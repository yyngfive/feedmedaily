from __future__ import annotations

import base64
import json
import os
from pathlib import Path

if os.name == "nt":
    from ctypes import (
        POINTER,
        Structure,
        byref,
        c_char,
        c_wchar_p,
        cast,
        create_string_buffer,
        windll,
    )
    from ctypes.wintypes import DWORD

    class DATA_BLOB(Structure):
        _fields_ = [("cbData", DWORD), ("pbData", POINTER(c_char))]


def load_secret_values(path: Path) -> dict[str, str]:
    if not path.exists():
        return {}
    payload = json.loads(path.read_text(encoding="utf-8"))
    scheme = str(payload.get("scheme") or "base64")
    values = payload.get("values") or {}
    return {key: _decode_secret(str(value), scheme) for key, value in values.items()}


def store_secret_values(path: Path, values: dict[str, str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    scheme = "dpapi" if os.name == "nt" else "base64"
    payload = {
        "scheme": scheme,
        "values": {key: _encode_secret(value, scheme) for key, value in values.items()},
    }
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


def _encode_secret(value: str, scheme: str) -> str:
    raw = value.encode("utf-8")
    if scheme == "dpapi":
        return base64.b64encode(_crypt_protect(raw)).decode("ascii")
    return base64.b64encode(raw).decode("ascii")


def _decode_secret(value: str, scheme: str) -> str:
    raw = base64.b64decode(value.encode("ascii"))
    if scheme == "dpapi":
        return _crypt_unprotect(raw).decode("utf-8")
    return raw.decode("utf-8")


def _crypt_protect(data: bytes) -> bytes:
    if os.name != "nt":
        return data
    buffer = create_string_buffer(data, len(data))
    blob_in = DATA_BLOB(len(data), cast(buffer, POINTER(c_char)))
    blob_out = DATA_BLOB()
    if not windll.crypt32.CryptProtectData(
        byref(blob_in),
        c_wchar_p("FeedMeDaily"),
        None,
        None,
        None,
        0,
        byref(blob_out),
    ):
        raise OSError("Could not encrypt secret with Windows DPAPI.")
    try:
        return cast(blob_out.pbData, POINTER(c_char * blob_out.cbData)).contents.raw
    finally:
        windll.kernel32.LocalFree(blob_out.pbData)


def _crypt_unprotect(data: bytes) -> bytes:
    if os.name != "nt":
        return data
    buffer = create_string_buffer(data, len(data))
    blob_in = DATA_BLOB(len(data), cast(buffer, POINTER(c_char)))
    blob_out = DATA_BLOB()
    description = c_wchar_p()
    if not windll.crypt32.CryptUnprotectData(
        byref(blob_in),
        byref(description),
        None,
        None,
        None,
        0,
        byref(blob_out),
    ):
        raise OSError("Could not decrypt secret with Windows DPAPI.")
    try:
        return cast(blob_out.pbData, POINTER(c_char * blob_out.cbData)).contents.raw
    finally:
        windll.kernel32.LocalFree(blob_out.pbData)
