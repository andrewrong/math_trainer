#!/bin/zsh

# A small local-only service helper. It deliberately uses nohup rather than
# launchd: it stays alive after this terminal closes, but does not start
# automatically after a Mac restart.

set -eu

ROOT_DIR=${0:A:h:h}
BINARY="$ROOT_DIR/bin/math_trainer"
CONFIG="$ROOT_DIR/config.yaml"
PID_FILE="${TMPDIR:-/tmp}/learning-compass.pid"
LOG_FILE="${TMPDIR:-/tmp}/learning-compass.log"

usage() {
  print "Usage: ${0:t} {start|stop|restart|status}"
}

is_running() {
  [[ -f "$PID_FILE" ]] || return 1

  local pid
  pid=$(<"$PID_FILE")
  [[ -n "$pid" ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1

  local command
  command=$(/bin/ps -p "$pid" -o command= 2>/dev/null || true)
  [[ "$command" == *"$BINARY"* ]]
}

clear_stale_pid() {
  if [[ -f "$PID_FILE" ]] && ! is_running; then
    /bin/rm -f "$PID_FILE"
  fi
}

start() {
  if is_running; then
    print "Learning Compass is already running (PID $(<"$PID_FILE"))."
    return
  fi
  clear_stale_pid

  if [[ ! -x "$BINARY" ]]; then
    print -u2 "Missing executable: $BINARY"
    print -u2 "Build it first with: mise run build"
    exit 1
  fi

  local account api_token
  account=$(/usr/bin/id -un)
  api_token=$(/usr/bin/security find-generic-password -a "$account" -s "LearningCompassAgentAPI" -w)

  (
    cd "$ROOT_DIR"
    /usr/bin/nohup /usr/bin/env MATHTRAINER_AUTOMATION_API_TOKEN="$api_token" MATHTRAINER_SERVER_HOST="127.0.0.1" \
      "$BINARY" -config "$CONFIG" </dev/null >>"$LOG_FILE" 2>&1 &
    print -r -- $! >"$PID_FILE"
  )

  sleep 1
  if ! is_running; then
    print -u2 "Learning Compass did not stay running. Check: $LOG_FILE"
    clear_stale_pid
    exit 1
  fi

  print "Learning Compass started (PID $(<"$PID_FILE"))."
  print "Local URL: http://127.0.0.1:8080"
  print "Log: $LOG_FILE"
}

stop() {
  if ! is_running; then
    clear_stale_pid
    print "Learning Compass is not running."
    return
  fi

  local pid
  pid=$(<"$PID_FILE")
  /bin/kill "$pid"
  /bin/rm -f "$PID_FILE"
  print "Learning Compass stopped."
}

status() {
  if is_running; then
    print "Learning Compass is running (PID $(<"$PID_FILE"))."
  else
    clear_stale_pid
    print "Learning Compass is not running."
    return 1
  fi
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  restart) stop; start ;;
  status) status ;;
  *) usage; exit 2 ;;
esac
