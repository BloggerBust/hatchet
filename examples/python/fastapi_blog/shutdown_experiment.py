# > Shutdown experiment
import asyncio
import tempfile
from pathlib import Path

from fastapi import BackgroundTasks, FastAPI

app = FastAPI()

MARKER_DIR = Path(tempfile.gettempdir()) / "fastapi-blog-markers"


async def three_second_task() -> None:
    (MARKER_DIR / "started").touch()
    await asyncio.sleep(3)
    (MARKER_DIR / "finished").touch()


@app.post("/experiment")
async def post__experiment(background_tasks: BackgroundTasks) -> dict[str, str]:
    MARKER_DIR.mkdir(exist_ok=True)
    background_tasks.add_task(three_second_task)

    return {"markers": str(MARKER_DIR)}


