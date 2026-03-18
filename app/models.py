from pydantic import BaseModel
from typing import Optional

class PasteCreate(BaseModel):
    content: str
    lang: Optional[str] = "markup"
    ttl: Optional[int] = 0
    burn: Optional[bool] = False
    encrypted: Optional[bool] = False