import unittest
from unittest.mock import patch

import base64
import json

from api_register import (
    APIResponse,
    MailAccount,
    choose_initial_screen_hint,
    choose_post_email_action,
    register_account,
    resolve_workspace_id,
    should_relogin_after_create_account,
)


class FlowLogicTests(unittest.TestCase):
    def test_dashboard_login_contract_uses_signup_screen_hint_initially(self):
        self.assertEqual(choose_initial_screen_hint("login"), "signup")

    def test_dashboard_login_contract_uses_page_type_for_new_account(self):
        self.assertEqual(
            choose_post_email_action("create_account_password", is_login=True),
            "register_password",
        )

    def test_dashboard_login_contract_uses_page_type_for_existing_account(self):
        self.assertEqual(
            choose_post_email_action("login_password", is_login=True),
            "login_password",
        )

    def test_relogin_required_after_new_account_creation(self):
        self.assertTrue(should_relogin_after_create_account(False, True))

    def test_workspace_resolution_prefers_create_account_data(self):
        workspace_id, cookie_data = resolve_workspace_id(
            "",
            {"workspace": {"id": "ws-create"}},
        )
        self.assertEqual(workspace_id, "ws-create")
        self.assertIsNone(cookie_data)

    def test_workspace_resolution_falls_back_to_continue_data(self):
        workspace_id, cookie_data = resolve_workspace_id(
            "",
            {},
            {"workspace": {"id": "ws-continue"}},
        )
        self.assertEqual(workspace_id, "ws-continue")
        self.assertIsNone(cookie_data)

    def test_workspace_resolution_falls_back_to_cookie(self):
        payload = base64.urlsafe_b64encode(
            json.dumps({"workspaces": [{"id": "ws-cookie"}]}).encode("utf-8")
        ).decode("ascii").rstrip("=")
        workspace_id, cookie_data = resolve_workspace_id(f"{payload}.sig")
        self.assertEqual(workspace_id, "ws-cookie")
        self.assertEqual(cookie_data["workspaces"][0]["id"], "ws-cookie")

    def test_workspace_resolution_reports_malformed_cookie(self):
        with self.assertRaisesRegex(RuntimeError, "解析 cookie"):
            resolve_workspace_id("bad-cookie")


class FakeSession:
    scenario = "new_account"
    instances = []

    def __init__(self, proxy: str = ""):
        self.proxy = proxy
        self.calls = []
        self.instance_id = len(FakeSession.instances) + 1
        self.cookies = {
            "oai-did": "device-123",
            "oai-client-auth-session": self._workspace_cookie("ws-cookie"),
        }
        self._signup_calls = 0
        FakeSession.instances.append(self)

    def _workspace_cookie(self, workspace_id: str) -> str:
        payload = base64.urlsafe_b64encode(
            json.dumps({"workspaces": [{"id": workspace_id}]}).encode("utf-8")
        ).decode("ascii").rstrip("=")
        return f"{payload}.sig"

    def get(self, url: str, **kwargs):
        self.calls.append(("GET", url, None))
        return APIResponse(200, "ok", {})

    def post_json(self, url: str, data: dict, headers=None):
        self.calls.append(("POST_JSON", url, data))
        if url.endswith("/backend-api/sentinel/req"):
            return APIResponse(200, json.dumps({"token": "sentinel-token"}), {})
        if url.endswith("/authorize/continue"):
            self._signup_calls += 1
            screen_hint = data.get("screen_hint")
            if FakeSession.scenario == "new_account" and self.instance_id == 1 and self._signup_calls == 1:
                page_type = "create_account_password"
            elif screen_hint == "login":
                page_type = "login_password"
            else:
                page_type = "login_password"
            return APIResponse(200, json.dumps({"page": {"type": page_type}}), {})
        if url.endswith("/user/register"):
            return APIResponse(200, json.dumps({"page": {"type": "email_otp_verification"}}), {})
        if url.endswith("/password/verify"):
            return APIResponse(200, json.dumps({"page": {"type": "email_otp_verification"}}), {})
        if url.endswith("/email-otp/send"):
            return APIResponse(200, json.dumps({"sent": True}), {})
        if url.endswith("/email-otp/validate"):
            return APIResponse(200, json.dumps({"ok": True}), {})
        if url.endswith("/create_account"):
            return APIResponse(200, json.dumps({"workspace": {"id": "ws-created"}}), {})
        if url.endswith("/workspace/select"):
            return APIResponse(200, json.dumps({"continue_url": "http://continue.example"}), {})
        if url.endswith("/passwordless/send-otp"):
            return APIResponse(200, json.dumps({"sent": True}), {})
        return APIResponse(200, json.dumps({}), {})

    def post_form(self, url: str, data: dict):
        self.calls.append(("POST_FORM", url, data))
        token_payload = {
            "access_token": "access-token",
            "refresh_token": "refresh-token",
            "id_token": "header.payload.signature",
            "expires_in": 3600,
        }
        return APIResponse(200, json.dumps(token_payload), {})

    def get_cookie(self, name: str):
        return self.cookies.get(name)

    def follow_redirects(self, url: str, max_hops: int = 12):
        self.calls.append(("FOLLOW", url, {"max_hops": max_hops}))
        return "http://localhost:1455/auth/callback?code=auth-code&state=state-123"

    def close(self):
        return None

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()


