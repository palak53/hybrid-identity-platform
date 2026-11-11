"""Structured JSON logging with the current request ID attached.

One JSON object per line on stdout: Kubernetes, systemd/journald and CloudWatch
all collect stdout, and Datadog/Loki parse JSON without custom rules.
"""

from __future__ import annotations

import contextvars
import json
import logging
from datetime import datetime, timezone

# Holds the request ID for the request being handled on this thread. A
# ContextVar (not a global) keeps concurrent requests on gthread workers apart.
request_id_var: contextvars.ContextVar[str] = contextvars.ContextVar("request_id", default="")


def _install_record_factory() -> None:
    """Stamp the current request ID onto every log record WHEN IT IS CREATED.

    Reading the ContextVar later, at formatting time, is wrong: handlers can
    format records after the request has finished or on another thread.
    """
    previous = logging.getLogRecordFactory()
    if getattr(previous, "_adds_request_id", False):
        return  # already installed (module re-imported)

    def factory(*args, **kwargs):
        record = previous(*args, **kwargs)
        record.request_id = request_id_var.get()
        return record

    factory._adds_request_id = True
    logging.setLogRecordFactory(factory)


_install_record_factory()

# Attributes every LogRecord has. Anything else was passed via extra={...}
# and is copied into the JSON output.
# Built from the plain LogRecord class, not the factory above, so request_id
# is NOT treated as a standard attribute.
_STANDARD_ATTRS = set(vars(logging.LogRecord("", 0, "", 0, "", (), None))) | {"message", "asctime", "taskName"}


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        entry = {
            "time": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(timespec="milliseconds"),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
        }
        for key, value in vars(record).items():
            if key == "request_id" and not value:
                continue  # log lines outside a request have no ID
            if key not in _STANDARD_ATTRS and not key.startswith("_"):
                entry[key] = value
        if record.exc_info:
            entry["exception"] = self.formatException(record.exc_info)
        return json.dumps(entry, default=str)
