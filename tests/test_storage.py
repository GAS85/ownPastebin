# tests/test_storage.py
from app.storage import save_paste, get_paste, delete_paste


def test_storage_save_get_delete():
    key = "test123"

    save_paste(key, {"content": "abc"}, ttl=0)

    data = get_paste(key)
    assert data["content"] == "abc"

    delete_paste(key)

    assert get_paste(key) is None


def test_storage_ttl():
    import time

    key = "ttl_test"

    save_paste(key, {"content": "temp"}, ttl=1)

    time.sleep(2)

    assert get_paste(key) is None


def test_storage_burn():
    from app.storage import get_and_delete_paste

    key = "burn_test"

    save_paste(key, {"content": "burn"}, ttl=0)

    data = get_and_delete_paste(key)
    assert data["content"] == "burn"

    assert get_paste(key) is None