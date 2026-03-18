from fastapi import APIRouter, Request, HTTPException, Response
from fastapi.responses import HTMLResponse, JSONResponse, PlainTextResponse
from nanoid import generate
from app.storage import save_paste, get_paste, delete_paste, get_and_delete_paste
from app.config import settings
from app.crypto import encrypt, decrypt
import html
import json
import base64

router = APIRouter()

def build_url(paste_id):
    return f"{settings.BASE_URL}/{paste_id}"

# Helper: Encode before storing
def encode_for_storage(raw_bytes: bytes) -> str:
    if settings.SERVER_SIDE_ENCRYPTION_ENABLED:
        # Fernet returns base64-safe bytes
        return encrypt(raw_bytes).decode()
    else:
        # Plain mode → base64 encode
        return base64.b64encode(raw_bytes).decode()

# Helper: Decode after retrieving
def decode_from_storage(stored_str: str) -> bytes:
    data = stored_str.encode()

    if settings.SERVER_SIDE_ENCRYPTION_ENABLED:
        return decrypt(data)
    else:
        return base64.b64decode(data)

# CREATE PASTE
@router.post("/")
async def create(request: Request):
    max_size = settings.MAX_PASTE_SIZE

    raw_body = await request.body()

    if len(raw_body) > max_size:
        raise HTTPException(413, "Paste too large")

    if not raw_body:
        raise HTTPException(400, "Empty paste")

    # Store as encrypted OR base64
    stored_content = encode_for_storage(raw_body)

    ttl = int(request.query_params.get("ttl", 0))
    burn = request.query_params.get("burn") == "true"

    paste_id = generate(size=settings.SLUG_LEN)

    save_paste(
        paste_id,
        {
            "content": stored_content,
            "burn": burn,
            "encrypted": settings.SERVER_SIDE_ENCRYPTION_ENABLED,
        },
        ttl,
    )

    url = build_url(paste_id)

    return JSONResponse(
        status_code=201,
        headers={"Location": url},
        content={"url": url, "id": paste_id},
    )

# INTERNAL FETCH (handles burn)
def fetch_paste(paste_id: str):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        paste = get_and_delete_paste(paste_id)
        if not paste:
            raise HTTPException(404)

    return paste

# VIEW (HTML)
@router.get("/{paste_id}", response_class=HTMLResponse)
async def view(paste_id: str):
    paste = fetch_paste(paste_id)

    data = decode_from_storage(paste["content"])

    try:
        text = data.decode()
    except UnicodeDecodeError:
        text = "[binary data]"

    return f"<pre>{html.escape(text)}</pre>"

# RAW (binary-safe)
@router.get("/raw/{paste_id}", response_class=PlainTextResponse)
async def raw(paste_id: str):
    paste = fetch_paste(paste_id)

    data = decode_from_storage(paste["content"])

    return Response(
        content=data,
        media_type="application/octet-stream",
    )

# DOWNLOAD
@router.get("/download/{paste_id}")
async def download(paste_id: str):
    paste = fetch_paste(paste_id)

    data = decode_from_storage(paste["content"])

    return Response(
        content=data,
        media_type="application/octet-stream",
        headers={"Content-Disposition": f"attachment; filename={paste_id}"},
    )

# DELETE
@router.delete("/{paste_id}")
async def delete(paste_id: str):
    delete_paste(paste_id)
    return {"status": "ok"}