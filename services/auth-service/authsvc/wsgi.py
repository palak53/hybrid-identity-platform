"""WSGI entry point used by Gunicorn: `gunicorn authsvc.wsgi`."""

import os

from django.core.wsgi import get_wsgi_application

os.environ.setdefault("DJANGO_SETTINGS_MODULE", "authsvc.settings")

application = get_wsgi_application()
