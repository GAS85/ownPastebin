# tests/test_encryption.py
import os
from fastapi.testclient import TestClient
from app.main import app


def test_server_side_encryption():
    os.environ["SERVER_SIDE_ENCRYPTION_ENABLED"] = "true"
    os.environ["SERVER_SIDE_ENCRYPTION_KEY"] = "a" * 32  # simple test key

    client = TestClient(app)

    res = client.post("/", data=b"secret-data")
    assert res.status_code == 201

    paste_id = res.json()["id"]

    res = client.get(f"/raw/{paste_id}")
    assert res.text == "secret-data"