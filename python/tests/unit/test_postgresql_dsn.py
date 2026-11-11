"""Unit tests for PostgreSQL DSN parsing and redaction, configuration validation
and the asyncpg connection arguments (including SSL)."""

from __future__ import annotations

import datetime
import urllib.parse
from pathlib import Path
from typing import Any

import pytest

from storage.adapters.postgresql import PostgreSQLAdapter, PostgreSQLStorage
from storage.adapters.postgresql.dsn import (
    connect_arguments,
    parse_dsn,
    redact_dsn,
    validate_config,
)
from storage.errors import (
    ConnectionFailedError,
    InvalidConfigError,
    InvalidDSNError,
    ValidationError,
)
from storage.types import Config

SECRET = "s3cr3t-pw"


def _query(kwargs: dict[str, Any]) -> dict[str, str]:
    """The query parameters of the asyncpg DSN in ``kwargs``."""
    return dict(urllib.parse.parse_qsl(urllib.parse.urlsplit(kwargs["dsn"]).query))


def _config(**changes: Any) -> Config:
    values: dict[str, Any] = {"host": "db", "database": "app", "username": "ann"}
    values.update(changes)
    return Config(**values)


class TestParseURL:
    def test_fields(self) -> None:
        config = parse_dsn(
            "postgresql://ann:pw@db.example.com:6543/app"
            "?sslmode=verify-full&sslrootcert=/ca.pem&sslcert=/c.crt&sslkey=/c.key"
            "&connect_timeout=7&application_name=svc"
        )
        assert (config.host, config.port, config.database) == (
            "db.example.com",
            6543,
            "app",
        )
        assert (config.username, config.password) == ("ann", "pw")
        assert config.ssl_mode == "verify-full"
        assert (config.ssl_root_ca, config.ssl_cert, config.ssl_key) == (
            "/ca.pem",
            "/c.crt",
            "/c.key",
        )
        assert config.connect_timeout == datetime.timedelta(seconds=7)
        assert config.options == {"application_name": "svc"}
        assert config.dsn == ""

    def test_percent_encoding_is_decoded(self) -> None:
        config = parse_dsn(
            "postgres://us%40er:p%40ss%3Aw%2Frd@%2Fvar%2Frun%2FPG/my%20db"
        )
        assert config.username == "us@er"
        assert config.password == "p@ss:w/rd"
        assert config.host == "/var/run/PG"  # case is kept for socket paths
        assert config.database == "my db"

    def test_ipv6_host_and_defaults(self) -> None:
        config = parse_dsn("postgresql://[::1]:5433")
        assert (config.host, config.port, config.database) == ("::1", 5433, "")
        bare = parse_dsn("postgresql://")
        assert (bare.host, bare.port, bare.username) == ("localhost", None, "")

    def test_query_parameters_override_the_url(self) -> None:
        config = parse_dsn("postgresql://a@h1/db1?user=b&host=h2&dbname=db2&port=7")
        assert (config.username, config.host, config.database, config.port) == (
            "b",
            "h2",
            "db2",
            7,
        )

    def test_connect_timeout_of_zero_means_no_limit(self) -> None:
        assert parse_dsn("postgresql://h/d?connect_timeout=0").connect_timeout is None


class TestParseKeyValue:
    def test_fields(self) -> None:
        config = parse_dsn(
            "host=db port=5433 dbname=app user=ann password=pw sslmode=require "
            "target_session_attrs=read-write"
        )
        assert (config.host, config.port, config.database) == ("db", 5433, "app")
        assert (config.username, config.password, config.ssl_mode) == (
            "ann",
            "pw",
            "require",
        )
        assert config.options == {"target_session_attrs": "read-write"}

    def test_quoted_values_and_escapes(self) -> None:
        config = parse_dsn("host = db  password='a b \\' c\\\\'  dbname=x\\ y user=''")
        assert config.password == "a b ' c\\"
        assert config.database == "x y"
        assert config.host == "db"
        assert config.username == ""


@pytest.mark.parametrize(
    "dsn",
    [
        "",
        "   ",
        "just-a-word",
        "host=db port",
        "=value",
        f"host=db password='{SECRET}",
        "host=db port=abc",
        "host=db port=70000",
        "host=db sslmode=sometimes",
        "host=db connect_timeout=1.5",
        f"mysql://admin:{SECRET}@db/prod",
        f"postgresql://ann:{SECRET}@h1:5432,h2:5432/db",
        f"postgresql://ann:{SECRET}@[::1/db",
        f"postgresql://ann:{SECRET}@db:port/app",
        f"postgresql://ann@db/app?password={SECRET}&bad",
    ],
)
def test_invalid_dsns_raise_invalid_dsn_without_quoting_the_secret(dsn: str) -> None:
    with pytest.raises(InvalidDSNError) as caught:
        parse_dsn(dsn)
    error = caught.value
    assert isinstance(error, ValidationError)
    assert SECRET not in str(error)
    assert SECRET not in repr(error)
    assert SECRET not in str(error.context)


