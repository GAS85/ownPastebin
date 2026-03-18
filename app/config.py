import os

class Settings:
    REDIS_URL = os.getenv("REDIS_URL", "redis://redis:6379/0")
    BASE_URL = os.getenv("BASE_URL", "http://localhost:8000")
    DEFAULT_TTL = int(os.getenv("DEFAULT_TTL", "60"))
    _max_ttl = os.getenv("MAX_TTL")
    MAX_TTL = int(_max_ttl) if _max_ttl is not None else None
    SLUG_LEN = int(os.getenv("SLUG_LEN", "20"))
    MAX_PASTE_SIZE = int(os.getenv("MAX_PASTE_SIZE", 5 * 1024 * 1024))  # 5 MB default
    SERVER_SIDE_ENCRYPTION_ENABLED = bool(os.getenv("SERVER_SIDE_ENCRYPTION_ENABLED", "false").lower() == "true")
    SERVER_SIDE_ENCRYPTION_KEY = os.getenv("SERVER_SIDE_ENCRYPTION_KEY", "")  # Must be 32 bytes for AES-256

settings = Settings()