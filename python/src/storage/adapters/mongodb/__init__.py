"""
MongoDB adapter for the Database Agnostic Storage Library.

This package provides a complete MongoDB implementation using Motor
for high-performance async operations.
"""

from storage.adapters.mongodb.adapter import MongoDBAdapter
from storage.adapters.mongodb.connection import MongoDBStorage

__all__ = [
    "MongoDBAdapter",
    "MongoDBStorage",
]
