# tests/test_concurrency.py
from concurrent.futures import ThreadPoolExecutor
from fastapi.testclient import TestClient
from app.main import app

client = TestClient(app)


def create_paste(i):
    res = client.post("/", content=f"data-{i}".encode())
    return res.status_code


def test_concurrent_requests():
    with ThreadPoolExecutor(max_workers=10) as executor:
        results = list(executor.map(create_paste, range(50)))

    assert all(code == 201 for code in results)