"""
Connection settings for the PostgreSQL adapter: DSN parsing and redaction,
configuration validation, and the asyncpg connection arguments built from a
``Config``.
"""

from __future__ import annotations

import datetime
import re
import urllib.parse
from typing import Any

from storage.errors import InvalidConfigError, InvalidDSNError
from storage.types import Config

DEFAULT_PORT = 5432
SSL_MODES = ("disable", "allow", "prefer", "require", "verify-ca", "verify-full")
# Config's default ssl_mode leaves the choice to the DSN, then PGSSLMODE.
_DEFAULT_SSL_MODE = Config().ssl_mode

_URL_PREFIX = re.compile(r"(?P<scheme>[A-Za-z][A-Za-z0-9+.-]*)://")
_URL_SCHEMES = frozenset({"postgres", "postgresql"})
_PARAMETER_NAME = re.compile(r"[A-Za-z0-9_.-]+")
_DURATION_FIELDS = (
    "conn_max_lifetime",
    "conn_max_idle_time",
    "connect_timeout",
    "query_timeout",
    "tx_timeout",
)


def parse_dsn(dsn: str) -> Config:
    """Parse a PostgreSQL DSN into a ``Config`` whose fields hold its settings.

    The DSN is a URL (``postgres://`` or ``postgresql://``, percent-encoded) or a
    libpq ``key=value`` string (values may be single-quoted; ``\\`` escapes).
    ``host``, ``port``, ``dbname``, ``user``, ``password``, ``sslmode``,
    ``sslrootcert``, ``sslcert``, ``sslkey`` and ``connect_timeout`` (seconds; 0
    or less means no limit) fill the matching fields; other parameters go to
    ``Config.options``. ``Config.dsn`` is left empty. A DSN naming several hosts
    cannot be represented by the fields: set it as ``Config.dsn`` instead.

    Raises:
        InvalidDSNError: If the DSN is invalid. Messages never quote the DSN;
            ``context["dsn"]`` holds it with its passwords redacted.
    """
    config = Config()
    try:
        if not dsn.strip():
            raise InvalidDSNError("DSN is empty")
        for key, value in _parameters(dsn):
            _apply(config, key, value)
    except InvalidDSNError as exc:
        exc.with_context("dsn", redact_dsn(dsn))
        raise
    return config


def redact_dsn(dsn: str) -> str:
    """Return ``dsn`` with its passwords replaced by ``***``.

    Covers the password of a URL's user information, ``password`` and
    ``sslpassword`` URL query parameters and ``key=value`` settings. For a URL
    without ``@`` whose "port" is not a number (as in ``postgresql://user:secret/db``)
    that "port" is redacted too.
    """
    prefix = _URL_PREFIX.match(dsn)
    if prefix is None:
        return _KEY_VALUE_SECRET.sub(r"\g<key>***", dsn)
    head, rest = dsn[: prefix.end()], dsn[prefix.end() :]
    if "@" in rest:
        userinfo, _, after = rest.rpartition("@")
        if ":" in userinfo:
            rest = userinfo.partition(":")[0] + ":***@" + after
    else:
        authority = re.split(r"[/?#]", rest, maxsplit=1)[0]
        host, colon, port = authority.rpartition(":")
        if colon and not port.isdigit() and not port.endswith("]"):
            rest = host + ":***" + rest[len(authority) :]
    return head + _URL_SECRET.sub(r"\g<key>***", rest)


_URL_SECRET = re.compile(r"(?P<key>[?&](?:ssl)?password=)[^&#]*", re.IGNORECASE)
_KEY_VALUE_SECRET = re.compile(
    r"(?P<key>(?<![\w.-])(?:ssl)?password\s*=\s*)(?:'(?:[^'\\]|\\.)*'?|\S*)",
    re.IGNORECASE | re.DOTALL,
)


def validate_config(config: Config) -> None:
    """Validate ``config`` for the PostgreSQL adapter.

    Raises:
        InvalidConfigError: If a setting is invalid; ``InvalidDSNError`` if
            ``config.dsn`` is.
    """
    if config.dsn:
        _dsn_arguments(config.dsn)
    else:
        for name in ("host", "database", "username"):
            if not getattr(config, name):
                raise InvalidConfigError(f"{name} is required when dsn is not set")
    if config.port is not None and not 1 <= config.port <= 65535:
        raise InvalidConfigError("port must be between 1 and 65535")
    if config.max_open_conns < 1:
        raise InvalidConfigError("max_open_conns must be positive")
    if config.max_idle_conns < 0:
        raise InvalidConfigError("max_idle_conns cannot be negative")
    if config.max_idle_conns > config.max_open_conns:
        raise InvalidConfigError("max_idle_conns cannot exceed max_open_conns")
    for name in _DURATION_FIELDS:
        value = getattr(config, name)
        if value is not None and value <= datetime.timedelta(0):
            raise InvalidConfigError(f"{name} must be positive (None means no limit)")
    if config.ssl_mode not in SSL_MODES:
        raise InvalidConfigError(f"ssl_mode must be one of: {', '.join(SSL_MODES)}")
    _option_parameters(config.options)


