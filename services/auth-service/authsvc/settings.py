"""Django settings for auth-service.

Every value comes from authsvc.config, which reads and validates environment
variables. There are no per-environment settings files: staging and prod
differ only in the environment they inject.
"""

import os

from authsvc import config

CONFIG = config.load(os.environ)

DEBUG = CONFIG.debug
SECRET_KEY = CONFIG.secret_key
ALLOWED_HOSTS = CONFIG.allowed_hosts

# Minimal on purpose. auth, sessions and the database arrive in v1 with the
# client-credentials flow; adding them now would add a dependency that
# readiness cannot yet check.
INSTALLED_APPS = [
    "core",
]

# Order matters, outermost first:
#   RequestID  - every later log line, including errors, carries the ID
#   AccessLog  - one line per request, with the final status code
#   Probes     - answers /healthz and /readyz BEFORE host validation (see
#                core/middleware.py: Kubernetes probes use the pod IP as Host)
#   Security   - HTTPS redirects and security headers
#   Common     - URL normalisation and host validation
MIDDLEWARE = [
    "core.middleware.RequestIDMiddleware",
    "core.middleware.AccessLogMiddleware",
    "core.middleware.ProbeMiddleware",
    "django.middleware.security.SecurityMiddleware",
    "django.middleware.common.CommonMiddleware",
]

ROOT_URLCONF = "authsvc.urls"
WSGI_APPLICATION = "authsvc.wsgi.application"

# No database until v1.
DATABASES = {}

# TLS terminates at the load balancer (ALB, Azure gateway, HAProxy). Trust its
# X-Forwarded-Proto header so Django knows the original request was HTTPS.
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")
SECURE_CONTENT_TYPE_NOSNIFF = True
X_FRAME_OPTIONS = "DENY"

USE_TZ = True
TIME_ZONE = "UTC"

LOGGING = {
    "version": 1,
    "disable_existing_loggers": False,
    "formatters": {"json": {"()": "authsvc.jsonlog.JSONFormatter"}},
    "handlers": {"stdout": {"class": "logging.StreamHandler", "formatter": "json"}},
    "root": {"handlers": ["stdout"], "level": CONFIG.log_level},
    "loggers": {
        # Django's own per-request warnings duplicate our access log.
        "django.request": {"level": "ERROR"},
        "django.server": {"level": "ERROR"},
        # Bots probe with random Host headers. The access log already records
        # the 400; a full stack trace per probe would flood the logs.
        "django.security.DisallowedHost": {"level": "CRITICAL"},
    },
}
