"""URL routes. /healthz and /readyz are answered by core.middleware.ProbeMiddleware
before routing, so they are not listed here. OIDC endpoints arrive in v1."""

urlpatterns = []
