import os

def parse_size(value: str) -> int:
    if value is None:
        return None

    value = value.strip().lower()

    if value.endswith("mb"):
        return int(value[:-2]) * 1024 * 1024
    if value.endswith("kb"):
        return int(value[:-2]) * 1024
    if value.endswith("gb"):
        return int(value[:-2]) * 1024 * 1024 * 1024

    return int(value)  # bytes

def parse_time(value: str) -> int:
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

class Settings:
    REDIS_URL = os.getenv("REDIS_URL", "redis://redis:6379/0")
    BASE_URL = os.getenv("BASE_URL", "http://localhost:8000")
    DEFAULT_TTL = parse_time(os.getenv("DEFAULT_TTL", "0"))
    MAX_TTL = parse_time(os.getenv("MAX_TTL"))
    SLUG_LEN = int(os.getenv("SLUG_LEN", "20"))
    MAX_PASTE_SIZE = parse_size(os.getenv("MAX_PASTE_SIZE", "5MB"))
    SERVER_SIDE_ENCRYPTION_ENABLED = bool(os.getenv("SERVER_SIDE_ENCRYPTION_ENABLED", "false").lower() == "true")
    SERVER_SIDE_ENCRYPTION_KEY = os.getenv("SERVER_SIDE_ENCRYPTION_KEY", "")  # Must be 32 bytes for AES-256

settings = Settings()