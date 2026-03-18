import os

class Settings:
    REDIS_URL = os.getenv("REDIS_URL", "redis://redis:6379/0")
    BASE_URL = os.getenv("BASE_URL", "http://localhost:8000")
    DEFAULT_TTL = int(os.getenv("DEFAULT_TTL", "60"))
    SLUG_LEN = int(os.getenv("SLUG_LEN", "20"))
    MAX_PASTE_SIZE = int(os.getenv("MAX_PASTE_SIZE", 5 * 1024 * 1024))  # 5 MB default

settings = Settings()