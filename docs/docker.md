# First deploy of the service

The service already runs from the owner's old checkout. `crew` deploys it as a docker service:
it checks out the deployed commit into `<service dir>/app/`, tags the image with the short
commit and runs `docker compose -p math_trainer --project-directory <service dir>/app`. It
hands `compose.yaml` three variables and nothing else:

- `CREW_SERVICE_DIR`: the service directory; `compose.yaml` mounts only its `data/` and
  `config/`;
- `CREW_SECRETS`: the directory of service secrets, outside the service directory;
- `CREW_IMAGE`: the image of the release.

Placeholders: `<checkout>` is the owner's old checkout of this repository, `<service dir>` is
the directory of the service, `<secrets dir>` is the directory of service secrets. The service
directory holds no secret. Type the lines in order.

## 1. Stop the old process

```sh
kill -TERM $(lsof -ti tcp:8080 -sTCP:LISTEN)
while lsof -ti tcp:8080 -sTCP:LISTEN >/dev/null; do sleep 1; done
ls -l <checkout>/math_trainer.db-wal   # must not exist, or be empty
```

SIGTERM lets SQLite write the WAL back into the database.

## 2. Back up the database

Keep the dated copy outside the service directory.

```sh
cp <checkout>/math_trainer.db <backup dir>/math_trainer.db.$(date +%Y%m%d)
```

## 3. Create the directories and copy the files

```sh
mkdir -p <service dir>/data/{db,tts,reading,logs} <service dir>/config
cp <checkout>/math_trainer.db <service dir>/data/db/
cp -R <checkout>/web/audio/tts/. <service dir>/data/tts/
cp -R <checkout>/resources/reading/. <service dir>/data/reading/
cp <checkout>/config.yaml <service dir>/config/
cp <checkout>/.secrets/google_adc.json <secrets dir>/math_trainer.google_adc.json
```

## 4. Edit `config/config.yaml`

In `<service dir>/config/config.yaml` change:

- `db.path` to `/app/data/math_trainer.db`;
- `server.host` to `0.0.0.0`;
- every `127.0.0.1` or `localhost` base URL (OCR, ollama, lmstudio) to `host.docker.internal`;
- every non-empty key or token: move it to `<secrets dir>/math_trainer.env`, one `NAME=value`
  line each, and set its value to `""` in `config.yaml`.

Each name is `MATHTRAINER_` plus the config path in capitals with `.` as `_` (the bare names
such as `OPENAI_API_KEY` do not work under this prefix). For example:

- `MATHTRAINER_AI_PROVIDERS_ANTHROPIC_API_KEY`, `MATHTRAINER_AI_PROVIDERS_OPENAI_API_KEY`,
  `MATHTRAINER_AI_PROVIDERS_GEMINI_API_KEY`, `MATHTRAINER_AI_PROVIDERS_OPENROUTER_API_KEY`;
- `MATHTRAINER_AUTOMATION_API_TOKEN`;
- `MATHTRAINER_READING_OCR_API_KEY`.

The key must stay in `config.yaml` (as `""`) for the variable to take effect.

Check that no secret is left; every value shown must be empty (or the placeholder `lm-studio`):

```sh
grep -nE 'api_key|api_token' <service dir>/config/config.yaml
```

## 5. Count, deploy, check, count again

```sh
sqlite3 <service dir>/data/db/math_trainer.db ".tables"
```

Pick a few tables from that list and count their rows:

```sh
sqlite3 <service dir>/data/db/math_trainer.db "select count(*) from <table>;"
crew cap --apply math_trainer deploy
curl -fsS http://127.0.0.1:8080/api/v1/health
sqlite3 <service dir>/data/db/math_trainer.db "select count(*) from <table>;"
```

The counts before and after the deploy must be equal.

## 6. Keep the old database, or roll back

Keep the old database in `<checkout>` (and the backup of step 2) until the owner has used the
page. Rollback before the first deploy has a previous release: stop the container, then start
the old binary as before:

```sh
docker compose -p math_trainer down
```
