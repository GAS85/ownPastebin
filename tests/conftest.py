# tests/conftest.py
import os
import tempfile
import pytest
from fastapi.testclient import TestClient

# Force SQLite for tests
os.environ.pop("REDIS_URL", None)
os.environ.pop("POSTGRES_URL", None)

# Use temp DB
db_fd, db_path = tempfile.mkstemp()
os.environ["SQLITE_PATH"] = db_path

from app.main import app  # import AFTER env setup


@pytest.fixture(scope="session")
def client():
    return TestClient(app)


@pytest.fixture(scope="session", autouse=True)
def cleanup():
    yield
    os.close(db_fd)
    os.unlink(db_path)