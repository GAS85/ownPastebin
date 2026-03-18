from cryptography.fernet import Fernet
from app.config import settings

fernet = None

if settings.SERVER_SIDE_ENCRYPTION_ENABLED:
    if not settings.SERVER_SIDE_ENCRYPTION_KEY:
        raise RuntimeError("SERVER_SIDE_ENCRYPTION_ENABLED=true but SERVER_SIDE_ENCRYPTION_KEY is missing")

    fernet = Fernet(settings.SERVER_SIDE_ENCRYPTION_KEY.encode())

def encrypt(text: str) -> str:
    if not fernet:
        return text
    return fernet.encrypt(text.encode()).decode()

def decrypt(token: str) -> str:
    if not fernet:
        return token
    return fernet.decrypt(token.encode()).decode()