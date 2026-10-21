import logging
import os

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .db import Base, engine
from .routes_admin import router as admin_router
from .routes_sync import fail, router as sync_router

logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO"))

app = FastAPI(title="MenuVex Sync Service", version="2.0.0")
app.include_router(sync_router)
app.include_router(admin_router)


@app.on_event("startup")
def _startup():
    # Local-development convenience only. Production uses
    # migrations/001_init.sql (or Alembic) — never create_all against a
    # live PostgreSQL that other services own.
    if os.environ.get("SYNC_CREATE_TABLES") == "1":
        Base.metadata.create_all(engine)


@app.exception_handler(Exception)
async def unhandled(_: Request, exc: Exception):
    logging.exception("sync request failed")
    return fail(500, "internal", "internal sync error; safe to retry", "retry")


@app.get("/healthz")
def healthz():
    return {"success": True, "data": {"status": "ok"}, "error": None}
