"""
MongoDB adapter for the Database Agnostic Storage Library, on PyMongo's native
asyncio API (``pymongo.AsyncMongoClient``).

MongoDB cannot execute SQL: queries and commands must be built with the query
builder, whose structured ``spec`` is translated into MongoDB operations.
"""

from storage.adapters.mongodb.adapter import MongoDBAdapter
from storage.adapters.mongodb.connection import MongoDBStorage
from storage.adapters.mongodb.result import MongoDBResult, MongoDBRow
from storage.adapters.mongodb.transaction import MongoDBTransaction

__all__ = [
    "MongoDBAdapter",
    "MongoDBResult",
    "MongoDBRow",
    "MongoDBStorage",
    "MongoDBTransaction",
]
