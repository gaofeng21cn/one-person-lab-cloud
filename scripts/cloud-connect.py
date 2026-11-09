#!/usr/bin/env python3
"""Cloud-owned local credential helper for the Framework Connect provider."""

import argparse
import datetime as dt
import fcntl
import http.client
import ipaddress
import json
import os
from pathlib import Path
import re
import signal
import ssl
import stat
import sys
import tempfile
import urllib.parse
import uuid


SCHEMA = "opl-cloud-connect.v1"
AUDIENCE = "opl-cloud-mcp"
PROTOCOL = "2025-03-26"
REQUEST_LIMIT = 64 * 1024
RESPONSE_LIMIT = 1024 * 1024
HTTP_TIMEOUT = 4
PAGE_TOOLS = (
    "cloud_list_compute_plans", "cloud_list_storage_plans", "cloud_list_workspaces",
)
OBJECT_TOOLS = (
    "cloud_get_quote", "cloud_get_workspace", "cloud_get_workspace_operation",
)
CAPABILITIES = PAGE_TOOLS + OBJECT_TOOLS
REF = re.compile(r"cloud:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\Z")
REVISION = re.compile(r"rev:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")


class Failure(Exception):
    def __init__(self, code, disposition="not_dispatched"):
        self.code, self.disposition = code, disposition


def fail(code, disposition="not_dispatched"):
    raise Failure(code, disposition)


def exact_fields(value, required, optional=()):
    if not isinstance(value, dict) or not set(required) <= value.keys() or value.keys() - set(required) - set(optional):
        fail("INVALID_INPUT")


def text_value(value, limit=256):
    return isinstance(value, str) and bool(value.strip()) and len(value) <= limit and not any(ord(c) < 32 or ord(c) == 127 for c in value)


def decode_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("duplicate JSON field")
            result[key] = value
        return result

    try:
        return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda _: fail("INVALID_INPUT"))
    except (ValueError, UnicodeError, RecursionError):
        fail("INVALID_INPUT")


def expiration(value):
    if not text_value(value, 64):
        fail("INVALID_CREDENTIAL")
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            fail("INVALID_CREDENTIAL")
        return parsed
    except ValueError:
        fail("INVALID_CREDENTIAL")


def endpoint_origin(value):
    if not text_value(value, 2048) or any(c.isspace() for c in value) or "\\" in value:
        fail("INVALID_ENDPOINT")
    try:
        parsed = urllib.parse.urlsplit(value)
        port = parsed.port
        host = parsed.hostname
        if not host or parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
            fail("INVALID_ENDPOINT")
        loopback = host.lower() == "localhost"
        try:
            loopback = loopback or ipaddress.ip_address(host).is_loopback
        except ValueError:
            pass
        if parsed.scheme != "https" and not (parsed.scheme == "http" and loopback):
            fail("INVALID_ENDPOINT")
        # Do not allow encoded separators/hosts or a zero port to change routing.
        if "%" in host or port == 0:
            fail("INVALID_ENDPOINT")
        hostname = "[" + host.lower() + "]" if ":" in host else host.lower()
        default_port = 443 if parsed.scheme == "https" else 80
        suffix = ":" + str(port) if port and port != default_port else ""
        return parsed.scheme + "://" + hostname + suffix
    except (ValueError, UnicodeError):
        fail("INVALID_ENDPOINT")


def credential_record(value):
    exact_fields(value, ("id", "label", "audience", "expiresAt", "credential"), ("tenantId", "issuedAt"))
    token = value["credential"]
    if value["audience"] != AUDIENCE or not text_value(value["id"]) or not text_value(value["label"]):
        fail("INVALID_CREDENTIAL")
    if "tenantId" in value and value["tenantId"] is not None and not text_value(value["tenantId"]):
        fail("INVALID_CREDENTIAL")
    if not isinstance(token, str) or not 16 <= len(token) <= 4096 or not re.fullmatch(r"[A-Za-z0-9._~+/=-]+", token):
        fail("INVALID_CREDENTIAL")
    expiration(value["expiresAt"])
    if "issuedAt" in value:
        expiration(value["issuedAt"])
    # Never project a downloaded bearer token through an accidental metadata echo.
    if token in json.dumps({k: v for k, v in value.items() if k != "credential"}):
        fail("INVALID_CREDENTIAL")
    return value


def store_root(create=False):
    root = Path(os.environ.get("OPL_CLOUD_CONNECT_HOME", str(Path.home() / ".config" / "opl-cloud" / "connect"))).expanduser()
    if create:
        root.mkdir(mode=0o700, parents=True, exist_ok=True)
    try:
        info = root.lstat()
    except FileNotFoundError:
        return root
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        fail("CREDENTIAL_STORE_UNSAFE")
    return root


