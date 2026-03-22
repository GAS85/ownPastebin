import os

def parse_size(value: str) -> int | None:
    if value is None:
        return None

    value = value.strip().lower()

    if value.endswith("kb"):
        return int(value[:-2]) * 1024
    if value.endswith("mb"):
        return int(value[:-2]) * 1024 * 1024
    if value.endswith("gb"):
        return int(value[:-2]) * 1024 * 1024 * 1024

    return int(value)  # bytes

def parse_time(value: str) -> int | None:
    if value is None:
        return None

    value = value.strip().lower()

    # Hours
    if value.endswith("h"):
        return int(value[:-1]) * 3600
    # Days
    if value.endswith("d"):
        return int(value[:-1]) * 3600 * 24
    # Month
    if value.endswith("m"):
        return int(value[:-1]) * 3600 * 24 * 30

    return int(value)  # seconds


def parse_bool(value: str | None, default=False) -> bool:
    if value is None:
        return default
    return value.lower() in ("1", "true", "yes", "on")


class Settings:
    # STORAGE BACKENDS
    REDIS_URL = os.getenv("REDIS_URL")  # None = disabled
    POSTGRES_URL = os.getenv("POSTGRES_URL")  # None = disabled
    SQLITE_PATH = os.getenv("SQLITE_PATH", "/data/pastes.db")

    # APP SETTINGS
    BASE_URL = os.getenv("BASE_URL", "http://localhost:8000")

    DEFAULT_TTL = parse_time(os.getenv("DEFAULT_TTL", "0"))
    MAX_TTL = parse_time(os.getenv("MAX_TTL"))

    SLUG_LEN = int(os.getenv("SLUG_LEN", "20"))
    MAX_PASTE_SIZE = parse_size(os.getenv("MAX_PASTE_SIZE", "5MB"))

    # SECURITY
    SERVER_SIDE_ENCRYPTION_ENABLED = parse_bool(
        os.getenv("SERVER_SIDE_ENCRYPTION_ENABLED"), False
    )
    SERVER_SIDE_ENCRYPTION_KEY = os.getenv(
        "SERVER_SIDE_ENCRYPTION_KEY", ""
    )  # Must be 32 bytes for AES-256


settings = Settings()