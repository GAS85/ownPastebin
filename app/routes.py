from fastapi import APIRouter, Request, HTTPException, Response, status
from fastapi.responses import HTMLResponse, JSONResponse, PlainTextResponse, Response
from urllib.parse import parse_qs
from nanoid import generate
from app.storage import save_paste, get_paste, delete_paste, get_and_delete_paste
from app.config import settings
import html
import json
from app.crypto import encrypt, decrypt

router = APIRouter()

def build_url(paste_id):
    return f"{settings.BASE_URL}/{paste_id}"

@router.post("/")
async def create(request: Request):
    max_size = settings.MAX_PASTE_SIZE

    # Read body exactly once
    raw_body = await request.body()

    # Enforce size limit immediately
    if len(raw_body) > max_size:
        raise HTTPException(413, "Paste too large")

    content_type = request.headers.get("content-type", "").lower()

    content = ""

    # JSON
    if "application/json" in content_type:
        try:
            import json
            data = json.loads(raw_body)
            content = data.get("content", "")
        except:
            pass

    # Form (only if truly form)
    elif "application/x-www-form-urlencoded" in content_type:
        from urllib.parse import parse_qs
        parsed = parse_qs(raw_body.decode())
        content = parsed.get("content", [""])[0]

    # Multipart
    elif "multipart/form-data" in content_type:
        form = await request.form()
        file = form.get("content")
        if hasattr(file, "read"):
            content = (await file.read()).decode(errors="replace")

    # RAW fallback (THIS is what curl --data-binary uses)
    if not content:
        content = raw_body.decode(errors="replace")

    if not content:
        raise HTTPException(400, "Empty paste")

    # 🔍 debug (optional)
    #print("CONTENT LENGTH:", raw_body.__len__())
    #print("CONTENT PREVIEW:", repr(content[:100]))


    # ✅ encrypt ONLY final content
    if settings.SERVER_SIDE_ENCRYPTION_ENABLED:
        content = encrypt(content)

    ttl = int(request.query_params.get("ttl", 0))
    burn = request.query_params.get("burn") == "true"

    paste_id = generate(size=settings.SLUG_LEN)

    save_paste(paste_id, {
        "content": content,
        "burn": burn,
        "encrypted": settings.SERVER_SIDE_ENCRYPTION_ENABLED
    }, ttl)

    # Return URL in Location header and body
    url = build_url(paste_id)
    return JSONResponse(
        status_code=201,
        headers={"Location": url},
        content={"url": url, "id": paste_id},
    )

@router.get("/{paste_id}", response_class=HTMLResponse)
async def view(paste_id: str):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        paste = get_and_delete_paste(paste_id)
        if not paste:
            raise HTTPException(404)

    if settings.SERVER_SIDE_ENCRYPTION_ENABLED and paste.get("encrypted"):
        paste["content"] = decrypt(paste["content"])

    return f"<pre>{html.escape(paste['content'])}</pre>"

@router.get("/raw/{paste_id}", response_class=PlainTextResponse)
async def raw(paste_id: str):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        paste = get_and_delete_paste(paste_id)
        if not paste:
            raise HTTPException(404)

    if settings.SERVER_SIDE_ENCRYPTION_ENABLED and paste.get("encrypted"):
        paste["content"] = decrypt(paste["content"])

    return paste["content"]

@router.get("/download/{paste_id}")
async def download(paste_id: str):
    paste = get_paste(paste_id)
    if not paste:
        raise HTTPException(404)

    if paste.get("burn"):
        delete_paste(paste_id)

    if settings.SERVER_SIDE_ENCRYPTION_ENABLED and paste.get("encrypted"):
        paste["content"] = decrypt(paste["content"])

    return Response(
        content=paste["content"],
        media_type="application/octet-stream",
        headers={"Content-Disposition": f"attachment; filename={paste_id}.txt"}
    )

@router.delete("/{paste_id}")
async def delete(paste_id: str):
    delete_paste(paste_id)
    return {"status": "ok"}