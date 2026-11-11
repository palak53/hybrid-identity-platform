"""Load and validate auth-service configuration from environment variables.

Why environment variables: Kubernetes injects ConfigMaps and Secrets as env
vars, systemd on the on-prem VMs does the same with EnvironmentFile, and so does
an EC2 launch template. One mechanism works on every runtime we target.

Why fail fast: an invalid value stops the process at startup with a clear
message. A pod that crashes on deploy is caught by the rollout; a bad value
discovered at 3 a.m. under load is an incident.

This module has no Django imports, so it is used by both settings.py and
gunicorn.conf.py, and is easy to unit test with a fake environment.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping

LOG_LEVELS = ("DEBUG", "INFO", "WARNING", "ERROR")
_TRUE = {"1", "true", "yes", "on"}
_FALSE = {"0", "false", "no", "off"}


class ConfigError(Exception):
    """Raised when one or more settings are invalid. Lists every problem."""

    def __init__(self, problems: list[str]):
        self.problems = problems
        super().__init__("invalid configuration:\n  - " + "\n  - ".join(problems))


@dataclass(frozen=True)
class Config:
    debug: bool
    secret_key: str
    allowed_hosts: list[str]
    log_level: str
    port: int
    workers: int
    threads: int
    graceful_timeout: int
    request_timeout: int
    keepalive: int


def load(env: Mapping[str, str]) -> Config:
    """Build a Config from an environment mapping (pass os.environ)."""
    problems: list[str] = []

    debug = _bool(env, "DEBUG", False, problems)

    secret_key = env.get("DJANGO_SECRET_KEY", "")
    if not secret_key:
        if debug:
            # Local development only. Never valid outside DEBUG mode.
            secret_key = "insecure-dev-only-key"
        else:
            problems.append("DJANGO_SECRET_KEY is required when DEBUG is off")
    elif len(secret_key) < 32:
        problems.append("DJANGO_SECRET_KEY must be at least 32 characters")

    allowed_hosts = [h.strip() for h in env.get("ALLOWED_HOSTS", "").split(",") if h.strip()]
    if not allowed_hosts:
        if debug:
            allowed_hosts = ["localhost", "127.0.0.1", "[::1]"]
        else:
            problems.append("ALLOWED_HOSTS is required when DEBUG is off (comma-separated)")

    log_level = env.get("LOG_LEVEL", "INFO").upper()
    if log_level not in LOG_LEVELS:
        problems.append(f"LOG_LEVEL must be one of {', '.join(LOG_LEVELS)}, got {log_level!r}")

    port = _int(env, "PORT", 8080, 1, 65535, problems)
    workers = _int(env, "WEB_CONCURRENCY", 2, 1, 64, problems)
    threads = _int(env, "GUNICORN_THREADS", 4, 1, 64, problems)
    graceful_timeout = _int(env, "GRACEFUL_TIMEOUT", 20, 1, 300, problems)
    request_timeout = _int(env, "REQUEST_TIMEOUT", 30, 1, 300, problems)
    keepalive = _int(env, "KEEPALIVE", 75, 1, 300, problems)

    if problems:
        raise ConfigError(problems)

    return Config(
        debug=debug,
        secret_key=secret_key,
        allowed_hosts=allowed_hosts,
        log_level=log_level,
        port=port,
        workers=workers,
        threads=threads,
        graceful_timeout=graceful_timeout,
        request_timeout=request_timeout,
        keepalive=keepalive,
    )


def _bool(env: Mapping[str, str], key: str, default: bool, problems: list[str]) -> bool:
    raw = env.get(key)
    if raw is None or raw == "":
        return default
    value = raw.strip().lower()
    if value in _TRUE:
        return True
    if value in _FALSE:
        return False
    problems.append(f"{key} must be true or false, got {raw!r}")
    return default


def _int(env: Mapping[str, str], key: str, default: int, lo: int, hi: int, problems: list[str]) -> int:
    raw = env.get(key)
    if raw is None or raw == "":
        return default
    try:
        value = int(raw)
    except ValueError:
        problems.append(f"{key} must be a whole number between {lo} and {hi}, got {raw!r}")
        return default
    if not lo <= value <= hi:
        problems.append(f"{key} must be between {lo} and {hi}, got {value}")
        return default
    return value
