"""Tests for configuration, probes, request IDs and access logging.

Run with: python manage.py test
"""

import json
import logging

from django.test import SimpleTestCase, override_settings

from authsvc import config
from authsvc.jsonlog import JSONFormatter
from core import probes

VALID_KEY = "k" * 40


class ConfigTests(SimpleTestCase):
    def test_debug_defaults(self):
        cfg = config.load({"DEBUG": "true"})
        self.assertTrue(cfg.debug)
        self.assertEqual(cfg.port, 8080)
        self.assertEqual(cfg.log_level, "INFO")
        self.assertIn("localhost", cfg.allowed_hosts)

    def test_production_values(self):
        cfg = config.load({
            "DJANGO_SECRET_KEY": VALID_KEY,
            "ALLOWED_HOSTS": "login.example.com, auth.internal",
            "PORT": "9090",
            "LOG_LEVEL": "debug",
            "WEB_CONCURRENCY": "3",
        })
        self.assertFalse(cfg.debug)
        self.assertEqual(cfg.allowed_hosts, ["login.example.com", "auth.internal"])
        self.assertEqual((cfg.port, cfg.log_level, cfg.workers), (9090, "DEBUG", 3))

    def test_invalid_values(self):
        cases = [
            ({"DEBUG": "true", "PORT": "abc"}, "PORT"),
            ({"DEBUG": "true", "PORT": "70000"}, "PORT"),
            ({"DEBUG": "maybe"}, "DEBUG"),
            ({"DEBUG": "true", "LOG_LEVEL": "loud"}, "LOG_LEVEL"),
            ({"DEBUG": "true", "WEB_CONCURRENCY": "0"}, "WEB_CONCURRENCY"),
            ({"ALLOWED_HOSTS": "x"}, "DJANGO_SECRET_KEY is required"),
            ({"DJANGO_SECRET_KEY": "short", "ALLOWED_HOSTS": "x"}, "at least 32"),
            ({"DJANGO_SECRET_KEY": VALID_KEY}, "ALLOWED_HOSTS"),
        ]
        for env, expected in cases:
            with self.subTest(env=env):
                with self.assertRaises(config.ConfigError) as ctx:
                    config.load(env)
                self.assertIn(expected, str(ctx.exception))

    def test_reports_all_problems_at_once(self):
        with self.assertRaises(config.ConfigError) as ctx:
            config.load({"PORT": "abc", "LOG_LEVEL": "loud"})
        message = str(ctx.exception)
        for expected in ("PORT", "LOG_LEVEL", "DJANGO_SECRET_KEY", "ALLOWED_HOSTS"):
            self.assertIn(expected, message)


class ProbeTests(SimpleTestCase):
    def tearDown(self):
        probes.READINESS_CHECKS.clear()

    def test_healthz(self):
        response = self.client.get("/healthz")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.content, b"ok\n")
        self.assertEqual(response["Cache-Control"], "no-store")

    def test_probes_accept_pod_ip_host(self):
        # Kubernetes probes use the pod IP as Host, which is not in ALLOWED_HOSTS.
        for path in ("/healthz", "/readyz"):
            with self.subTest(path=path):
                response = self.client.get(path, HTTP_HOST="10.0.3.17:8080")
                self.assertEqual(response.status_code, 200)

    @override_settings(ALLOWED_HOSTS=["login.example.com"])
    def test_other_paths_still_validate_host(self):
        response = self.client.get("/anything", HTTP_HOST="evil.example.com")
        self.assertEqual(response.status_code, 400)

    def test_readyz_ready_with_no_checks(self):
        response = self.client.get("/readyz")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["status"], "ready")

    def test_readyz_fails_when_a_check_fails(self):
        probes.READINESS_CHECKS["postgres"] = lambda: (False, "connection refused")
        response = self.client.get("/readyz")
        self.assertEqual(response.status_code, 503)
        self.assertEqual(response.json()["checks"]["postgres"], "connection refused")

    def test_readyz_treats_crashing_check_as_failed(self):
        def broken():
            raise RuntimeError("bug")

        probes.READINESS_CHECKS["redis"] = broken
        response = self.client.get("/readyz")
        self.assertEqual(response.status_code, 503)
        self.assertIn("RuntimeError", response.json()["checks"]["redis"])

    def test_healthz_ignores_failing_dependencies(self):
        probes.READINESS_CHECKS["postgres"] = lambda: (False, "down")
        self.assertEqual(self.client.get("/healthz").status_code, 200)

    def test_probe_wrong_method(self):
        response = self.client.post("/healthz")
        self.assertEqual(response.status_code, 405)
        self.assertEqual(response["Allow"], "GET, HEAD")

    def test_unknown_path(self):
        self.assertEqual(self.client.get("/nope").status_code, 404)


class RequestIDTests(SimpleTestCase):
    def test_generated_when_missing(self):
        request_id = self.client.get("/healthz")["X-Request-ID"]
        self.assertRegex(request_id, r"^[0-9a-f]{32}$")

    def test_reused_when_valid(self):
        response = self.client.get("/healthz", HTTP_X_REQUEST_ID="abc-123_x.y")
        self.assertEqual(response["X-Request-ID"], "abc-123_x.y")

    def test_replaced_when_invalid(self):
        for bad in ("has space", "new\nline", "a" * 129):
            with self.subTest(bad=bad):
                response = self.client.get("/healthz", HTTP_X_REQUEST_ID=bad)
                self.assertNotEqual(response["X-Request-ID"], bad)
                self.assertRegex(response["X-Request-ID"], r"^[0-9a-f]{32}$")


class AccessLogTests(SimpleTestCase):
    def test_logs_one_json_line_per_request(self):
        with self.assertLogs("access", level="INFO") as logs:
            self.client.get("/nope", HTTP_X_REQUEST_ID="req-1")
        record = logs.records[0]
        line = json.loads(JSONFormatter().format(record))
        self.assertEqual(line["status"], 404)
        self.assertEqual(line["path"], "/nope")
        self.assertEqual(line["request_id"], "req-1")
        self.assertIn("duration_ms", line)

    def test_probes_logged_at_debug(self):
        with self.assertLogs("access", level="DEBUG") as logs:
            self.client.get("/healthz")
        self.assertEqual(logs.records[0].levelno, logging.DEBUG)
