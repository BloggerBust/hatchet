from types import TracebackType

from fastapi import BackgroundTasks, FastAPI
from pydantic import BaseModel

from hatchet_sdk import Context, Hatchet


class Session:
    ## simulate async db session
    async def __aenter__(self) -> "Session":
        return self

    async def __aexit__(
        self,
        type_: type[BaseException] | None,
        value: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        pass


class User(BaseModel):
    id: int
    email: str


async def get_user(db: Session, user_id: int) -> User:
    return User(id=user_id, email="test@example.com")


async def create_user(db: Session) -> User:
    return User(id=1, email="test@example.com")


async def send_welcome_email(email: str) -> None:
    print(f"Sending welcome email to {email}")


app = FastAPI()
hatchet = Hatchet()


# > FastAPI Background Tasks
async def send_welcome_email_task_bg(user_id: int) -> None:
    async with Session() as db:
        user = await get_user(db, user_id)

        await send_welcome_email(user.email)


@app.post("/background-tasks/user")
async def post__create_user__background_tasks(
    background_tasks: BackgroundTasks,
) -> User:
    async with Session() as db:
        user = await create_user(db)

        background_tasks.add_task(send_welcome_email_task_bg, user.id)

        return user




# > Hatchet Task
class WelcomeEmailInput(BaseModel):
    user_id: int


@hatchet.task(input_validator=WelcomeEmailInput)
async def send_welcome_email_task_hatchet(
    input: WelcomeEmailInput, _ctx: Context
) -> None:
    async with Session() as db:
        user = await get_user(db, input.user_id)

        await send_welcome_email(user.email)


@app.post("/hatchet/user")
async def post__create_user__hatchet() -> dict[str, int | str]:
    async with Session() as db:
        user = await create_user(db)

        ref = await send_welcome_email_task_hatchet.aio_run(
            WelcomeEmailInput(user_id=user.id),
            wait_for_result=False,
        )

        return {"id": user.id, "welcome_email_run_id": ref.workflow_run_id}