def connect_arguments(config: Config) -> dict[str, Any]:
    """Return the asyncpg connection keyword arguments for a validated ``config``.

    - ``dsn``: a URL is passed on unchanged, except that its ``connect_timeout``
      parameter (which asyncpg would send to the server as a setting) is taken
      out; a ``key=value`` DSN is converted to the URL form
      ``postgresql://?key=value&...``. The host, port, database, username and
      password fields are not used.
    - Without a DSN, host, port (default 5432), database, username and, when
      set, password are passed as keyword arguments.
    - ``options`` are added to the URL query: asyncpg interprets the libpq
      connection parameters among them (``target_session_attrs``,
      ``passfile``, ...) and sends the others to the server as settings
      (``application_name``, ``search_path``, ...).
    - ``connect_timeout`` (or the DSN's) becomes ``timeout``; ``None`` is no limit.
    - SSL: an ``ssl_mode`` other than the default ``"prefer"`` is passed as
      ``sslmode``, and ``ssl_root_ca``, ``ssl_cert`` and ``ssl_key`` as
      ``sslrootcert``, ``sslcert`` and ``sslkey``, overriding the DSN's own.
      asyncpg builds the SSL context from them as libpq does: ``"disable"``
      never uses TLS, ``"require"`` refuses plaintext, ``"verify-ca"`` and
      ``"verify-full"`` verify the server certificate against ``ssl_root_ca``
      (else ``~/.postgresql/root.crt``), and ``"verify-full"`` its host name.
      With the default ``"prefer"`` the DSN's ``sslmode`` applies, else the
      ``PGSSLMODE`` environment variable, else ``prefer``.
    """
    kwargs: dict[str, Any] = {}
    if config.dsn:
        base, params = _dsn_arguments(config.dsn)
    else:
        base, params = "postgresql://", {}
        kwargs["host"] = config.host
        kwargs["port"] = config.port or DEFAULT_PORT
        kwargs["user"] = config.username
        kwargs["database"] = config.database
        if config.password:
            kwargs["password"] = config.password
    params.update(_option_parameters(config.options))
    timeout = config.connect_timeout
    if "connect_timeout" in params:
        timeout = _connect_timeout(params.pop("connect_timeout"))
    if config.ssl_mode != _DEFAULT_SSL_MODE:
        params["sslmode"] = config.ssl_mode
    for key, value in (
        ("sslrootcert", config.ssl_root_ca),
        ("sslcert", config.ssl_cert),
        ("sslkey", config.ssl_key),
    ):
        if value:
            params[key] = value
    if params or config.dsn:
        kwargs["dsn"] = base + ("?" + urllib.parse.urlencode(params) if params else "")
    kwargs["timeout"] = None if timeout is None else timeout.total_seconds()
    return kwargs


def _parameters(dsn: str) -> list[tuple[str, str]]:
    """The libpq parameters of a URL or key=value DSN, in order."""
    if _URL_PREFIX.match(dsn):
        return _url_parameters(_split_url(dsn))
    return _key_value_parameters(dsn)


def _dsn_arguments(dsn: str) -> tuple[str, dict[str, str]]:
    """Split a DSN into an asyncpg URL without query and its query parameters."""
    if not _URL_PREFIX.match(dsn):
        return "postgresql://", dict(_key_value_parameters(dsn))
    parts = _split_url(dsn)
    base = urllib.parse.urlunsplit((parts.scheme, parts.netloc, parts.path, "", ""))
    return base, dict(_query_parameters(parts.query))


def _split_url(dsn: str) -> urllib.parse.SplitResult:
    prefix = _URL_PREFIX.match(dsn)
    if prefix is not None and prefix["scheme"].lower() not in _URL_SCHEMES:
        raise InvalidDSNError(
            "DSN scheme must be postgres:// or postgresql://",
            context={"scheme": prefix["scheme"]},
        )
    try:
        return urllib.parse.urlsplit(dsn)
    except ValueError:  # e.g. an unterminated IPv6 address
        raise InvalidDSNError("malformed DSN URL") from None


