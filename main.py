# /// script
# requires-python = ">=3.13"
# dependencies = [
#     "datastar-py",
#     "sanic",
# ]
# ///
import asyncio
import sanic
import os
import secrets

from datastar_py.sanic import (
    ServerSentEventGenerator as SSE,
    datastar_response,
    read_signals,
)
app = sanic.Sanic("Jotter")

HTML = '''<!doctype html>
<html lang="en">
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>Jotter</title>
        <script
            type="module"
            src="https://cdn.jsdelivr.net/gh/starfederation/datastar@main/bundles/datastar.js"
        ></script>
        <style>
            * {
                margin: 0;
                padding: 0;
                box-sizing: border-box;
            }

            html,
            body {
                height: 100%;
                width: 100%;
                overflow: hidden;
            }

            #jot-field {
                width: 100vw;
                height: 100vh;
                border: none;
                outline: none;
                resize: none;
                padding: 20px;
                font-family:
                    "SF Mono", Monaco, "Cascadia Code", "Roboto Mono",
                    Consolas, "Courier New", monospace;
                font-size: 16px;
                line-height: 1.5;
                background-color: #ffffff;
                color: #333333;
            }

            @media (prefers-color-scheme: dark) {
                #jot-field {
                    background-color: #333333;
                    color: #ffffff;
                }
            }

            #main-input:focus {
                outline: none;
            }
        </style>
    </head>
    <body data-on-load="@get('/updates')">
        <textarea
            id="jot-field"
            data-bind-jot
            data-on-input__debounce.400ms="@post('/write')"
            placeholder="Start typing..."
        >{{JOT}}</textarea>
    </body>
</html>'''

jot_secret_path = os.environ.get("JOT_SECRET_PATH", ".jotsecret")
base_url = os.environ.get("BASE_URL", "https://localhost:8000")
jot_file_path = os.environ.get("JOT_FILE_PATH", "jot.txt")

def get_or_create_token():
    """Get existing token or create a new one on first visit"""
    if not os.path.exists(jot_secret_path):
        jot_secret = secrets.token_urlsafe(32)
        with open(jot_secret_path, "w") as f:
            f.write(jot_secret)
        return jot_secret
    else:
        with open(jot_secret_path, "r") as f:
            return f.read()

def get_default_jot_content(token: str) -> str:
    """Generate default content with the current token"""
    return f'''Welcome to Jotter!

Make sure to save the link below, it's the only way to access this website:

{base_url}/?token={token}

If you want to "log out" of jotter, simply clear your browser's cookies.'''

@app.on_request
async def check_token(req: sanic.Request):
    query_token = req.args.get("token")
    cookie_token = req.cookies.get("token")

    # Get or create token (lazy initialization)
    jot_secret = get_or_create_token()

    if query_token and query_token == jot_secret:
        req.ctx.token = query_token
    elif cookie_token and cookie_token == jot_secret:
        req.ctx.token = cookie_token
    else:
        req.ctx.token = False
        raise sanic.Forbidden("Invalid token")


@app.on_response
async def save_token(request: sanic.Request, response: sanic.HTTPResponse):
    if request.ctx.token:
        response.add_cookie("token", request.ctx.token)

@app.get("/")
async def index(req: sanic.Request, ):
    # Initialize jot file with current token if it doesn't exist
    if not os.path.exists(jot_file_path):
        token = get_or_create_token()
        default_content = get_default_jot_content(token)
        with open(jot_file_path, "w") as f:
            f.write(default_content)

    if req.args.get("token"):
        return sanic.redirect("/")

    with open(jot_file_path, "r") as f:
        jot = f.read()
    return sanic.html(HTML.replace("{{JOT}}", jot))

@app.post("/write")
async def write(req: sanic.Request):
    try:
        signals = await read_signals(req)
        jot = signals.get("jot", "") if signals else ""

        with open(jot_file_path, "w") as f:
            f.write(jot)
        return sanic.HTTPResponse(status=201)
    except Exception as e:
        return sanic.HTTPResponse(f"Error: {str(e)}", status=500)

@app.get("/updates")
@datastar_response
async def updates(req: sanic.Request):
    last_modified = None

    # Send initial content
    with open(jot_file_path, "r") as f:
        jot = f.read()
    last_modified = os.path.getmtime(jot_file_path)
    yield SSE.patch_signals({"jot": jot})

    while True:
        try:
            current_modified = os.path.getmtime(jot_file_path)
            if current_modified != last_modified:
                with open(jot_file_path, "r") as f:
                    jot = f.read()
                yield SSE.patch_signals({"jot": jot})
                last_modified = current_modified
        except FileNotFoundError:
            # File was deleted, wait for it to be recreated
            pass

        await asyncio.sleep(0.1)  # Check every 100ms

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8000)
