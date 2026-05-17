from __future__ import annotations

import pathlib


def test_python_package_is_not_required_for_default_repo_workflows() -> None:
    pyproject = pathlib.Path("pyproject.toml").read_text(encoding="utf-8")
    assert "package = false" in pyproject
    assert '[project.scripts]' not in pyproject
