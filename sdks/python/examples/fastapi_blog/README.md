## FastAPI background tasks and Hatchet

The example behind the article on FastAPI background tasks. `trigger.py` is
a FastAPI app with the same signup operation implemented two ways, at two
paths: `/background-tasks/user` hands the welcome email to FastAPI's
`BackgroundTasks`, and `/hatchet/user` hands it to Hatchet. `worker.py` is
the Hatchet worker that runs the Hatchet version.

## Prerequisites

1. [Python 3.10 or higher](https://www.python.org/downloads/)
2. [Docker](https://docs.docker.com/engine/install/), for the local Hatchet server
3. The [Hatchet CLI](https://docs.hatchet.run/reference/cli)

## Setup

1. Start a local Hatchet server. The CLI creates a `local` profile with a
   client token for it.

```bash
hatchet server start
```

2. From the `sdks/python` directory, create a virtual environment and
   install the dependencies.

```bash
python -m venv .venv && source .venv/bin/activate
pip install hatchet-sdk fastapi uvicorn
```

## Running the example

The worker and the FastAPI app each need their own terminal. Open each one
in the `sdks/python` directory, activate the virtual environment, and load
the `local` profile's `HATCHET_CLIENT_` variables:

```bash
source .venv/bin/activate
eval "$(hatchet profile env --name local)"
```

1. Start the Hatchet worker.

```bash
python -m examples.fastapi_blog.worker
```

2. In a second terminal, start the FastAPI app.

```bash
uvicorn examples.fastapi_blog.trigger:app
```

3. In a third terminal, call the `BackgroundTasks` version. The response
   comes back first; "Sending welcome email" then prints in the uvicorn
   terminal, because the task runs in the web process after the response.

```bash
curl -X POST http://127.0.0.1:8000/background-tasks/user
```

4. Call the Hatchet version. The response carries the run id, and "Sending
   welcome email" prints in the worker terminal instead.

```bash
curl -X POST http://127.0.0.1:8000/hatchet/user
```

5. Inspect the run by id with the CLI or the dashboard at
   http://localhost:8888.

```bash
hatchet runs get <welcome_email_run_id>
```

## Running the shutdown experiment

`shutdown_experiment.py` is the app behind the article's shutdown table. Its
`POST /experiment` route hands `BackgroundTasks` a function that touches a
`started` marker file, sleeps three seconds, and touches a `finished` marker.
The response body names the marker directory, normally
`/tmp/fastapi-blog-markers`. Hatchet is not involved; only the virtual
environment from setup step 2 is needed.

1. In the `sdks/python` directory, with the virtual environment activated,
   clear old markers and start the app. uvicorn prints its process id in
   the line `Started server process [1234]`; the signals below go to that
   id.

```bash
rm -rf /tmp/fastapi-blog-markers
uvicorn examples.fastapi_blog.shutdown_experiment:app
```

2. In a second terminal, call the route and, within three seconds, send
   the signal for the case you want. Wait for uvicorn to finish shutting
   down, then list the markers.

```bash
curl -X POST http://127.0.0.1:8000/experiment
kill -TERM 1234
```

After uvicorn has exited, inspect the marker directory:

```bash
ls /tmp/fastapi-blog-markers
```

Repeat from step 1 for each case:

- Default `SIGTERM`, as above. uvicorn logs "Waiting for background tasks to
  complete", exits about three seconds after the signal, and both markers
  are present.
- Request limit. Start with `uvicorn examples.fastapi_blog.shutdown_experiment:app --limit-max-requests 1`
  and send no signal. The one request makes uvicorn log "Maximum request
  limit of 1 exceeded. Terminating process.", wait for the task, and exit
  with both markers present.
- Graceful deadline. Start with `uvicorn examples.fastapi_blog.shutdown_experiment:app --timeout-graceful-shutdown 1`
  and send `kill -TERM`. One second later uvicorn logs "Cancel 1 running
  task(s), timeout graceful shutdown exceeded" and exits; only `started`
  is present.
- `SIGKILL`. Send `kill -KILL 1234` instead. The process dies at once with
  nothing more logged; only `started` is present.
