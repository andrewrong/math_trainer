# Running the service with docker compose

Placeholders: `<checkout>` is the git checkout of this repository, `<service dir>` is a new
directory that holds everything the container writes, `<secrets dir>` is the existing directory
where the secrets of all services live, outside `<service dir>`. The service directory holds no
secret. Type the lines in order.

## 1. Stop the running process

```sh
kill -TERM $(lsof -ti tcp:8080 -sTCP:LISTEN)
while lsof -ti tcp:8080 -sTCP:LISTEN >/dev/null; do sleep 1; done
ls -l <checkout>/math_trainer.db-wal   # must not exist, or be empty
```

SIGTERM lets SQLite write the WAL back into the database.

## 2. Back up the database

Keep the copy outside the service directory.

```sh
cp <checkout>/math_trainer.db <backup dir>/math_trainer.db.$(date +%Y%m%d)
```

## 3. Create the service directory and copy the files

```sh
mkdir -p <service dir>/{data,tts,reading,logs}
cp <checkout>/math_trainer.db <service dir>/data/
cp -R <checkout>/web/audio/tts/. <service dir>/tts/
cp -R <checkout>/resources/reading/. <service dir>/reading/
cp <checkout>/config.yaml <service dir>/config.yaml
cp <checkout>/.secrets/google_adc.json <secrets dir>/math_trainer.google_adc.json
```

## 4. Edit the copied `config.yaml`

In `<service dir>/config.yaml` change:

- `db.path` to `/app/data/math_trainer.db`;
- `server.host` to `0.0.0.0`;
- every `127.0.0.1` or `localhost` base URL (OCR, ollama, lmstudio) to `host.docker.internal`.

Then move every non-empty secret value of the copied `config.yaml` (API keys, tokens) to
`<secrets dir>/math_trainer.env`, one `NAME=value` line each, and set the value to `""` in the
copied `config.yaml`. The names:

- `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`;
- `LEARNING_COMPASS_API_TOKEN`;
- for any other key, `MATHTRAINER_` plus its config path in capitals with `.` as `_`, for
  example `MATHTRAINER_AI_PROVIDERS_OPENROUTER_API_KEY` or `MATHTRAINER_READING_OCR_API_KEY`.
  The key must stay in `config.yaml` (as `""`) for the variable to take effect.

Check that no secret is left in the copied file; every value shown must be empty (or the
placeholder `lm-studio`):

```sh
grep -nE 'api_key|api_token' <service dir>/config.yaml
```

## 5. Count, start, check, count again

```sh
sqlite3 <service dir>/data/math_trainer.db ".tables"
```

Pick a few tables from that list and count their rows:

```sh
sqlite3 <service dir>/data/math_trainer.db "select count(*) from <table>;"
cd <checkout>
MATH_TRAINER_HOME=<service dir> MATH_TRAINER_ENV_FILE=<secrets dir>/math_trainer.env MATH_TRAINER_GOOGLE_ADC=<secrets dir>/math_trainer.google_adc.json docker compose up -d --build
curl -fsS http://127.0.0.1:8080/api/v1/health
sqlite3 <service dir>/data/math_trainer.db "select count(*) from <table>;"
```

The counts before and after the start must be equal.

## 6. Clean up, or roll back

Only after the owner has used the page, remove the old database copy from the checkout:

```sh
rm -f <checkout>/math_trainer.db <checkout>/math_trainer.db-wal <checkout>/math_trainer.db-shm
```

Rollback: `docker compose down`. If the old database was already removed from the checkout,
restore it from the dated backup of step 2 before starting the old binary, otherwise the old
binary creates a fresh, empty database:

```sh
cp <backup dir>/math_trainer.db.<date> <checkout>/math_trainer.db
```

Then start the old binary as before. Keep the backup of step 2 until you are sure you will not
roll back. The other old files were not changed.
