import base64
from cryptography.fernet import Fernet
from app.config import settings


# Create Fernet only if enabled
if settings.SERVER_SIDE_ENCRYPTION_ENABLED:
    fernet = Fernet(settings.SERVER_SIDE_ENCRYPTION_KEY)
else:
    fernet = None

# Encrypt (accepts bytes, returns bytes)
def encrypt(data: bytes) -> bytes:
    if not fernet:
        raise RuntimeError("Encryption is disabled")
    return fernet.encrypt(data)

# Decrypt (accepts bytes, returns bytes)
def decrypt(token: bytes) -> bytes:
    if not fernet:
        raise RuntimeError("Encryption is disabled")
    return fernet.decrypt(token)