def read_json_file(path, private=False):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, "rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or (private and (info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600)):
                fail("CREDENTIAL_STORE_UNSAFE")
            raw = stream.read(REQUEST_LIMIT + 1)
    except FileNotFoundError:
        fail("CREDENTIAL_NOT_FOUND")
    except OSError:
        fail("CREDENTIAL_STORE_UNAVAILABLE")
    if len(raw) > REQUEST_LIMIT:
        fail("INVALID_CREDENTIAL")
    return decode_json(raw)


def load_record(path):
    record = read_json_file(path, private=True)
    exact_fields(record, ("schema_version", "cloud_ref", "revision", "endpoint", "credential"))
    if record["schema_version"] != SCHEMA or not isinstance(record["cloud_ref"], str) or not REF.fullmatch(record["cloud_ref"]):
        fail("INVALID_CREDENTIAL")
    if not isinstance(record["revision"], str) or not REVISION.fullmatch(record["revision"]):
        fail("INVALID_CREDENTIAL")
    if path.stem != record["cloud_ref"][6:] or endpoint_origin(record["endpoint"]) != record["endpoint"]:
        fail("INVALID_CREDENTIAL")
    credential_record(record["credential"])
    return record


def metadata(record):
    credential = record["credential"]
    expired = expiration(credential["expiresAt"]) <= dt.datetime.now(dt.timezone.utc)
    return {
        "cloud_ref": record["cloud_ref"], "revision": record["revision"],
        "credential_id": credential["id"], "label": credential["label"],
        "tenant_id": credential.get("tenantId"), "audience": AUDIENCE,
        "expires_at": credential["expiresAt"], "endpoint": record["endpoint"],
        "status": "attention_needed" if expired else "ready",
        "status_code": "CREDENTIAL_EXPIRED" if expired else "READY",
        "capabilities": [] if expired else list(CAPABILITIES),
    }


def records(root):
    return [load_record(path) for path in sorted(root.glob("*.json"))]


def import_credential(path, endpoint):
    origin = endpoint_origin(endpoint)
    credential = credential_record(read_json_file(path))
    if expiration(credential["expiresAt"]) <= dt.datetime.now(dt.timezone.utc):
        fail("CREDENTIAL_EXPIRED")
    root = store_root(create=True)
    # Concurrent imports cannot mint two bindings for the same credential.
    fd = os.open(root / ".import.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "a") as lock:
        info = os.fstat(lock.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600:
            fail("CREDENTIAL_STORE_UNSAFE")
        fcntl.flock(lock, fcntl.LOCK_EX)
        matching = [r for r in records(root) if r["endpoint"] == origin and r["credential"]["id"] == credential["id"]]
        reference = matching[0]["cloud_ref"] if matching else "cloud:" + str(uuid.uuid4())
        record = {"schema_version": SCHEMA, "cloud_ref": reference, "revision": "rev:" + str(uuid.uuid4()), "endpoint": origin, "credential": credential}
        fd, temporary = tempfile.mkstemp(prefix=".credential-", dir=root)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as stream:
                json.dump(record, stream, ensure_ascii=True)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, root / (reference[6:] + ".json"))
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    return metadata(record)


def validate_invocation(capability, value):
    if capability not in CAPABILITIES:
        fail("CAPABILITY_UNAVAILABLE")
    if capability in PAGE_TOOLS:
        exact_fields(value, (), ("cursor", "limit"))
        if "cursor" in value and (not isinstance(value["cursor"], str) or len(value["cursor"]) > 4096):
            fail("INVALID_INPUT")
        if "limit" in value and (type(value["limit"]) is not int or not 1 <= value["limit"] <= 100):
            fail("INVALID_INPUT")
    else:
        exact_fields(value, ("id",), ("expectedRevision",))
        if not text_value(value["id"]) or ("expectedRevision" in value and (not isinstance(value["expectedRevision"], str) or len(value["expectedRevision"]) > 256)):
            fail("INVALID_INPUT")


def mcp_request(record, message, notification=False):
    parsed = urllib.parse.urlsplit(record["endpoint"])
    token = record["credential"]["credential"]
    connection_type = http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
    options = {"timeout": HTTP_TIMEOUT}
    if parsed.scheme == "https":
        options["context"] = ssl.create_default_context()
    connection = connection_type(parsed.hostname, parsed.port, **options)
    dispatched = False

    def deadline(_number, _frame):
        fail("REMOTE_RESPONSE_UNKNOWN" if dispatched else "REMOTE_UNREACHABLE",
             "unknown_after_dispatch" if dispatched else "not_dispatched")

    previous_handler = signal.signal(signal.SIGALRM, deadline)
    signal.setitimer(signal.ITIMER_REAL, HTTP_TIMEOUT)
    try:
        try:
            connection.connect()
        except (OSError, http.client.HTTPException):
            fail("REMOTE_UNREACHABLE")
        try:
            dispatched = True
            connection.request("POST", "/api/v2/mcp", body=json.dumps(message).encode("utf-8"), headers={
                "Authorization": "Bearer " + token,
                "Content-Type": "application/json", "Accept": "application/json, text/event-stream",
                "MCP-Protocol-Version": PROTOCOL,
            })
            response = connection.getresponse()
            if 300 <= response.status < 400:
                fail("REMOTE_REDIRECT_REJECTED", "provider_reported")
            if response.status != 200 and not (notification and response.status in (202, 204)):
                code = {401: "REMOTE_UNAUTHORIZED", 403: "REMOTE_FORBIDDEN"}.get(response.status, "REMOTE_HTTP_ERROR")
                fail(code, "provider_reported")
            raw = response.read(RESPONSE_LIMIT + 1)
            if len(raw) > RESPONSE_LIMIT:
                fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
            if notification and not raw:
                return None
            if response.getheader("Content-Type", "").split(";", 1)[0].strip().lower() != "application/json":
                fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
            try:
                result = decode_json(raw)
            except Failure:
                fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
            if not isinstance(result, dict) or result.get("jsonrpc") != "2.0" or result.get("id") != message.get("id"):
                fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
            if "error" in result:
                fail("REMOTE_MCP_ERROR", "provider_reported")
            if not isinstance(result.get("result"), dict):
                fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
            return result["result"]
        except (OSError, http.client.HTTPException):
            fail("REMOTE_RESPONSE_UNKNOWN", "unknown_after_dispatch")
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous_handler)
        connection.close()


