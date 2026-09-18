#!/usr/bin/env python3
"""Configure the local dev realm for the editor login proxy.

Idempotent: safe to re-run. Talks to the Keycloak Admin REST API with nothing
but the Python standard library, so the compose service needs no pip install.

This mirrors the approach in membership-registry (keycloak/setup.py there) —
an empty realm imported at boot, everything real applied over the Admin API —
scoped down to what the proxy needs.

NEVER run this against production Keycloak. Production clients are registered
by hand through the admin console, following
membership-registry/docs/keycloak-clients.md.

Environment:
    KEYCLOAK_URL        base URL of Keycloak      (default http://keycloak:8180)
    KEYCLOAK_REALM      realm to configure        (default membership-registry)
    KC_ADMIN            bootstrap admin username  (default admin)
    KC_ADMIN_PASSWORD   bootstrap admin password  (default admin)
    CMS_CLIENT_ID       client to create          (default cms-auth-proxy)
    CMS_CLIENT_SECRET   its secret                (default dev-secret-cms-auth-proxy)
    CMS_REDIRECT_URI    exact OAuth callback      (default http://localhost:8080/callback)
    CMS_WEB_ORIGIN      browser origin of the CMS (default http://localhost:1313)
    EDITOR_ROLE         realm role required to edit (default cms-editor)
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BASE_URL = os.environ.get("KEYCLOAK_URL", "http://keycloak:8180").rstrip("/")
REALM = os.environ.get("KEYCLOAK_REALM", "membership-registry")
ADMIN_USER = os.environ.get("KC_ADMIN", "admin")
ADMIN_PASSWORD = os.environ.get("KC_ADMIN_PASSWORD", "admin")

CLIENT_ID = os.environ.get("CMS_CLIENT_ID", "cms-auth-proxy")
CLIENT_SECRET = os.environ.get("CMS_CLIENT_SECRET", "dev-secret-cms-auth-proxy")
REDIRECT_URI = os.environ.get("CMS_REDIRECT_URI", "http://localhost:8080/callback")
WEB_ORIGIN = os.environ.get("CMS_WEB_ORIGIN", "http://localhost:1313")
EDITOR_ROLE = os.environ.get("EDITOR_ROLE", "cms-editor")

# Realm roles. The first three match the production realm; cms-editor is the
# one this proxy gates on.
REALM_ROLES = [
    ("admin", "Administrator"),
    ("membership", "Guild member"),
    ("prodeko-external-member", "External member"),
    (EDITOR_ROLE, "May edit the website through Decap CMS"),
]

# Test users. The second one exists to prove the role check denies: it is a
# perfectly valid Prodeko account with no editor role, and it must not get in.
TEST_USERS = [
    {
        "email": "editor@prodeko.org",
        "password": "kananugetti",
        "first": "Erkki",
        "last": "Editor",
        "roles": ["membership", EDITOR_ROLE],
    },
    {
        "email": "member@prodeko.org",
        "password": "kananugetti",
        "first": "Meeri",
        "last": "Member",
        "roles": ["membership"],
    },
]


class Admin:
    """Minimal Keycloak Admin REST client over urllib."""

    def __init__(self, base_url: str, realm: str) -> None:
        self.base_url = base_url
        self.realm = realm
        self.token = ""

    # -- transport --

    def _request(self, method: str, url: str, body=None, form=None):
        data = None
        headers = {"Accept": "application/json"}
        if form is not None:
            data = urllib.parse.urlencode(form).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        elif body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read()
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    def _admin_url(self, path: str) -> str:
        return f"{self.base_url}/admin/realms/{self.realm}{path}"

    def get(self, path: str):
        return self._request("GET", self._admin_url(path))

    def post(self, path: str, body) -> bool:
        """POST; returns False on 409 Conflict instead of raising."""
        try:
            self._request("POST", self._admin_url(path), body=body)
            return True
        except urllib.error.HTTPError as e:
            if e.code == 409:
                return False
            raise RuntimeError(f"POST {path} -> {e.code}: {e.read().decode()}") from e

    def put(self, path: str, body) -> None:
        try:
            self._request("PUT", self._admin_url(path), body=body)
        except urllib.error.HTTPError as e:
            raise RuntimeError(f"PUT {path} -> {e.code}: {e.read().decode()}") from e

    # -- lifecycle --

    def wait_until_ready(self, timeout: int = 120) -> None:
        print(f"Waiting for Keycloak at {self.base_url} ...", flush=True)
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                self._request("GET", f"{self.base_url}/realms/master")
                print("Keycloak is up.", flush=True)
                return
            except (urllib.error.URLError, OSError):
                time.sleep(2)
        sys.exit(f"ERROR: Keycloak did not become ready within {timeout}s")

    def authenticate(self) -> None:
        body = self._request(
            "POST",
            f"{self.base_url}/realms/master/protocol/openid-connect/token",
            form={
                "client_id": "admin-cli",
                "username": ADMIN_USER,
                "password": ADMIN_PASSWORD,
                "grant_type": "password",
            },
        )
        self.token = body["access_token"]
        print("Authenticated against the admin API.", flush=True)

    def wait_for_realm(self, timeout: int = 60) -> None:
        """The realm arrives via --import-realm, slightly after Keycloak itself."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                self.get("")
                return
            except urllib.error.HTTPError as e:
                if e.code != 404:
                    raise
                time.sleep(2)
        sys.exit(f"ERROR: realm '{self.realm}' never appeared; is realm-export.json mounted?")