class RegisterAccountSequenceTests(unittest.TestCase):
    def setUp(self):
        FakeSession.instances = []

    def _run_register_account(self, scenario: str):
        FakeSession.scenario = scenario
        account = MailAccount(email="user@example.com", password="Secret123!")
        with patch("api_register.APISession", FakeSession), \
             patch("api_register.create_oauth_params", return_value={
                 "auth_url": "https://auth.openai.com/oauth/authorize?state=state-123",
                 "state": "state-123",
                 "verifier": "verifier-123",
             }), \
             patch("api_register.poll_verification_code", side_effect=["111111", "222222"]), \
             patch("api_register.random.uniform", return_value=0):
            register_account(account, mode="login")
        return list(FakeSession.instances)

    def test_new_account_dashboard_contract_uses_register_password_then_email_otp(self):
        sessions = self._run_register_account("new_account")
        urls = [url for session in sessions for _, url, _ in session.calls]
        self.assertIn("https://auth.openai.com/api/accounts/user/register", urls)
        self.assertIn("https://auth.openai.com/api/accounts/email-otp/send", urls)
        self.assertNotIn("https://auth.openai.com/api/accounts/passwordless/send-otp", urls)

    def test_existing_account_dashboard_contract_uses_password_verify(self):
        sessions = self._run_register_account("existing_account")
        urls = [url for session in sessions for _, url, _ in session.calls]
        self.assertIn("https://auth.openai.com/api/accounts/password/verify", urls)
        self.assertNotIn("https://auth.openai.com/api/accounts/passwordless/send-otp", urls)

    def test_new_account_dashboard_contract_relogs_after_create_account(self):
        sessions = self._run_register_account("new_account")
        self.assertEqual(len(sessions), 2)
        relogin_urls = [url for _, url, _ in sessions[1].calls]
        self.assertIn("https://auth.openai.com/api/accounts/password/verify", relogin_urls)

    def test_dashboard_login_contract_uses_signup_then_login_screen_hints(self):
        sessions = self._run_register_account("new_account")
        first_screen_hint = None
        relogin_screen_hint = None

        for method, url, data in sessions[0].calls:
            if method == "POST_JSON" and url.endswith("/authorize/continue"):
                first_screen_hint = data.get("screen_hint")
                break

        for method, url, data in sessions[1].calls:
            if method == "POST_JSON" and url.endswith("/authorize/continue"):
                relogin_screen_hint = data.get("screen_hint")
                break

        self.assertEqual(first_screen_hint, "signup")
        self.assertEqual(relogin_screen_hint, "login")

    def test_new_account_relogin_retries_without_recreating_account(self):
        class ReloginRetrySession(FakeSession):
            role_sequence = ["main", "relogin_fail", "relogin_success"]

            def __init__(self, proxy: str = ""):
                super().__init__(proxy)
                self.role = self.role_sequence[self.instance_id - 1]

            def post_json(self, url: str, data: dict, headers=None):
                if self.role == "relogin_fail" and url.endswith("/authorize/continue"):
                    self.calls.append(("POST_JSON", url, data))
                    return APIResponse(403, "<!DOCTYPE html><title>Just a moment...</title>", {})
                return super().post_json(url, data, headers=headers)

        created_sessions = []

        def session_factory(proxy: str = ""):
            session = ReloginRetrySession(proxy)
            created_sessions.append(session)
            return session

        account = MailAccount(email="user@example.com", password="Secret123!")
        with patch("api_register.create_api_session", side_effect=session_factory), \
             patch("api_register.create_oauth_params", return_value={
                 "auth_url": "https://auth.openai.com/oauth/authorize?state=state-123",
                 "state": "state-123",
                 "verifier": "verifier-123",
             }), \
             patch("api_register.poll_verification_code", side_effect=["111111", "222222"]), \
             patch("api_register.random.uniform", return_value=0):
            result = register_account(account, mode="login")

        self.assertEqual(result["access_token"], "access-token")
        self.assertEqual(len(created_sessions), 3)

        create_calls = [
            1
            for method, url, _ in created_sessions[0].calls
            if method == "POST_JSON" and url.endswith("/create_account")
        ]
        self.assertEqual(len(create_calls), 1)
