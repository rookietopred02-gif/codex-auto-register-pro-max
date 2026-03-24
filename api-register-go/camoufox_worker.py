#!/usr/bin/env python3
import importlib.util
import json
import os
import random
import sys
import time
import traceback
import uuid
from pathlib import Path
from urllib.parse import urlparse


def write_response(payload: dict) -> int:
    sys.stdout.write(json.dumps(payload, ensure_ascii=False))
    sys.stdout.flush()
    return 0


def camoufox_ready() -> bool:
    return importlib.util.find_spec("camoufox") is not None


def camoufox_binary_path():
    if not camoufox_ready():
        return None, "missing_dependency"
    try:
        from camoufox.pkgman import camoufox_path

        return str(camoufox_path(download_if_missing=False)), None
    except FileNotFoundError:
        return None, "missing_binary"
    except Exception as exc:
        return None, f"binary_check_failed: {exc}"


def session_root() -> Path:
    return Path(__file__).resolve().parent / ".camoufox-sessions"


def session_dir(session_id: str) -> Path:
    return session_root() / session_id


def session_profile_dir(session_id: str) -> Path:
    return session_dir(session_id) / "profile"


def session_state_path(session_id: str) -> Path:
    return session_dir(session_id) / "state.json"


def save_session_state(session_id: str, state: dict) -> None:
    base = session_dir(session_id)
    base.mkdir(parents=True, exist_ok=True)
    session_state_path(session_id).write_text(
        json.dumps(state, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def load_session_state(session_id: str):
    path = session_state_path(session_id)
    if not path.exists():
        return None
    return json.loads(path.read_text(encoding="utf-8"))


def parse_proxy(proxy_value: str):
    proxy_value = (proxy_value or "").strip()
    if not proxy_value:
        return None
    parsed = urlparse(proxy_value)
    if not parsed.scheme or not parsed.hostname:
        raise ValueError(f"invalid proxy url: {proxy_value}")
    server = f"{parsed.scheme}://{parsed.hostname}"
    if parsed.port:
        server += f":{parsed.port}"
    proxy = {"server": server}
    if parsed.username:
        proxy["username"] = parsed.username
    if parsed.password:
        proxy["password"] = parsed.password
    return proxy


def detect_page_type(current_url: str, body_text: str = "") -> str:
    lowered = (current_url or "").lower()
    body_lower = (body_text or "").lower()
    if lowered.startswith("http://localhost:1455/auth/callback"):
        return "callback"
    if "add-phone" in lowered:
        return "add_phone"
    if "email-verification" in lowered:
        return "email_verification"
    if "about-you" in lowered:
        return "about_you"
    if "consent" in lowered:
        return "consent"
    if "create-account/password" in lowered:
        return "create_account_password"
    if "create-account" in lowered:
        return "create_account"
    if "log-in/password" in lowered:
        return "log_in_password"
    if "log-in" in lowered:
        return "log_in"
    if (
        "cannot create your account with the given information" in body_lower
        or "registration_disallowed" in body_lower
    ):
        return "registration_disallowed"
    if "user_already_exists" in body_lower:
        return "user_already_exists"
    if "oops, an error occurred" in body_lower and "authentication" in body_lower:
        return "auth_error"
    if "authorize" in lowered:
        return "authorize"
    return ""


def build_launch_kwargs(session_id: str, headless, proxy_value: str):
    launch_kwargs = {
        "persistent_context": True,
        "user_data_dir": str(session_profile_dir(session_id)),
        "headless": headless,
    }
    if proxy_value:
        launch_kwargs["proxy"] = parse_proxy(proxy_value)
        launch_kwargs["geoip"] = True
    return launch_kwargs


def locator_exists(locator) -> bool:
    try:
        return locator.count() > 0
    except Exception:
        return False


def safe_body_text(page) -> str:
    try:
        return page.locator("body").inner_text(timeout=3000)
    except Exception:
        return ""


def extract_error_messages(page):
    selector = '[role="alert"], [aria-live], [data-testid*="error"], .error'
    try:
        return page.locator(selector).evaluate_all(
            "els => els.map(e => (e.innerText || '').trim()).filter(Boolean)"
        )
    except Exception:
        return []


def extract_cookie_debug(context):
    cookies = context.cookies()
    device_id = ""
    cookie_names = []
    for cookie in cookies:
        name = cookie.get("name", "")
        if name:
            cookie_names.append(name)
        if name == "oai-did":
            device_id = cookie.get("value", "")
    return {
        "device_id": device_id,
        "cookie_names": cookie_names,
    }


def snapshot_page(page, context, callback_url: str = ""):
    current_url = page.url
    title = ""
    try:
        title = page.title()
    except Exception:
        pass
    body_text = safe_body_text(page)
    page_type = detect_page_type(current_url, body_text)
    blocker_page_type = ""
    if page_type in {"add_phone", "registration_disallowed"}:
        blocker_page_type = page_type

    cookie_debug = extract_cookie_debug(context)
    return {
        "current_url": current_url,
        "page_type": page_type,
        "blocker_page_type": blocker_page_type,
        "callback_url": callback_url,
        "debug": {
            "title": title,
            "body_excerpt": body_text[:1200],
            "errors": extract_error_messages(page),
            **cookie_debug,
        },
    }


def click_first(locator, timeout=10000):
    locator.first.click(timeout=timeout)


def fill_first(locator, value: str, timeout=10000):
    locator.first.fill(value, timeout=timeout)


def input_value(locator, timeout=2000) -> str:
    try:
        return locator.first.input_value(timeout=timeout)
    except Exception:
        return ""


def locator_is_enabled(locator, timeout=2000) -> bool:
    try:
        return locator.first.is_enabled(timeout=timeout)
    except Exception:
        return False


def wait_for_first_visible(locator, timeout=1000) -> bool:
    try:
        locator.first.wait_for(state="visible", timeout=timeout)
        return True
    except Exception:
        return False


def wait_for_page_transition(page, previous_page_type: str, timeout_ms=8000) -> bool:
    deadline = time.time() + timeout_ms / 1000
    while time.time() < deadline:
        current_page_type = detect_page_type(page.url, safe_body_text(page))
        if current_page_type and current_page_type != previous_page_type:
            return True
        page.wait_for_timeout(250)
    return False


def find_first_visible_locator(page, selectors, timeout_ms=12000):
    deadline = time.time() + timeout_ms / 1000
    while time.time() < deadline:
        for selector in selectors:
            locator = page.locator(selector)
            if not locator_exists(locator):
                continue
            if wait_for_first_visible(locator, timeout=800):
                return locator.first, selector
        page.wait_for_timeout(250)
    return None, ""


def describe_inputs(page):
    try:
        return page.locator("input").evaluate_all(
            "els => els.slice(0, 12).map(e => ({type:e.type||'', name:e.name||'', placeholder:e.placeholder||'', autocomplete:e.autocomplete||'', inputmode:e.inputMode||'', id:e.id||'', disabled:!!e.disabled, value:(e.value||'').slice(0,32)}))"
        )
    except Exception:
        return []


def describe_buttons(page):
    try:
        return page.locator("button").evaluate_all(
            "els => els.slice(0, 12).map(e => ({text:(e.innerText||'').trim(), type:e.type||'', name:e.getAttribute('name')||'', value:e.getAttribute('value')||'', disabled:!!e.disabled}))"
        )
    except Exception:
        return []


given_names = [
    "Liam",
    "Noah",
    "Oliver",
    "James",
    "Elijah",
    "William",
    "Henry",
    "Lucas",
    "Olivia",
    "Emma",
    "Charlotte",
    "Amelia",
    "Sophia",
]


family_names = [
    "Smith",
    "Johnson",
    "Williams",
    "Brown",
    "Jones",
    "Miller",
    "Davis",
    "Wilson",
    "Anderson",
    "Thomas",
    "Taylor",
    "Moore",
]


def random_profile():
    year = random.randint(1986, 2005)
    month = random.randint(1, 12)
    day = random.randint(1, 28)
    return {
        "name": f"{random.choice(given_names)} {random.choice(family_names)}",
        "year": f"{year:04d}",
        "month": f"{month:02d}",
        "day": f"{day:02d}",
        "birthday": f"{year:04d}-{month:02d}-{day:02d}",
    }


def fill_date_segment(page, locator, value: str):
    locator.focus()
    page.keyboard.type(value)


def complete_about_you(page, debug: dict):
    profile = random_profile()
    name_input, name_selector = find_first_visible_locator(
        page,
        [
            'input[name="name"]',
            'input[placeholder="Full name"]',
            'input[autocomplete="name"]',
        ],
        timeout_ms=8000,
    )
    month_segment, month_selector = find_first_visible_locator(
        page, ['[data-type="month"]', '[role="spinbutton"][data-type="month"]'], timeout_ms=5000
    )
    day_segment, day_selector = find_first_visible_locator(
        page, ['[data-type="day"]', '[role="spinbutton"][data-type="day"]'], timeout_ms=5000
    )
    year_segment, year_selector = find_first_visible_locator(
        page, ['[data-type="year"]', '[role="spinbutton"][data-type="year"]'], timeout_ms=5000
    )
    submit_button, submit_selector = find_first_visible_locator(
        page,
        [
            'button[type="submit"]:has-text("Finish creating account")',
            'button[type="submit"]',
        ],
        timeout_ms=8000,
    )
    if (
        name_input is None
        or month_segment is None
        or day_segment is None
        or year_segment is None
        or submit_button is None
    ):
        return {
            "ok": False,
            "error": "about_you form is not fully available",
            "selectors": {
                "name": name_selector,
                "month": month_selector,
                "day": day_selector,
                "year": year_selector,
                "submit": submit_selector,
            },
        }

    name_input.fill(profile["name"], timeout=10000)
    fill_date_segment(page, month_segment, profile["month"])
    fill_date_segment(page, day_segment, profile["day"])
    fill_date_segment(page, year_segment, profile["year"])
    page.wait_for_timeout(500)

    hidden_birthday = ""
    try:
        hidden_birthday = page.locator('input[name="birthday"]').first.input_value(timeout=3000)
    except Exception:
        pass

    debug["about_you_profile"] = {
        "name": profile["name"],
        "birthday": profile["birthday"],
        "hidden_birthday": hidden_birthday,
    }
    debug["about_you_selectors"] = {
        "name": name_selector,
        "month": month_selector,
        "day": day_selector,
        "year": year_selector,
        "submit": submit_selector,
    }

    submit_button.click(timeout=10000)
    try:
        page.wait_for_load_state("domcontentloaded", timeout=10000)
    except Exception:
        pass
    page.wait_for_timeout(7000)
    return {"ok": True}


def advance_signup_flow(page, email: str, password: str, mode: str, debug: dict):
    mode = (mode or "signup").strip().lower() or "signup"
    flow_steps = []

    for step in range(8):
        page.wait_for_timeout(1500)
        body_text = safe_body_text(page)
        current_url = page.url
        page_type = detect_page_type(current_url, body_text)
        flow_steps.append(
            {"step": step + 1, "url": current_url, "page_type": page_type}
        )

        if page_type in {
            "email_verification",
            "add_phone",
            "callback",
            "about_you",
            "consent",
            "registration_disallowed",
        }:
            break

        if page_type == "log_in":
            email_input = page.locator('input[type="email"]')
            email_submit = page.locator('button[type="submit"][value="email"]')
            if not locator_exists(email_input) or not locator_exists(email_submit):
                break
            current_email = input_value(email_input).strip().lower()
            if current_email != email.strip().lower():
                if not locator_is_enabled(email_input):
                    debug["flow_blocker"] = {
                        "page_type": page_type,
                        "reason": "email input disabled before value could be updated",
                        "current_value": current_email,
                    }
                    break
                fill_first(email_input, email)
            if not locator_is_enabled(email_submit):
                if wait_for_page_transition(page, page_type):
                    continue
                debug["flow_blocker"] = {
                    "page_type": page_type,
                    "reason": "email submit button remained disabled",
                    "current_value": current_email,
                }
                break
            try:
                click_first(email_submit)
            except Exception as exc:
                if wait_for_page_transition(page, page_type):
                    continue
                debug["flow_blocker"] = {
                    "page_type": page_type,
                    "reason": "email submit click failed",
                    "error": str(exc),
                }
                break
            continue

        if page_type == "log_in_password":
            if mode != "login":
                signup_link = page.get_by_role("link", name="Sign up")
                if locator_exists(signup_link):
                    click_first(signup_link)
                    page.wait_for_timeout(2000)
                    continue
            password_input = page.locator('input[type="password"]')
            submit_button = page.locator('button[type="submit"]:has-text("Continue")')
            if locator_exists(password_input) and locator_exists(submit_button):
                fill_first(password_input, password)
                click_first(submit_button)
                page.wait_for_timeout(5000)
                continue
            break

        if page_type == "create_account":
            email_input = page.locator('input[type="email"]')
            email_submit = page.locator('button[type="submit"][value="email"]')
            if not locator_exists(email_input) or not locator_exists(email_submit):
                break
            current_email = input_value(email_input).strip().lower()
            if current_email != email.strip().lower():
                if not locator_is_enabled(email_input):
                    debug["flow_blocker"] = {
                        "page_type": page_type,
                        "reason": "prefilled email is disabled and does not match request email",
                        "current_value": current_email,
                    }
                    break
                fill_first(email_input, email)
            if not locator_is_enabled(email_submit):
                if wait_for_page_transition(page, page_type):
                    continue
                debug["flow_blocker"] = {
                    "page_type": page_type,
                    "reason": "create-account email submit button remained disabled",
                    "current_value": current_email,
                }
                break
            try:
                click_first(email_submit)
            except Exception as exc:
                if wait_for_page_transition(page, page_type):
                    continue
                debug["flow_blocker"] = {
                    "page_type": page_type,
                    "reason": "create-account email submit click failed",
                    "error": str(exc),
                }
                break
            page.wait_for_timeout(2000)
            continue

        if page_type == "create_account_password":
            password_input = page.locator('input[type="password"]')
            submit_button = page.locator('button[type="submit"]:has-text("Continue")')
            if not locator_exists(password_input) or not locator_exists(submit_button):
                break
            fill_first(password_input, password)
            click_first(submit_button)
            page.wait_for_timeout(5000)
            continue

        break

    debug["flow_steps"] = flow_steps


def status_from_snapshot(snapshot: dict, interactive: bool = False) -> str:
    callback_url = snapshot.get("callback_url", "")
    page_type = snapshot.get("page_type", "")
    errors = snapshot.get("debug", {}).get("errors", [])

    if callback_url:
        return "callback_url_ready"
    if page_type == "email_verification":
        if any("incorrect code" in (error or "").lower() for error in errors):
            return "invalid_otp"
        return "awaiting_otp"
    if page_type in {"add_phone", "registration_disallowed"}:
        return "blocked"
    if interactive:
        return "unexpected_page"
    return "started"


def apply_state_to_response(base: dict, snapshot: dict, status: str):
    debug = base.setdefault("debug", {})
    debug.update(snapshot.get("debug", {}))
    base["status"] = status
    base["page_type"] = snapshot.get("page_type", "")
    base["blocker_page_type"] = snapshot.get("blocker_page_type", "")
    base["current_url"] = snapshot.get("current_url", "")
    base["callback_url"] = snapshot.get("callback_url", "")
    return base


def persist_snapshot(
    session_id: str,
    req: dict,
    snapshot: dict,
    headless: bool,
    proxy_value: str,
):
    save_session_state(
        session_id,
        {
            "session_id": session_id,
            "auth_url": str(req.get("auth_url", "")).strip(),
            "current_url": snapshot.get("current_url", ""),
            "page_type": snapshot.get("page_type", ""),
            "blocker_page_type": snapshot.get("blocker_page_type", ""),
            "callback_url": snapshot.get("callback_url", ""),
            "device_id": snapshot.get("debug", {}).get("device_id", ""),
            "proxy": proxy_value,
            "headless": bool(headless),
            "email": str(req.get("email", "")).strip(),
            "mode": str(req.get("mode", "")).strip(),
        },
    )


def start_flow(req: dict, engine: str, debug: dict):
    auth_url = str(req.get("auth_url", "")).strip()
    if not auth_url:
        return {
            "ok": False,
            "engine": engine,
            "ready": True,
            "status": "invalid_request",
            "error": "auth_url is required for start_flow",
            "debug": debug,
        }

    session_id = str(req.get("session_id", "")).strip() or str(uuid.uuid4())
    headless = req.get("headless")
    if headless is None:
        headless = True

    proxy_value = str(req.get("proxy", "")).strip()
    launch_kwargs = build_launch_kwargs(session_id, headless, proxy_value)
    email = str(req.get("email", "")).strip()
    password = str(req.get("password", "")).strip()
    mode = str(req.get("mode", "")).strip()

    from camoufox.sync_api import Camoufox

    try:
        with Camoufox(**launch_kwargs) as context:
            callback_box = {"url": ""}

            def handle_request(request):
                if request.url.startswith("http://localhost:1455/auth/callback"):
                    callback_box["url"] = request.url

            context.on("request", handle_request)
            page = context.pages[0] if context.pages else context.new_page()
            page.goto(auth_url, wait_until="domcontentloaded", timeout=90000)
            page.wait_for_timeout(2500)

            interactive = bool(email and password)
            if interactive:
                advance_signup_flow(page, email, password, mode, debug)

            snapshot = snapshot_page(page, context, callback_box["url"])
            snapshot["debug"]["profile_dir"] = str(session_profile_dir(session_id))
            status = status_from_snapshot(snapshot, interactive=interactive)
            persist_snapshot(session_id, req, snapshot, headless, proxy_value)

            response = {
                "ok": True,
                "engine": engine,
                "ready": True,
                "session_id": session_id,
                "debug": debug,
            }
            apply_state_to_response(response, snapshot, status)
            return response
    except Exception as exc:
        debug["traceback"] = traceback.format_exc(limit=3)
        return {
            "ok": False,
            "engine": engine,
            "ready": True,
            "session_id": session_id,
            "status": "browser_error",
            "error": str(exc),
            "debug": debug,
        }


def submit_otp(req: dict, engine: str, debug: dict):
    session_id = str(req.get("session_id", "")).strip()
    if not session_id:
        return {
            "ok": False,
            "engine": engine,
            "ready": True,
            "status": "invalid_request",
            "error": "session_id is required for submit_otp",
            "debug": debug,
        }

    otp = str(req.get("otp", "")).strip()
    if not otp:
        return {
            "ok": False,
            "engine": engine,
            "ready": True,
            "session_id": session_id,
            "status": "invalid_request",
            "error": "otp is required for submit_otp",
            "debug": debug,
        }

    state = load_session_state(session_id)
    if not state:
        return {
            "ok": False,
            "engine": engine,
            "ready": True,
            "session_id": session_id,
            "status": "session_not_found",
            "error": f"session state not found for {session_id}",
            "debug": debug,
        }

    headless = req.get("headless")
    if headless is None:
        headless = bool(state.get("headless", True))

    proxy_value = str(req.get("proxy", "")).strip() or str(state.get("proxy", "")).strip()
    current_url = (
        str(req.get("current_url", "")).strip()
        or str(state.get("current_url", "")).strip()
        or str(state.get("auth_url", "")).strip()
    )
    launch_kwargs = build_launch_kwargs(session_id, headless, proxy_value)

    from camoufox.sync_api import Camoufox

    try:
        with Camoufox(**launch_kwargs) as context:
            callback_box = {"url": str(state.get("callback_url", "")).strip()}
            about_you_completed = False

            def handle_request(request):
                if request.url.startswith("http://localhost:1455/auth/callback"):
                    callback_box["url"] = request.url

            context.on("request", handle_request)
            page = context.new_page()
            if current_url:
                page.goto(current_url, wait_until="domcontentloaded", timeout=90000)
            page.wait_for_timeout(2000)

            code_input, input_selector = find_first_visible_locator(
                page,
                [
                    'input[name="code"]',
                    'input[placeholder="Code"]',
                    'input[inputmode="numeric"]',
                    'input[id$="-code"]',
                ],
                timeout_ms=12000,
            )
            validate_button, button_selector = find_first_visible_locator(
                page,
                [
                    'button[type="submit"][value="validate"]',
                    'button[type="submit"]:has-text("Continue")',
                    'button[type="submit"]',
                ],
                timeout_ms=8000,
            )
            if code_input is None or validate_button is None:
                snapshot = snapshot_page(page, context, callback_box["url"])
                snapshot["debug"]["profile_dir"] = str(session_profile_dir(session_id))
                snapshot["debug"]["inputs"] = describe_inputs(page)
                snapshot["debug"]["buttons"] = describe_buttons(page)
                snapshot["debug"]["current_url_requested"] = current_url
                persist_snapshot(session_id, req, snapshot, headless, proxy_value)
                response = {
                    "ok": False,
                    "engine": engine,
                    "ready": True,
                    "session_id": session_id,
                    "error": "OTP form is not available in the current browser session",
                    "debug": debug,
                }
                if input_selector:
                    response["debug"]["otp_input_selector"] = input_selector
                if button_selector:
                    response["debug"]["otp_submit_selector"] = button_selector
                apply_state_to_response(response, snapshot, "unexpected_page")
                return response

            debug["otp_input_selector"] = input_selector
            debug["otp_submit_selector"] = button_selector
            code_input.fill(otp, timeout=10000)
            validate_button.click(timeout=10000)

            deadline = time.time() + 45
            snapshot = None
            status = "unexpected_page"
            while time.time() < deadline:
                page.wait_for_timeout(1000)
                snapshot = snapshot_page(page, context, callback_box["url"])
                page_type = snapshot.get("page_type", "")
                errors = snapshot.get("debug", {}).get("errors", [])
                lowered_errors = " ".join(errors).lower()

                if snapshot.get("callback_url"):
                    status = "callback_url_ready"
                    break
                if page_type in {"add_phone", "registration_disallowed"}:
                    status = "blocked"
                    break
                if page_type == "email_verification" and "incorrect code" in lowered_errors:
                    status = "invalid_otp"
                    break
                if page_type == "about_you":
                    if not about_you_completed:
                        complete_result = complete_about_you(page, debug)
                        debug["about_you_result"] = complete_result
                        if not complete_result.get("ok"):
                            status = "unexpected_page"
                            break
                        about_you_completed = True
                    continue
                if page_type and page_type != "email_verification":
                    status = "unexpected_page"
                    break

            if snapshot is None:
                snapshot = snapshot_page(page, context, callback_box["url"])
                status = status_from_snapshot(snapshot, interactive=True)

            snapshot["debug"]["profile_dir"] = str(session_profile_dir(session_id))
            persist_snapshot(session_id, req, snapshot, headless, proxy_value)

            response = {
                "ok": status in {"callback_url_ready", "awaiting_otp"},
                "engine": engine,
                "ready": True,
                "session_id": session_id,
                "debug": debug,
            }
            apply_state_to_response(response, snapshot, status)
            if status == "invalid_otp":
                response["error"] = "Incorrect code"
            elif status == "blocked" and snapshot.get("blocker_page_type"):
                response["error"] = snapshot["blocker_page_type"]
            elif status == "unexpected_page":
                response["error"] = f"unexpected page after OTP submission: {snapshot.get('page_type', '') or snapshot.get('current_url', '')}"
            return response
    except Exception as exc:
        debug["traceback"] = traceback.format_exc(limit=3)
        return {
            "ok": False,
            "engine": engine,
            "ready": True,
            "session_id": session_id,
            "status": "browser_error",
            "error": str(exc),
            "debug": debug,
        }


def main() -> int:
    raw = sys.stdin.read()
    if not raw.strip():
        return write_response({"ok": False, "error": "empty request"})

    try:
        req = json.loads(raw)
    except json.JSONDecodeError as exc:
        return write_response({"ok": False, "error": f"invalid json: {exc}"})

    action = str(req.get("action", "")).strip()
    engine = str(req.get("engine", "camoufox")).strip() or "camoufox"
    ready = camoufox_ready()
    binary_path, binary_error = camoufox_binary_path()
    debug = {
        "python": sys.version.split()[0],
        "script": os.path.abspath(__file__),
        "binary_path": binary_path,
    }

    if action == "handshake":
        status = "ready"
        error = None
        if not ready:
            status = "missing_dependency"
            error = "camoufox is not installed in the active Python environment"
        elif binary_error == "missing_binary":
            status = "missing_binary"
            error = "camoufox python package is installed, but browser binaries are missing"
        elif binary_error:
            status = "binary_check_failed"
            error = binary_error
        return write_response({
            "ok": True,
            "engine": engine,
            "ready": ready and binary_error is None,
            "status": status,
            "error": error,
            "debug": debug,
        })

    if action in {"start_flow", "submit_otp"}:
        if not ready:
            return write_response({
                "ok": False,
                "engine": engine,
                "ready": False,
                "status": "missing_dependency",
                "error": "camoufox is not installed in the active Python environment",
                "debug": debug,
            })
        if binary_error == "missing_binary":
            return write_response({
                "ok": False,
                "engine": engine,
                "ready": False,
                "status": "missing_binary",
                "error": "camoufox python package is installed, but browser binaries are missing",
                "debug": debug,
            })
        if binary_error:
            return write_response({
                "ok": False,
                "engine": engine,
                "ready": False,
                "status": "binary_check_failed",
                "error": binary_error,
                "debug": debug,
            })
        if action == "start_flow":
            return write_response(start_flow(req, engine, debug))
        return write_response(submit_otp(req, engine, debug))

    return write_response({
        "ok": False,
        "engine": engine,
        "error": f"unsupported action: {action}",
        "debug": debug,
    })


if __name__ == "__main__":
    raise SystemExit(main())
