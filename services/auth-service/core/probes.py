"""Liveness and readiness logic.

Liveness (/healthz): "is this process able to serve HTTP?" It must NEVER check
dependencies. If it checked Postgres, one database blip would fail liveness on
every pod at once and Kubernetes would restart them all, turning a small
problem into a full outage.

Readiness (/readyz): "should this instance receive traffic right now?" It
checks the dependencies the service cannot work without. A failing readiness
probe takes the instance out of the load balancer but does not restart it.

Each check is a function returning (ok, detail). v1 adds a Postgres check and
v2 a Redis check by appending to READINESS_CHECKS.
"""

from __future__ import annotations

from typing import Callable

Check = Callable[[], "tuple[bool, str]"]

READINESS_CHECKS: dict[str, Check] = {}


def run_readiness_checks() -> tuple[bool, dict[str, str]]:
    """Run every readiness check. A check that raises counts as failed."""
    results: dict[str, str] = {}
    all_ok = True
    for name, check in READINESS_CHECKS.items():
        try:
            ok, detail = check()
        except Exception as exc:  # a broken check must not crash the probe
            ok, detail = False, f"check raised {type(exc).__name__}"
        results[name] = detail
        all_ok = all_ok and ok
    return all_ok, results
