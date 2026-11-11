"""Middleware shared by every request.

Same idea as Django's built-in middleware: each class wraps the next layer
(`get_response`) and can act before and after it.
"""

from __future__ import annotations

import logging
import re
import secrets
import time

from django.http import HttpRequest, HttpResponse, JsonResponse

from authsvc.jsonlog import request_id_var
from core.probes import run_readiness_checks

REQUEST_ID_HEADER = "X-Request-ID"
_VALID_REQUEST_ID = re.compile(r"^[A-Za-z0-9._-]{1,128}$")

access_log = logging.getLogger("access")

PROBE_PATHS = ("/healthz", "/readyz")


class RequestIDMiddleware:
    """Give every request an ID and return it in the response.

    A valid incoming X-Request-ID is reused, so one ID follows a request across
    services. Anything else is replaced: the ID ends up in logs and headers, so
    accepting arbitrary client input would allow log injection and log bloat.
    """

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request: HttpRequest) -> HttpResponse:
        incoming = request.headers.get(REQUEST_ID_HEADER, "")
        request_id = incoming if _VALID_REQUEST_ID.match(incoming) else secrets.token_hex(16)
        request.request_id = request_id
        token = request_id_var.set(request_id)
        try:
            response = self.get_response(request)
        finally:
            request_id_var.reset(token)
        response[REQUEST_ID_HEADER] = request_id
        return response


class AccessLogMiddleware:
    """Write one structured log line per request.

    Probe requests are logged at DEBUG: Kubernetes calls them every few seconds
    per pod, and logging them at INFO buries real traffic and inflates the log
    bill.
    """

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request: HttpRequest) -> HttpResponse:
        start = time.perf_counter()
        response = self.get_response(request)
        duration_ms = round((time.perf_counter() - start) * 1000, 3)

        level = logging.DEBUG if request.path in PROBE_PATHS else logging.INFO
        access_log.log(
            level,
            "request",
            extra={
                "method": request.method,
                "path": request.path,
                "status": response.status_code,
                "duration_ms": duration_ms,
                "remote_addr": request.META.get("REMOTE_ADDR", ""),
            },
        )
        return response


class ProbeMiddleware:
    """Answer /healthz and /readyz before the rest of Django runs.

    Why middleware and not a normal view: Kubernetes probes connect to the pod
    IP, so the Host header is something like "10.0.3.17:8080". Django's host
    validation (ALLOWED_HOSTS, inside CommonMiddleware) would reject that with
    400 and the pod would never become ready. Answering here, before
    CommonMiddleware, avoids it without opening ALLOWED_HOSTS to "*".
    It also keeps probes cheap: no URL resolution, no further middleware.
    """

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request: HttpRequest) -> HttpResponse:
        if request.path not in PROBE_PATHS:
            return self.get_response(request)

        if request.method not in ("GET", "HEAD"):
            response = HttpResponse("method not allowed\n", status=405, content_type="text/plain")
            response["Allow"] = "GET, HEAD"
        elif request.path == "/healthz":
            response = HttpResponse("ok\n", content_type="text/plain")
        else:
            ready, checks = run_readiness_checks()
            response = JsonResponse({"status": "ready" if ready else "not ready", "checks": checks},
                                    status=200 if ready else 503)
        response["Cache-Control"] = "no-store"
        return response
