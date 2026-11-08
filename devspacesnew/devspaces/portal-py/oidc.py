"""oidc.py — validate bearer JWTs (RS256) from one OIDC issuer: Keycloak realm, Entra ID, Okta…
Mirrors portal/oidc.go. Uses PyJWT + cryptography when installed; without them bearer tokens are
refused with a clear message (session mode and -dev-user still work)."""
from __future__ import annotations

import json
import threading
import time
import urllib.request

try:
    import jwt  # PyJWT
    from jwt import PyJWKClient
except Exception:  # pragma: no cover - optional dependency
    jwt = None
    PyJWKClient = None


class OIDCVerifier:
    def __init__(self, issuer: str, audience: str, user_claim="preferred_username", groups_claim="groups", discovery_url=""):
        self.issuer = issuer.rstrip("/")
        self.discovery_url = discovery_url or self.issuer + "/.well-known/openid-configuration"
        self.audience = audience
        self.user_claim = user_claim or "preferred_username"
        self.groups_claim = groups_claim or "groups"
        self._jwks = None
        self._lock = threading.Lock()
        self._discovered = 0.0

    def available(self) -> bool:
        return jwt is not None

    def _client(self):
        with self._lock:
            if self._jwks is None or time.time() - self._discovered > 3600:
                with urllib.request.urlopen(self.discovery_url, timeout=10) as r:
                    disc = json.load(r)
                jwks = disc["jwks_uri"]
                internal = self.discovery_url.removesuffix("/.well-known/openid-configuration")
                if internal != self.issuer and jwks.startswith(self.issuer):
                    jwks = internal + jwks[len(self.issuer):]  # Keycloak advertises its public URL; reach it by the internal one
                self._jwks = PyJWKClient(jwks, cache_keys=True, lifespan=3600)
                self._discovered = time.time()
            return self._jwks

    def verify(self, token: str) -> dict:
        """Returns a user dict {name, short, groups, auth} or raises ValueError."""
        if jwt is None:
            raise ValueError("bearer tokens need PyJWT[crypto] (pip install -r requirements.txt)")
        try:
            key = self._client().get_signing_key_from_jwt(token).key
            claims = jwt.decode(token, key, algorithms=["RS256"], audience=self.audience, issuer=self.issuer,
                                options={"require": ["exp", "iss", "aud"]}, leeway=60)
        except jwt.ExpiredSignatureError:
            raise ValueError("token expired — sign in again")
        except jwt.InvalidAudienceError:
            raise ValueError(f"token audience is not {self.audience} (request a token for that scope / add an audience mapper in Keycloak)")
        except jwt.InvalidIssuerError:
            raise ValueError(f"token issuer is not {self.issuer}")
        except Exception as e:  # signature, kid, malformed …
            raise ValueError(f"invalid token: {e}")
        name = ""
        for c in (self.user_claim, "preferred_username", "email", "upn", "unique_name", "sub"):
            if claims.get(c):
                name = str(claims[c])
                break
        if not name:
            raise ValueError(f"no {self.user_claim} claim in token")
        groups = [str(g) for g in claims.get(self.groups_claim) or []]
        from kube import wscli
        return {"name": name, "short": wscli.sanitize_user(name), "groups": groups, "auth": "bearer"}
