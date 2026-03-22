# tests/test_api.py
import time


def test_create_paste(client):
    res = client.post("/", data=b"hello world")
    assert res.status_code == 201

    data = res.json()
    assert "id" in data
    assert "url" in data


def test_get_paste(client):
    res = client.post("/", data=b"hello")
    paste_id = res.json()["id"]

    res = client.get(f"/{paste_id}")
    assert res.status_code == 200
    assert "hello" in res.text


def test_raw_paste(client):
    res = client.post("/", data=b"raw content")
    paste_id = res.json()["id"]

    res = client.get(f"/raw/{paste_id}")
    assert res.status_code == 200
    assert res.text == "raw content"


def test_delete_paste(client):
    res = client.post("/", data=b"delete me")
    paste_id = res.json()["id"]

    res = client.delete(f"/{paste_id}")
    assert res.status_code == 200

    res = client.get(f"/{paste_id}")
    assert res.status_code == 404


def test_burn_after_read(client):
    res = client.post("/?burn=true", data=b"burn me")
    paste_id = res.json()["id"]

    # First read
    res = client.get(f"/{paste_id}")
    assert res.status_code == 200

    # Second read --> gone
    res = client.get(f"/{paste_id}")
    assert res.status_code == 404


def test_ttl_expiry(client):
    res = client.post("/?ttl=1", data=b"short lived")
    paste_id = res.json()["id"]

    time.sleep(2)

    res = client.get(f"/{paste_id}")
    assert res.status_code == 404


def test_large_payload_rejected(client):
    big_data = b"x" * (6 * 1024 * 1024)  # 6MB

    res = client.post("/", data=big_data)
    assert res.status_code == 413