def test_a_password_misread_as_port_is_not_quoted() -> None:
    dsn = f"postgresql://ann:{SECRET}/app"  # '@host' forgotten
    with pytest.raises(InvalidDSNError) as caught:
        parse_dsn(dsn)
    assert SECRET not in str(caught.value) + str(caught.value.context)


def test_adapter_parse_dsn_delegates() -> None:
    assert PostgreSQLAdapter().parse_dsn("host=db dbname=app").database == "app"


@pytest.mark.parametrize(
    ("dsn", "expected"),
    [
        (f"postgresql://ann:{SECRET}@db/app", "postgresql://ann:***@db/app"),
        (
            f"postgres://ann:{SECRET}@db/app?password={SECRET}&x=1",
            "postgres://ann:***@db/app?password=***&x=1",
        ),
        (
            f"postgresql://db/app?user=ann&sslpassword={SECRET}",
            "postgresql://db/app?user=ann&sslpassword=***",
        ),
        (f"postgresql://ann:{SECRET}/app", "postgresql://ann:***/app"),
        ("postgresql://ann@db:5432/app", "postgresql://ann@db:5432/app"),
        (f"host=db password={SECRET} user=ann", "host=db password=*** user=ann"),
        (
            f"host=db password = '{SECRET} x' user=ann",
            "host=db password = *** user=ann",
        ),
        (f"host=db sslpassword={SECRET}", "host=db sslpassword=***"),
        ("host=db mypassword=x", "host=db mypassword=x"),
    ],
)
def test_redact_dsn(dsn: str, expected: str) -> None:
    assert redact_dsn(dsn) == expected


class TestValidateConfig:
    def test_valid_configs(self) -> None:
        validate_config(_config())
        validate_config(Config(dsn="postgresql://h1:5432,h2:5432/app"))
        validate_config(Config(dsn="host=db dbname=app"))
        validate_config(
            _config(query_timeout=None, tx_timeout=None, connect_timeout=None)
        )

    @pytest.mark.parametrize(
        "changes",
        [
            {"host": ""},
            {"database": ""},
            {"username": ""},
            {"port": 0},
            {"port": 65536},
            {"max_open_conns": 0},
            {"max_idle_conns": -1},
            {"max_open_conns": 2, "max_idle_conns": 3},
            {"query_timeout": datetime.timedelta(0)},
            {"tx_timeout": datetime.timedelta(seconds=-1)},
            {"conn_max_lifetime": datetime.timedelta(0)},
            {"conn_max_idle_time": datetime.timedelta(0)},
            {"connect_timeout": datetime.timedelta(0)},
            {"ssl_mode": "sometimes"},
            {"options": {"bad key": "x"}},
            {"options": {"search_path": ["a"]}},
        ],
    )
    def test_invalid_configs(self, changes: dict[str, Any]) -> None:
        with pytest.raises(InvalidConfigError):
            validate_config(_config(**changes))

    def test_invalid_dsn(self) -> None:
        with pytest.raises(InvalidDSNError):
            validate_config(Config(dsn="mysql://db/app"))

    def test_adapter_validate_config_delegates(self) -> None:
        with pytest.raises(InvalidConfigError):
            PostgreSQLAdapter().validate_config(_config(max_open_conns=0))


