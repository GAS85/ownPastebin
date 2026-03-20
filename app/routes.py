from fastapi import APIRouter, Request, HTTPException, Response
from fastapi.responses import HTMLResponse, JSONResponse, PlainTextResponse
from fastapi.templating import Jinja2Templates
from nanoid import generate
from app.storage import save_paste, get_paste, delete_paste, get_and_delete_paste
from app.config import settings
from app.crypto import encrypt, decrypt
import html
import base64

templates = Jinja2Templates(directory="app/templates")

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

    # Enforce MAX_TTL rules:
    # - If requested_ttl is None → use MAX_TTL
    # - If requested_ttl > MAX_TTL → clamp to MAX_TTL
    # - Otherwise → use requested_ttl
def resolve_ttl(requested_ttl: int | None) -> int:
    max_ttl = getattr(settings, "MAX_TTL", None)

    # No TTL provided
    if requested_ttl is None:
        return max_ttl or 0

    # Clamp if MAX_TTL exists
    if max_ttl is not None:
        return min(requested_ttl, max_ttl)

    return requested_ttl

# CONFIG ENDPOINT
@router.get("/config")
async def get_config():
    return {
        "max_ttl": settings.MAX_TTL or 0,
        "default_ttl": settings.DEFAULT_TTL,
        "max_paste_size": settings.MAX_PASTE_SIZE,
        "server_side_encryption": settings.SERVER_SIDE_ENCRYPTION_ENABLED,
    }

# RAW (binary-safe)

@router.get("/raw/{paste_id}")
async def raw(paste_id: str):
    paste = fetch_paste(paste_id)
    data = decode_from_storage(paste["content"])

    try:
        text = data.decode("utf-8")
        # It's text show in browser
        return Response(
            content=text,
            media_type="text/plain; charset=utf-8",
        )
    except UnicodeDecodeError:
        # Binary force download
        return Response(
            content=data,
            media_type="application/octet-stream",
            headers={
                "Content-Disposition": f"attachment; filename={paste_id}"
            },
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

@router.get("/", response_class=HTMLResponse)
async def new_paste(request: Request):
    return templates.TemplateResponse(
        "index.html",
        {
            "request": request,
            "is_editable": True,
            "is_created": False,
            "is_burned": False,
            "is_error": False,
            "is_encrypted": False,
            "uri_prefix": "",
            "pastebin_code": "",
            "version": "1.0",
            "css_imports": [
                "/static/prism.css",
                "/static/bootstrap.min.css",
                "/static/custom.css",
            ],
            "js_imports": [
                "/static/prism.js",
                "/static/jquery-3.6.0.min.js",
                "/static/bootstrap.bundle.min.js",
                "/static/crypto-js.min.js",
                "/static/custom.js",
            ],
            "js_init": [],
            "ui_expiry_default": "1d",
            "ui_expiry_times": [
                ("Never", "0"),
                ("5 min", "300"),
                ("10 min", "600"),
                ("1 hour", "3600"),
                ("1 day", "86400"),
                ("1 week", "604800"),
                ("1 month", "18144000"),
                ("1 year", "220752000"),
            ],
            "level": request.query_params.get("level"),
            "glyph": request.query_params.get("glyph"),
            "msg": request.query_params.get("msg"),
            "url": request.query_params.get("url"),
        },
    )

# VIEW (HTML)
@router.get("/{paste_id}", response_class=HTMLResponse)
async def view(paste_id: str, request: Request):
    paste = fetch_paste(paste_id)

    data = decode_from_storage(paste["content"])

    try:
        text = data.decode()
    except UnicodeDecodeError:
        text = "[binary data]"

    return templates.TemplateResponse(
        "index.html",  # reuse your existing template
        {
            "request": request,
            "is_editable": False,
            "is_created": True,
            "is_burned": paste.get("burn", False),
            "is_error": False,
            "is_encrypted": paste.get("e2e_encrypted", False),
            "uri_prefix": "",
            "pastebin_code": text,
            "pastebin_id": paste_id,
            "pastebin_cls": f"language-{paste.get('lang', 'text')}",
            "version": "1.0",
            "css_imports": [
                "/static/prism.css",
                "/static/bootstrap.min.css",
                "/static/custom.css",
            ],
            "js_imports": [
                "/static/prism.js",
                "/static/jquery-3.6.0.min.js",
                "/static/bootstrap.bundle.min.js",
                "/static/crypto-js.min.js",
                "/static/custom.js",
            ],
            "js_init": [],
            "ui_expiry_default": "1d",
            "ui_expiry_times": [
                ("Never", "0"),
                ("5 min", "300"),
                ("10 min", "600"),
                ("1 hour", "3600"),
                ("1 day", "86400"),
                ("1 week", "604800"),
                ("1 month", "18144000"),
                ("1 year", "220752000"),
            ]
        },
    )

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

    # Handle TTL, do not allow garbage values (negative, zero, non-integer)
    ttl_param = request.query_params.get("ttl")

    try:
        ttl = int(ttl_param) if ttl_param else None
    except ValueError:
        raise HTTPException(400, "Invalid TTL")

    if ttl is not None and ttl < 0:
        raise HTTPException(400, "TTL must be >= 0")

    ttl = resolve_ttl(ttl)

    burn = request.query_params.get("burn") == "true"

    lang = request.query_params.get("lang") or "text"

    e2e_encrypted = request.query_params.get("encrypted") == "true"

    paste_id = generate(size=settings.SLUG_LEN)

    save_paste(
        paste_id,
        {
            "content": stored_content,
            "burn": burn,
            "encrypted": settings.SERVER_SIDE_ENCRYPTION_ENABLED,
            "e2e_encrypted": e2e_encrypted,
            "lang": lang,
        },
        ttl,
    )

    url = build_url(paste_id)

    return JSONResponse(
        status_code=201,
        headers={"Location": url},
        content={"url": url, "id": paste_id, "lang": lang},
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

# DELETE
@router.delete("/{paste_id}")
async def delete(paste_id: str):
    delete_paste(paste_id)
    return {
        "url": f"/?level=info&msg=Paste deleted successfully"
    }