def _url_parameters(parts: urllib.parse.SplitResult) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    userinfo, _, hostport = parts.netloc.rpartition("@")
    if userinfo:
        user, colon, password = userinfo.partition(":")
        params.append(("user", urllib.parse.unquote(user)))
        if colon:
            params.append(("password", urllib.parse.unquote(password)))
    if "," in hostport:
        raise InvalidDSNError(
            "parse_dsn does not support several hosts; set the DSN as Config.dsn"
        )
    host, port = _split_host_port(hostport)
    if host:
        params.append(("host", urllib.parse.unquote(host)))
    if port:
        params.append(("port", port))
    if len(parts.path) > 1:
        params.append(("dbname", urllib.parse.unquote(parts.path[1:])))
    params.extend(_query_parameters(parts.query))
    return params


def _split_host_port(hostport: str) -> tuple[str, str]:
    if hostport.startswith("["):  # [IPv6 address]
        host, bracket, rest = hostport[1:].partition("]")
        if not bracket or (rest and not rest.startswith(":")):
            raise InvalidDSNError("malformed host in DSN")
        return host, rest[1:]
    host, _, port = hostport.partition(":")
    return host, port


def _query_parameters(query: str) -> list[tuple[str, str]]:
    if not query:
        return []
    try:
        return urllib.parse.parse_qsl(
            query, keep_blank_values=True, strict_parsing=True
        )
    except ValueError:
        raise InvalidDSNError("malformed query string in DSN") from None


def _key_value_parameters(dsn: str) -> list[tuple[str, str]]:
    """Parse a libpq ``key=value`` connection string."""
    params: list[tuple[str, str]] = []
    i = 0
    n = len(dsn)
    while True:
        while i < n and dsn[i].isspace():
            i += 1
        if i >= n:
            return params
        start = i
        while i < n and dsn[i] != "=" and not dsn[i].isspace():
            i += 1
        key = dsn[start:i]
        while i < n and dsn[i].isspace():
            i += 1
        if i >= n or dsn[i] != "=" or not _PARAMETER_NAME.fullmatch(key):
            raise InvalidDSNError(
                f"DSN must be a URL or key=value settings (error at offset {start})"
            )
        i += 1
        while i < n and dsn[i].isspace():
            i += 1
        quoted = i < n and dsn[i] == "'"
        if quoted:
            i += 1
        value: list[str] = []
        while True:
            if i >= n:
                if quoted:
                    raise InvalidDSNError(f"unterminated quoted value for {key} in DSN")
                break
            char = dsn[i]
            if char == "\\" and i + 1 < n:
                value.append(dsn[i + 1])
                i += 2
                continue
            if (quoted and char == "'") or (not quoted and char.isspace()):
                i += 1
                break
            value.append(char)
            i += 1
        params.append((key, "".join(value)))


def _apply(config: Config, key: str, value: str) -> None:
    if key == "host":
        config.host = value
    elif key == "port":
        if not (value.isascii() and value.isdigit() and 1 <= int(value) <= 65535):
            raise InvalidDSNError("port in DSN must be a number between 1 and 65535")
        config.port = int(value)
    elif key in ("dbname", "database"):
        config.database = value
    elif key == "user":
        config.username = value
    elif key == "password":
        config.password = value
    elif key == "sslmode":
        if value not in SSL_MODES:
            raise InvalidDSNError(
                f"sslmode in DSN must be one of: {', '.join(SSL_MODES)}"
            )
        config.ssl_mode = value
    elif key == "sslrootcert":
        config.ssl_root_ca = value
    elif key == "sslcert":
        config.ssl_cert = value
    elif key == "sslkey":
        config.ssl_key = value
    elif key == "connect_timeout":
        config.connect_timeout = _connect_timeout(value)
    else:
        config.options[key] = value


def _connect_timeout(value: str) -> datetime.timedelta | None:
    """libpq's connect_timeout: whole seconds; 0 or less waits indefinitely."""
    try:
        seconds = int(value)
    except ValueError:
        raise InvalidDSNError("connect_timeout in DSN must be whole seconds") from None
    return datetime.timedelta(seconds=seconds) if seconds > 0 else None


def _option_parameters(options: dict[str, Any]) -> dict[str, str]:
    """Render ``Config.options`` as connection parameters."""
    params: dict[str, str] = {}
    for key, value in options.items():
        if not isinstance(key, str) or not _PARAMETER_NAME.fullmatch(key):
            raise InvalidConfigError(f"invalid connection option name {key!r}")
        if isinstance(value, bool):
            params[key] = "true" if value else "false"
        elif isinstance(value, str | int | float):
            params[key] = str(value)
        else:
            raise InvalidConfigError(
                f"connection option {key!r} must be a string, number or boolean"
            )
    return params
