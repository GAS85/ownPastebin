from fastapi import FastAPI, HTTPException, Request
from fastapi.staticfiles import StaticFiles
from app.templates import templates
from app.routes import router

app = FastAPI()

app.mount("/static", StaticFiles(directory="app/static"), name="static")
app.include_router(router)

@app.exception_handler(404)
async def custom_404_handler(request: Request, exc: HTTPException):
    return templates.TemplateResponse(
        name="index.html",
        context={
            "is_error": True,
            "is_editable": False,
            "is_created": False,
            "is_burned": False,
            "is_encrypted": False,
            "is_clone": False,
            "uri_prefix": "",
            "version": "1.0",
            "css_imports": [
                "static/prism.css",
                "static/bootstrap.min.css",
                "static/custom.css",
            ],
            "js_imports": [
                "static/prism.js",
                "static/jquery-3.6.0.min.js",
                "static/bootstrap.bundle.min.js",
                "static/crypto-js.min.js",
                "static/custom.js",
            ],
            "js_init": [],
        },
        request=request,
        status_code=404,
    )