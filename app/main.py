from __future__ import annotations

import logging
from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles

from app.api.routes import router
from app.core.config import get_settings
from app.scheduler.runner import ProbeScheduler
from app.services.probe_service import ProbeService
from app.storage import database

logging.basicConfig(level=logging.INFO)


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()
    database.init_engine(settings)
    await database.init_db()
    assert database.SessionLocal is not None
    service = ProbeService(settings, database.SessionLocal)
    scheduler = ProbeScheduler(service, settings.probe.interval_seconds)
    app.state.settings = settings
    app.state.probe_service = service
    app.state.scheduler = scheduler
    scheduler.start()
    try:
        yield
    finally:
        await scheduler.stop()
        await service.manager.stop()


app = FastAPI(title="Proxy Check", version="0.1.0", lifespan=lifespan)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)
app.include_router(router)


static_dir = Path(get_settings().app.static_dir)
assets_dir = static_dir / "assets"
if assets_dir.exists():
    app.mount("/assets", StaticFiles(directory=assets_dir), name="assets")


@app.get("/{full_path:path}", include_in_schema=False)
async def serve_frontend(full_path: str):
    index_path = static_dir / "index.html"
    if index_path.exists():
        return FileResponse(index_path)
    return {
        "message": "Proxy Check API is running. Build frontend/ to enable the dashboard.",
        "api": "/api",
    }
