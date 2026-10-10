# > Worker
from examples.fastapi_blog.trigger import hatchet, send_welcome_email_task_hatchet


def main() -> None:
    worker = hatchet.worker(
        "fastapi-blog-worker", workflows=[send_welcome_email_task_hatchet]
    )
    worker.start()


if __name__ == "__main__":
    main()

