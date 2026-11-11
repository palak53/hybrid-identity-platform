"""Gunicorn settings: the production server for auth-service.

`python manage.py runserver` is for development only. Gunicorn runs a master
process that manages several worker processes, restarts crashed workers, and
shuts down gracefully on SIGTERM.

Values come from the same validated config as Django, so a bad value fails
before any worker starts.
"""

import os

# Underscore name: Gunicorn treats top-level names in this file as settings,
# and "config" is one of its own setting names.
from authsvc import config as _appconfig

_cfg = _appconfig.load(os.environ)

# Listen on all interfaces; inside a container that is the pod IP.
bind = f"0.0.0.0:{_cfg.port}"

# gthread: each worker process runs a few threads. Auth requests mostly wait
# on I/O (database, Redis), so threads give more concurrency per MB than extra
# processes. On Kubernetes, scale by adding PODS (HPA), not by raising
# workers: keep workers x threads matched to the pod's CPU request.
worker_class = "gthread"
workers = _cfg.workers
threads = _cfg.threads

# A worker silent for this long is killed and replaced (stuck request guard).
timeout = _cfg.request_timeout

# On SIGTERM, workers stop accepting new connections and get this long to
# finish in-flight requests. preStop sleep + graceful_timeout must stay below
# the pod's terminationGracePeriodSeconds (30s by default).
graceful_timeout = _cfg.graceful_timeout

# Keep-alive must be LONGER than the load balancer's idle timeout (AWS ALB
# defaults to 60s). If the backend closes first, the LB can reuse a closed
# connection and the client sees a random 502.
keepalive = _cfg.keepalive

# Protection against slow clients is the load balancer's job (ALB, Azure
# gateway, HAProxy buffer requests). Never expose Gunicorn directly.
limit_request_line = 4094
limit_request_fields = 100

# Our Django middleware writes the structured access log; Gunicorn's own
# access log would duplicate it.
accesslog = None
errorlog = "-"
loglevel = _cfg.log_level.lower()

# Heartbeat files in memory, not on a (possibly slow or read-only) disk.
worker_tmp_dir = "/dev/shm" if os.path.isdir("/dev/shm") else None

# Gunicorn's own messages (startup, worker restarts, shutdown) as JSON too, so
# every line on stdout has the same shape for the log pipeline.
logconfig_dict = {
    "version": 1,
    "disable_existing_loggers": False,
    "formatters": {"json": {"()": "authsvc.jsonlog.JSONFormatter"}},
    "handlers": {
        "console": {"class": "logging.StreamHandler", "formatter": "json", "stream": "ext://sys.stdout"},
    },
    "root": {"level": _cfg.log_level, "handlers": ["console"]},
    "loggers": {
        "gunicorn.error": {"level": _cfg.log_level, "handlers": ["console"], "propagate": False},
        # Silenced: the Django AccessLogMiddleware already logs each request,
        # with the request ID that Gunicorn does not know about.
        "gunicorn.access": {"level": "CRITICAL", "handlers": ["console"], "propagate": False},
    },
}