class TestConnectArguments:
    def test_fields(self) -> None:
        kwargs = connect_arguments(_config(password="pw"))
        assert kwargs == {
            "host": "db",
            "port": 5432,
            "user": "ann",
            "database": "app",
            "password": "pw",
            "timeout": 10.0,
        }

    def test_empty_password_is_not_passed_and_no_timeout_is_none(self) -> None:
        kwargs = connect_arguments(_config(port=6000, connect_timeout=None))
        assert "password" not in kwargs
        assert kwargs["port"] == 6000
        assert kwargs["timeout"] is None

    def test_default_ssl_mode_is_left_to_asyncpg(self) -> None:
        kwargs = connect_arguments(_config())
        assert "ssl" not in kwargs
        assert "dsn" not in kwargs

    def test_ssl_disable_is_honoured(self) -> None:
        kwargs = connect_arguments(_config(ssl_mode="disable"))
        assert "ssl" not in kwargs
        assert _query(kwargs) == {"sslmode": "disable"}

    def test_certificate_fields_are_passed(self) -> None:
        kwargs = connect_arguments(
            _config(
                ssl_mode="verify-full",
                ssl_root_ca="/etc/ca.pem",
                ssl_cert="/etc/client.crt",
                ssl_key="/etc/client.key",
            )
        )
        assert _query(kwargs) == {
            "sslmode": "verify-full",
            "sslrootcert": "/etc/ca.pem",
            "sslcert": "/etc/client.crt",
            "sslkey": "/etc/client.key",
        }

    def test_dsn_ssl_mode_is_kept_with_the_default_ssl_mode(self) -> None:
        dsn = "postgresql://ann:pw@db/app?sslmode=verify-full"
        assert connect_arguments(Config(dsn=dsn)) == {"dsn": dsn, "timeout": 10.0}

    def test_explicit_ssl_settings_override_the_dsn(self) -> None:
        kwargs = connect_arguments(
            Config(dsn="postgresql://db/app?sslmode=disable", ssl_mode="require")
        )
        assert kwargs["dsn"] == "postgresql://db/app?sslmode=require"

    def test_dsn_connect_timeout_is_taken_out(self) -> None:
        kwargs = connect_arguments(
            Config(dsn="postgresql://db/app?connect_timeout=3&application_name=svc")
        )
        assert kwargs == {
            "dsn": "postgresql://db/app?application_name=svc",
            "timeout": 3.0,
        }

    def test_key_value_dsn_is_converted_to_a_url(self) -> None:
        kwargs = connect_arguments(
            Config(
                dsn="host=db port=5433 dbname=app user=ann password='a b' "
                "connect_timeout=0"
            )
        )
        assert kwargs["dsn"].startswith("postgresql://?")
        assert _query(kwargs) == {
            "host": "db",
            "port": "5433",
            "dbname": "app",
            "user": "ann",
            "password": "a b",
        }
        assert kwargs["timeout"] is None

    def test_options_become_connection_parameters(self) -> None:
        kwargs = connect_arguments(
            _config(options={"application_name": "svc", "jit": False, "work_mem": 64})
        )
        assert _query(kwargs) == {
            "application_name": "svc",
            "jit": "false",
            "work_mem": "64",
        }

    def test_multi_host_dsn_is_passed_through(self) -> None:
        dsn = "postgresql://ann@h1:5432,h2:5433/app"
        assert connect_arguments(Config(dsn=dsn))["dsn"] == dsn

    def test_parsed_dsn_round_trips(self) -> None:
        config = parse_dsn(
            "postgresql://ann:p%40ss@db:6000/app?sslmode=require&search_path=x"
        )
        kwargs = connect_arguments(config)
        assert kwargs["password"] == "p@ss"
        assert (kwargs["host"], kwargs["port"], kwargs["database"]) == (
            "db",
            6000,
            "app",
        )
        assert _query(kwargs) == {"search_path": "x", "sslmode": "require"}


async def test_ssl_root_ca_is_loaded_before_connecting(tmp_path: Path) -> None:
    """asyncpg builds the SSL context from ssl_root_ca: a missing file fails the
    connection at once, before any network access (127.0.0.1:1 is closed)."""
    missing = tmp_path / "missing-ca.pem"
    config = _config(
        host="127.0.0.1",
        port=1,
        ssl_mode="verify-full",
        ssl_root_ca=str(missing),
        max_idle_conns=1,
    )
    with pytest.raises(ConnectionFailedError) as caught:
        await PostgreSQLAdapter().connect(config)
    assert isinstance(caught.value.__cause__, FileNotFoundError)


async def test_ssl_client_certificate_is_loaded_before_connecting(
    tmp_path: Path,
) -> None:
    config = _config(
        host="127.0.0.1",
        port=1,
        ssl_mode="require",
        ssl_cert=str(tmp_path / "missing.crt"),
        max_idle_conns=1,
    )
    with pytest.raises(ConnectionFailedError) as caught:
        await PostgreSQLStorage(config).connect()
    assert isinstance(caught.value.__cause__, FileNotFoundError)


async def test_connection_errors_do_not_quote_the_password() -> None:
    config = _config(host="127.0.0.1", port=1, password=SECRET, max_idle_conns=1)
    with pytest.raises(ConnectionFailedError) as caught:
        await PostgreSQLAdapter().connect(config)
    assert SECRET not in str(caught.value) + str(caught.value.context)
    assert caught.value.context["host"] == "127.0.0.1"


async def test_connection_errors_redact_the_dsn() -> None:
    config = Config(dsn=f"postgresql://ann:{SECRET}@127.0.0.1:1/app", max_idle_conns=1)
    with pytest.raises(ConnectionFailedError) as caught:
        await PostgreSQLAdapter().connect(config)
    assert caught.value.context["dsn"] == "postgresql://ann:***@127.0.0.1:1/app"
    assert SECRET not in str(caught.value)