def invoke(record, capability, value):
    if metadata(record)["status"] != "ready":
        fail("CREDENTIAL_EXPIRED")
    initialized = mcp_request(record, {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
        "protocolVersion": PROTOCOL, "capabilities": {}, "clientInfo": {"name": "opl-cloud-connect", "version": "1.0.0"},
    }})
    if initialized.get("protocolVersion") != PROTOCOL:
        fail("REMOTE_PROTOCOL_UNSUPPORTED", "provider_reported")
    mcp_request(record, {"jsonrpc": "2.0", "method": "notifications/initialized"}, notification=True)
    result = mcp_request(record, {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": capability, "arguments": value}})
    if result.get("isError"):
        fail("REMOTE_TOOL_ERROR", "provider_reported")
    if not isinstance(result.get("structuredContent"), dict):
        fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
    if record["credential"]["credential"] in json.dumps(result["structuredContent"]):
        fail("REMOTE_RESPONSE_INVALID", "unknown_after_dispatch")
    return result["structuredContent"]


def connect(request):
    if not isinstance(request, dict):
        fail("INVALID_INPUT")
    operation = request.get("operation")
    required = {"schema_version", "operation"}
    if operation in ("inspect", "invoke"):
        required.add("credential_handle")
    if operation == "invoke":
        required.update(("capability", "input"))
    exact_fields(request, required)
    if request["schema_version"] != SCHEMA or operation not in ("list", "inspect", "invoke"):
        fail("INVALID_INPUT")
    root = store_root()
    if operation == "list":
        return {"connections": [metadata(record) for record in records(root)]}
    handle = request["credential_handle"]
    exact_fields(handle, ("opaque", "revision"))
    if not isinstance(handle["opaque"], str) or not REF.fullmatch(handle["opaque"]) or not isinstance(handle["revision"], str) or not REVISION.fullmatch(handle["revision"]):
        fail("INVALID_INPUT")
    if operation == "invoke":
        validate_invocation(request["capability"], request["input"])
    record = load_record(root / (handle["opaque"][6:] + ".json"))
    if handle["revision"] != record["revision"]:
        fail("STALE_CREDENTIAL_HANDLE")
    if operation == "inspect":
        return {"connection": metadata(record)}
    return invoke(record, request["capability"], request["input"])


class ArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        fail("INVALID_ARGUMENTS")


def main():
    try:
        parser = ArgumentParser(description=__doc__)
        commands = parser.add_subparsers(dest="command", required=True)
        commands.add_parser("connect", help="Read one bounded provider request from stdin")
        manual = commands.add_parser("import", help="Import a human-downloaded Cloud credential file")
        manual.add_argument("--credential-file", required=True)
        manual.add_argument("--endpoint", required=True, help="Cloud HTTPS origin")
        args = parser.parse_args()
        if args.command == "import":
            result = import_credential(args.credential_file, args.endpoint)
        else:
            raw = sys.stdin.buffer.read(REQUEST_LIMIT + 1)
            if len(raw) > REQUEST_LIMIT:
                fail("INVALID_INPUT")
            result = connect(decode_json(raw))
        print(json.dumps({"ok": True, "disposition": "completed", "result": result}, ensure_ascii=True))
        return 0
    except Failure as error:
        envelope = {"ok": False, "disposition": error.disposition, "error": {"code": error.code}}
    except (OSError, ValueError, TypeError, RecursionError):
        envelope = {"ok": False, "disposition": "not_dispatched", "error": {"code": "LOCAL_HELPER_ERROR"}}
    print(json.dumps(envelope))
    return 1


if __name__ == "__main__":
    sys.exit(main())