def configure_roles(kc: Admin) -> None:
    print("Realm roles:", flush=True)
    existing = {r["name"] for r in kc.get("/roles")}
    for name, description in REALM_ROLES:
        if name in existing:
            print(f"  {name}: already present", flush=True)
        else:
            kc.post("/roles", {"name": name, "description": description})
            print(f"  {name}: created", flush=True)


def realm_roles_mapper() -> dict:
    """The protocol mapper without which the role check denies everyone.

    Keycloak's built-in `roles` scope puts realm_access.roles in the ACCESS
    token only. The proxy reads the ID token, so the claim has to be added
    there explicitly. See membership-registry/docs/keycloak-clients.md section
    5 — production has to be configured the same way, by hand.
    """
    return {
        "name": "realm roles",
        "protocol": "openid-connect",
        "protocolMapper": "oidc-usermodel-realm-role-mapper",
        "config": {
            "multivalued": "true",
            "claim.name": "realm_access.roles",
            "jsonType.label": "String",
            "id.token.claim": "true",
            "access.token.claim": "true",
            "userinfo.token.claim": "false",
        },
    }


def client_payload() -> dict:
    return {
        "clientId": CLIENT_ID,
        "name": "Decap CMS editor login proxy",
        "enabled": True,
        "protocol": "openid-connect",
        # Confidential, standard flow only, PKCE S256, exact redirect URI.
        # Same shape as a production Prodeko client.
        "publicClient": False,
        "secret": CLIENT_SECRET,
        "standardFlowEnabled": True,
        "implicitFlowEnabled": False,
        "directAccessGrantsEnabled": False,
        "serviceAccountsEnabled": False,
        "redirectUris": [REDIRECT_URI],
        "webOrigins": [WEB_ORIGIN],
        "fullScopeAllowed": True,
        "attributes": {"pkce.code.challenge.method": "S256"},
        "protocolMappers": [realm_roles_mapper()],
    }


def configure_client(kc: Admin) -> None:
    print(f"Client '{CLIENT_ID}':", flush=True)
    query = urllib.parse.urlencode({"clientId": CLIENT_ID})
    found = [c for c in kc.get(f"/clients?{query}") if c["clientId"] == CLIENT_ID]
    payload = client_payload()
    if found:
        kc.put(f"/clients/{found[0]['id']}", payload)
        print("  updated", flush=True)
    else:
        kc.post("/clients", payload)
        print("  created", flush=True)
    print(f"  redirect URI: {REDIRECT_URI}", flush=True)
    print("  realm-roles mapper on realm_access.roles, in the ID token", flush=True)


def configure_users(kc: Admin) -> None:
    print("Test users:", flush=True)
    all_roles = {r["name"]: r for r in kc.get("/roles")}
    for spec in TEST_USERS:
        email = spec["email"]
        payload = {
            "username": email,
            "email": email,
            "firstName": spec["first"],
            "lastName": spec["last"],
            "enabled": True,
            "emailVerified": True,
        }
        query = urllib.parse.urlencode({"email": email, "exact": "true"})
        existing = kc.get(f"/users?{query}")
        if existing:
            user_id = existing[0]["id"]
            kc.put(f"/users/{user_id}", payload)
        else:
            kc.post("/users", payload)
            user_id = kc.get(f"/users?{query}")[0]["id"]
        kc.put(
            f"/users/{user_id}/reset-password",
            {"type": "password", "value": spec["password"], "temporary": False},
        )
        wanted = [all_roles[n] for n in spec["roles"] if n in all_roles]
        if wanted:
            kc.post(f"/users/{user_id}/role-mappings/realm", wanted)
        print(f"  {email} / {spec['password']}  roles: {', '.join(spec['roles'])}", flush=True)


def main() -> None:
    if "id.prodeko.org" in BASE_URL:
        sys.exit("REFUSING to run against production Keycloak. Configure it by hand.")

    kc = Admin(BASE_URL, REALM)
    kc.wait_until_ready()
    kc.authenticate()
    kc.wait_for_realm()

    configure_roles(kc)
    configure_client(kc)
    configure_users(kc)

    print("\nDev realm ready.", flush=True)
    # Deliberately not printed as "issuer": under the split-horizon compose
    # setup the `iss` claim is fixed by KC_HOSTNAME on the Keycloak service,
    # not by the URL this script happened to use.
    print(f"  configured via: {BASE_URL}/realms/{REALM}", flush=True)
    print(f"  client:         {CLIENT_ID} / {CLIENT_SECRET}", flush=True)
    print(f"  editor role:    {EDITOR_ROLE}", flush=True)


if __name__ == "__main__":
    main()
