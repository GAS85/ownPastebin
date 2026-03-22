# tests/test_backends.py
import os
import pytest
from app.storage import get_storage

# To enable Redis/Postgres tests locally
# Redis
#export TEST_REDIS_URL=redis://localhost:6379/0

# Postgres
#export TEST_POSTGRES_URL=postgresql://user:pass@localhost:5432/testdb


def backend_available(name):
    if name == "redis":
        return os.getenv("TEST_REDIS_URL")
    if name == "postgres":
        return os.getenv("TEST_POSTGRES_URL")
    return True


@pytest.mark.parametrize("backend", ["sqlite", "redis", "postgres"])
def test_backends_basic(backend):
    if backend == "redis":
        url = os.getenv("TEST_REDIS_URL")
        if not url:
            pytest.skip("Redis not configured")
        os.environ["REDIS_URL"] = url
        os.environ.pop("POSTGRES_URL", None)

    elif backend == "postgres":
        url = os.getenv("TEST_POSTGRES_URL")
        if not url:
            pytest.skip("Postgres not configured")
        os.environ["POSTGRES_URL"] = url
        os.environ.pop("REDIS_URL", None)

    else:  # sqlite
        os.environ.pop("REDIS_URL", None)
        os.environ.pop("POSTGRES_URL", None)

    storage = get_storage()

    key = f"test-{backend}"

    storage.save(key, {"content": backend}, ttl=0)

    data = storage.get(key)
    assert data["content"] == backend

    storage.delete(key)
    assert storage.get(key) is None