from fastapi import APIRouter, Request, HTTPException
from fastapi.responses import HTMLResponse, PlainTextResponse, Response
from urllib.parse import parse_qs
from nanoid import generate
from app.storage import save_paste, get_paste, delete_paste
from app.config import settings
import html

router = APIRouter()

def build_url(paste_id):
    return f"{settings.BASE_URL}/{paste_id}"

@router.post("/")
async def create(request: Request):
    content_type = request.headers.get("content-type", "").lower()

    max_size = settings.MAX_PASTE_SIZE

    # ✅ Pre-check via Content-Length (if provided)
    content_length = request.headers.get("content-length")
    if content_length:
        try:
            if int(content_length) > max_size:
                raise HTTPException(413, "Paste too large")
        except ValueError:
            pass  # ignore malformed header

    content = ""
    raw_bytes = b""

    # ✅ MULTIPART (curl -F)
    if "multipart/form-data" in content_type:
        form = await request.form()
        file = form.get("content")

        if hasattr(file, "read"):  # UploadFile
            raw_bytes = await file.read()
        else:
            raw_bytes = str(file or "").encode()

    # ✅ FORM URLENCODED
    elif "application/x-www-form-urlencoded" in content_type:
        form = await request.form()
        content = form.get("content", "")
        raw_bytes = content.encode()

    # ✅ JSON
    elif "application/json" in content_type:
        data = await request.json()
        content = data.get("content", "")
        raw_bytes = content.encode()

    # ✅ RAW (curl --data-binary)
    else:
        raw_bytes = await request.body()

    # ✅ Final size check (authoritative)
    if len(raw_bytes) > max_size:
        raise HTTPException(413, "Paste too large")

    # Decode once after validation
    if not content:
        content = raw_bytes.decode(errors="replace")

    if not content:
        raise HTTPException(400, "Empty paste")

    # 🔍 debug (optional)
    print("CONTENT LENGTH:", len(content))
    print("CONTENT PREVIEW:", repr(content[:100]))

    ttl = int(request.query_params.get("ttl", 0))
    burn = request.query_params.get("burn") == "true"

    paste_id = generate(size=settings.SLUG_LEN)

    save_paste(paste_id, {
        "content": content,
        "burn": burn
    }, ttl)

    return build_url(paste_id)

@router.get("/{paste_id}", response_class=HTMLResponse)
async def view(paste_id: str, request: Request):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        # delete_paste(paste_id)
        # Delete in Redis
        paste = r.execute_command("GETDEL", paste_id)

    return f"<pre>{html.escape(paste['content'])}</pre>"
    # return f"{paste['content']}"

@router.get("/raw/{paste_id}", response_class=PlainTextResponse)
async def raw(paste_id: str):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        delete_paste(paste_id)

    return paste["content"]

@router.get("/download/{paste_id}")
async def download(paste_id: str):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        delete_paste(paste_id)

    return Response(
        content=paste["content"],
        media_type="application/octet-stream",
        headers={"Content-Disposition": f"attachment; filename={paste_id}.txt"}
    )

@router.delete("/{paste_id}")
async def delete(paste_id: str):
    delete_paste(paste_id)
    return {"status": "ok"}