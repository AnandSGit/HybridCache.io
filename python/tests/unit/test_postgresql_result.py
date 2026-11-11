"""Unit tests for PostgreSQL rows and results."""

from __future__ import annotations

import dataclasses
from collections.abc import Iterator
from typing import Any

import pytest

from storage.adapters.postgresql.result import PostgreSQLResult, PostgreSQLRow
from storage.errors import DataError, InvalidDataError


class FakeRecord:
    """The part of the ``asyncpg.Record`` interface the adapter relies on."""

    def __init__(self, **values: Any) -> None:
        self._values = values

    def keys(self) -> Iterator[str]:
        return iter(self._values.keys())

    def values(self) -> Iterator[Any]:
        return iter(self._values.values())

    def items(self) -> Iterator[tuple[str, Any]]:
        return iter(self._values.items())

    def get(self, key: str, default: Any = None) -> Any:
        return self._values.get(key, default)

    def __getitem__(self, key: str | int) -> Any:
        if isinstance(key, int):
            return list(self._values.values())[key]
        return self._values[key]

    def __contains__(self, key: object) -> bool:
        return key in self._values

    def __len__(self) -> int:
        return len(self._values)

    def __iter__(self) -> Iterator[Any]:
        return iter(self._values.values())


@dataclasses.dataclass
class User:
    id: int = 0
    name: str = ""
    email: str = "unset"


@dataclasses.dataclass(frozen=True)
class FrozenUser:
    id: int = 0


def _row() -> PostgreSQLRow:
    return PostgreSQLRow(FakeRecord(id=1, name="Ann"))


def test_row_accessors() -> None:
    row = _row()
    assert row.get("name") == "Ann"
    assert row.get("missing") is None
    assert row.get("missing", 7) == 7
    assert row.keys() == ["id", "name"]
    assert row.values() == [1, "Ann"]
    assert row.items() == [("id", 1), ("name", "Ann")]
    assert row["name"] == "Ann"
    assert row[0] == 1
    assert "id" in row
    assert "other" not in row
    assert len(row) == 2
    assert list(row) == [1, "Ann"]


async def test_scan_replaces_the_contents_of_list_destinations() -> None:
    user_id: list[Any] = []
    name: list[Any] = ["old", "values"]
    await _row().scan(user_id, name)
    assert user_id == [1]
    assert name == ["Ann"]


@pytest.mark.parametrize("count", [0, 1, 3])
async def test_scan_needs_one_destination_per_column(count: int) -> None:
    destinations: list[list[Any]] = [[] for _ in range(count)]
    with pytest.raises(InvalidDataError, match="scan needs 2 destinations"):
        await _row().scan(*destinations)


async def test_scan_rejects_destinations_that_are_not_lists_and_changes_nothing() -> (
    None
):
    first: list[Any] = ["unchanged"]
    with pytest.raises(InvalidDataError, match="destination 1 must be a list"):
        await _row().scan(first, {})
    assert first == ["unchanged"]
    with pytest.raises(DataError):
        await _row().scan(first, 5)


async def test_scan_into_a_dataclass_sets_matching_fields() -> None:
    user = User()
    await _row().scan_into(user)
    assert user == User(id=1, name="Ann", email="unset")


async def test_scan_into_an_object_sets_existing_attributes_only() -> None:
    class Plain:
        def __init__(self) -> None:
            self.name = ""

    obj = Plain()
    await _row().scan_into(obj)
    assert obj.name == "Ann"
    assert not hasattr(obj, "id")


@pytest.mark.parametrize("target", [User, FrozenUser()])
async def test_scan_into_rejects_classes_and_frozen_instances(target: Any) -> None:
    with pytest.raises(InvalidDataError):
        await _row().scan_into(target)


def _result(count: int) -> PostgreSQLResult:
    return PostgreSQLResult([FakeRecord(n=i) for i in range(count)])


async def test_result_iteration() -> None:
    result = _result(3)
    assert [row.get("n") async for row in result] == [0, 1, 2]
    assert await result.fetchone() is None
    assert len(result) == 3
    assert result
    assert result.columns() == ["n"]


async def test_result_fetching() -> None:
    result = _result(5)
    first = await result.fetchone()
    assert first is not None
    assert first.get("n") == 0
    assert [row.get("n") for row in await result.fetchmany(2)] == [1, 2]
    assert await result.fetchmany(0) == []
    assert await result.fetchmany(-1) == []
    assert [row.get("n") for row in await result.fetchall()] == [3, 4]
    assert await result.fetchall() == []
    assert await result.fetchmany() == []


async def test_closed_and_empty_results() -> None:
    result = _result(2)
    await result.close()
    assert await result.fetchone() is None
    assert await result.fetchall() == []
    assert len(result) == 0
    assert not result
    assert result.columns() == []
    empty = _result(0)
    assert empty.columns() == []
    assert [row async for row in empty] == []
