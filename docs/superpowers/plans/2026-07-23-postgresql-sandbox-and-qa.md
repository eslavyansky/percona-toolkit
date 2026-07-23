# PostgreSQL Sandbox + QA Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a native-binary PostgreSQL sandbox (`sandbox-pg/`) that starts standalone and replicated instances from pre-defined config, loads the `pagila` dataset, and cleans up — then wire `pt-pg-summary` integration tests onto it (no Docker), add phase-1 structural tests, a CI job, and developer docs.

**Architecture:** Bash scripts mirroring the MySQL sandbox (`sandbox/`): `start-sandbox`/`stop-sandbox`/`test-env` render placeholder templates into `${TMP_DIR}/<port>/` and drive `initdb`/`pg_ctl`/`pg_basebackup`/`psql`. Go tests read connection params from env and assert on collected content against `pagila`. CI installs PostgreSQL 17, runs the harness, tears it down.

**Tech Stack:** Bash, PostgreSQL 13–17 binaries (via `PT_PG_SANDBOX_BASEDIR`), Go (`pt-pg-summary`, `lib/pginfo`), GitHub Actions.

## Global Constraints

- New code lives ONLY in the new top-level `sandbox-pg/` and in `src/go/pt-pg-summary/`, plus `.github/workflows/`, `CONTRIBUTING.md`, and the `percona-toolkit-dev-setup` skill. The existing MySQL sandbox (`sandbox/`) MUST NOT be modified.
- Default ports: source `15432`, replicas `15433`, `15434`.
- Required env: `PERCONA_TOOLKIT_BRANCH` (repo root), `PT_PG_SANDBOX_BASEDIR` (PostgreSQL install dir containing `bin/initdb`, `bin/pg_ctl`, `bin/postgres`, `bin/psql`, `bin/pg_basebackup`, `bin/pg_isready`). No PATH auto-detection.
- `TMP_DIR` defaults to `/tmp` (`TMP_DIR=${TMP_DIR:-/tmp}`) and MUST be honored everywhere — no hardcoded `/tmp` in any rendered path.
- Superuser `postgres` / password `root`; replication role `replicator` / password `root`.
- Placeholder tokens in templates: `PORT`, `TMP_DIR`, `BASEDIR`, substituted via `sed` at render time.
- Structural assertions only (no golden files). CI is single-version (PG 17).
- No self-authored explanatory code comments beyond what the surrounding code already uses.

**Prerequisite for local execution:** a PostgreSQL 13–17 install. Set `PT_PG_SANDBOX_BASEDIR` to its prefix (the dir whose `bin/` holds `initdb` etc.) and `PERCONA_TOOLKIT_BRANCH` to the repo root before running any verification step.

---

## File Structure

- `sandbox-pg/servers/postgresql.conf` — baseline config template (copied into each version dir).
- `sandbox-pg/servers/pg_hba.conf` — baseline auth template.
- `sandbox-pg/servers/{13,14,15,16,17}/{postgresql.conf,pg_hba.conf}` — per-version copies of the baseline.
- `sandbox-pg/servers/start`, `sandbox-pg/servers/stop`, `sandbox-pg/servers/use` — per-instance script templates.
- `sandbox-pg/start-sandbox` — start one `source`/`replica` instance.
- `sandbox-pg/stop-sandbox` — stop + remove instances.
- `sandbox-pg/load-pagila-db` — load pagila into an instance.
- `sandbox-pg/test-env` — orchestrate the default 3-instance environment.
- `sandbox-pg/pagila/{pagila-schema.sql,pagila-data.sql}` — vendored dataset.
- `src/go/pt-pg-summary/internal/tu/tu.go` — env-based connection params (Docker code removed).
- `src/go/pt-pg-summary/main_test.go` — tests against the sandbox.
- `src/go/pt-pg-summary/docker-compose.yml` — DELETED.
- `.github/workflows/pg-tests.yml` — CI job.
- `CONTRIBUTING.md` — PG sandbox test section.

---

## Task 1: Core standalone sandbox (templates, configs, `start-sandbox source`, `stop-sandbox`)

**Files:**
- Create: `sandbox-pg/servers/postgresql.conf`
- Create: `sandbox-pg/servers/pg_hba.conf`
- Create: `sandbox-pg/servers/{13,14,15,16,17}/postgresql.conf` and `.../pg_hba.conf`
- Create: `sandbox-pg/servers/start`, `sandbox-pg/servers/stop`, `sandbox-pg/servers/use`
- Create: `sandbox-pg/start-sandbox`
- Create: `sandbox-pg/stop-sandbox`

**Interfaces:**
- Produces:
  - `start-sandbox <source|replica> <port> [source_port]` — renders templates into `${TMP_DIR}/<port>/`, runs `initdb`, starts the instance; for `source` also sets the `postgres` password to `root`, creates role `replicator`, and creates DB `percona_test` with table `sentinel(id INT PRIMARY KEY, ping VARCHAR(64))`.
  - `stop-sandbox <port…>` — runs `${TMP_DIR}/<port>/stop` then `rm -rf ${TMP_DIR}/<port>`.
  - Per-instance `${TMP_DIR}/<port>/{start,stop,use}` scripts. `use` = `psql -h ${TMP_DIR}/<port> -p <port> -U postgres "$@"`.

- [ ] **Step 1: Create the baseline config template `sandbox-pg/servers/postgresql.conf`**

```conf
port = PORT
listen_addresses = '127.0.0.1'
unix_socket_directories = 'TMP_DIR/PORT'
logging_collector = on
log_directory = 'TMP_DIR/PORT/data'
log_filename = 'pg.log'
log_min_messages = warning
max_connections = 50
shared_buffers = 32MB
wal_level = replica
max_wal_senders = 10
hot_standby = on
fsync = off
full_page_writes = off
```

- [ ] **Step 2: Create the baseline auth template `sandbox-pg/servers/pg_hba.conf`**

```conf
local   all             all                            trust
host    all             all      127.0.0.1/32          trust
host    all             all      ::1/128               trust
local   replication     all                            trust
host    replication     replicator  127.0.0.1/32       trust
```

- [ ] **Step 3: Fan the baseline out into per-version dirs 13–17**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg/servers"
for v in 13 14 15 16 17; do
  mkdir -p "$v"
  cp postgresql.conf "$v/postgresql.conf"
  cp pg_hba.conf     "$v/pg_hba.conf"
done
ls 13 14 15 16 17
```
Expected: each dir lists `pg_hba.conf` and `postgresql.conf`.

- [ ] **Step 4: Create per-instance template `sandbox-pg/servers/start`**

```sh
#!/bin/sh
BASEDIR="BASEDIR"
DATA="TMP_DIR/PORT/data"
LOG="TMP_DIR/PORT/data/pg.log"

if "$BASEDIR/bin/pg_isready" -h "TMP_DIR/PORT" -p PORT >/dev/null 2>&1; then
   echo "PostgreSQL sandbox PORT already running"
   exit 0
fi

echo -n "Starting PostgreSQL sandbox on port PORT... "
if "$BASEDIR/bin/pg_ctl" -D "$DATA" -l "$LOG" -w -t 60 start >/dev/null 2>&1; then
   echo "OK"
   exit 0
fi
echo "FAILED"
exit 1
```

- [ ] **Step 5: Create per-instance template `sandbox-pg/servers/stop`**

```sh
#!/bin/sh
BASEDIR="BASEDIR"
DATA="TMP_DIR/PORT/data"

echo -n "Stopping PostgreSQL sandbox on port PORT... "
if "$BASEDIR/bin/pg_ctl" -D "$DATA" -m fast -w stop >/dev/null 2>&1; then
   echo "OK"
   exit 0
fi
echo "OK (not running)"
exit 0
```

- [ ] **Step 6: Create per-instance template `sandbox-pg/servers/use`**

```sh
#!/bin/sh
BASEDIR="BASEDIR"
exec "$BASEDIR/bin/psql" -h "TMP_DIR/PORT" -p PORT -U postgres "$@"
```

- [ ] **Step 7: Create `sandbox-pg/start-sandbox`**

```bash
#!/bin/bash
TMP_DIR=${TMP_DIR:-/tmp}

die() { echo "$1" >&2; exit 1; }

type=$1
port=$2
source_port=$3

[ -n "$type" ] || die "Usage: start-sandbox source|replica port [source_port]"
[ -n "$port" ] || die "Usage: start-sandbox source|replica port [source_port]"
[ "$type" = "source" ] || [ "$type" = "replica" ] || die "Invalid type: $type (use source|replica)"
[ "$port" -gt 1024 ] 2>/dev/null || die "Port must be > 1024: $port"
if [ "$type" = "replica" ]; then
   [ -n "$source_port" ] || die "replica requires a source_port"
fi

[ -n "$PERCONA_TOOLKIT_BRANCH" ] || die "PERCONA_TOOLKIT_BRANCH is not set"
[ -d "$PERCONA_TOOLKIT_BRANCH" ] || die "Invalid PERCONA_TOOLKIT_BRANCH: $PERCONA_TOOLKIT_BRANCH"
[ -n "$PT_PG_SANDBOX_BASEDIR" ] || die "PT_PG_SANDBOX_BASEDIR is not set"
BASEDIR="$PT_PG_SANDBOX_BASEDIR"
for bin in initdb pg_ctl postgres psql pg_basebackup pg_isready; do
   [ -x "$BASEDIR/bin/$bin" ] || die "Missing executable: $BASEDIR/bin/$bin"
done

major=$("$BASEDIR/bin/postgres" --version | awk '{print $3}' | cut -d. -f1)
srcdir="$PERCONA_TOOLKIT_BRANCH/sandbox-pg/servers"
verdir="$srcdir/$major"
[ -d "$verdir" ] || die "No config for PostgreSQL major $major: $verdir"

inst="$TMP_DIR/$port"
data="$inst/data"

if [ -f "$data/postmaster.pid" ]; then
   echo "Sandbox $port already started"
   exit 0
fi

render() {
   sed -e "s!TMP_DIR!$TMP_DIR!g" -e "s!BASEDIR!$BASEDIR!g" -e "s!PORT!$port!g" "$1"
}

rm -rf "$inst" || die "Failed to rm $inst"
mkdir -p "$inst" || die "Failed to mkdir $inst"

if [ "$type" = "source" ]; then
   "$BASEDIR/bin/initdb" -D "$data" -U postgres --auth-local=trust --auth-host=trust >/dev/null 2>&1 \
      || die "initdb failed for $port"
else
   src_inst="$TMP_DIR/$source_port"
   [ -d "$src_inst" ] || die "Source sandbox does not exist: $src_inst"
   "$BASEDIR/bin/pg_basebackup" -h 127.0.0.1 -p "$source_port" -U replicator \
      -D "$data" -R -X stream >/dev/null 2>&1 || die "pg_basebackup failed for $port"
   touch "$data/standby.signal"
fi

render "$verdir/postgresql.conf" > "$data/postgresql.conf"
render "$verdir/pg_hba.conf"     > "$data/pg_hba.conf"

for s in start stop use; do
   render "$srcdir/$s" > "$inst/$s"
   chmod +x "$inst/$s"
done

"$inst/start" || die "Failed to start sandbox $port"

if [ "$type" = "source" ]; then
   "$inst/use" -c "ALTER USER postgres PASSWORD 'root';" >/dev/null
   "$inst/use" -c "DO \$do\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='replicator') THEN CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD 'root'; END IF; END \$do\$;" >/dev/null
   if ! "$inst/use" -tAc "SELECT 1 FROM pg_database WHERE datname='percona_test'" | grep -q 1; then
      "$inst/use" -c "CREATE DATABASE percona_test;" >/dev/null
   fi
   "$inst/use" -d percona_test -c "CREATE TABLE IF NOT EXISTS sentinel (id INT PRIMARY KEY, ping VARCHAR(64) NOT NULL DEFAULT '');" >/dev/null
fi

exit 0
```

- [ ] **Step 8: Create `sandbox-pg/stop-sandbox`**

```bash
#!/bin/bash
TMP_DIR=${TMP_DIR:-/tmp}

die() { echo "$1" >&2; exit 1; }

[ $# -lt 1 ] && die "Usage: stop-sandbox PORTS"

status=0
for port in "$@"; do
   inst="$TMP_DIR/$port"
   if [ ! -d "$inst" ]; then
      echo "PostgreSQL sandbox $port does not exist"
      continue
   fi
   if [ -x "$inst/stop" ]; then
      "$inst/stop"
      status=$((status | $?))
   fi
   rm -rf "$inst"
   status=$((status | $?))
done
exit $status
```

- [ ] **Step 9: Make scripts executable**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
chmod +x start-sandbox stop-sandbox servers/start servers/stop servers/use
```
Expected: no output, exit 0.

- [ ] **Step 10: Verify a standalone source starts, answers, and tears down**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
./start-sandbox source 15432
"$TMP_DIR/15432/use" -tAc "SELECT 1"
"$TMP_DIR/15432/use" -tAc "SELECT rolname FROM pg_roles WHERE rolname='replicator'"
"$TMP_DIR/15432/use" -d percona_test -tAc "SELECT count(*) FROM sentinel"
./stop-sandbox 15432
test ! -d "${TMP_DIR:-/tmp}/15432" && echo "CLEANED"
```
Expected: `1`, then `replicator`, then `0`, then `CLEANED`. (`${TMP_DIR:-/tmp}` defaults to `/tmp`.)

- [ ] **Step 11: Verify `TMP_DIR` is honored (no residual /tmp)**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
TMP_DIR=/tmp/pgsbtest ./start-sandbox source 15432
grep -R "/tmp/15432" /tmp/pgsbtest/15432/data/postgresql.conf /tmp/pgsbtest/15432/{start,stop,use} ; echo "grep_exit=$?"
TMP_DIR=/tmp/pgsbtest ./stop-sandbox 15432
```
Expected: `grep_exit=1` (no `/tmp/15432` matches; all paths point under `/tmp/pgsbtest/15432`).

- [ ] **Step 12: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add sandbox-pg/servers sandbox-pg/start-sandbox sandbox-pg/stop-sandbox
git commit -m "PT-2552 - Add core standalone PostgreSQL sandbox (start/stop, configs, templates)"
```

---

## Task 2: pagila dataset + `load-pagila-db`

**Files:**
- Create: `sandbox-pg/pagila/pagila-schema.sql` (vendored)
- Create: `sandbox-pg/pagila/pagila-data.sql` (vendored)
- Create: `sandbox-pg/load-pagila-db`

**Interfaces:**
- Consumes: per-instance `use` script and `start-sandbox source` from Task 1.
- Produces: `load-pagila-db <port> [dbname]` — creates DB `pagila` (or `dbname`), applies schema then data, runs `ANALYZE`.

- [ ] **Step 1: Vendor the pagila schema and data**

Run (pins to a fetched revision; commit the files, not a submodule):
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
mkdir -p pagila
base="https://raw.githubusercontent.com/devrimgunduz/pagila/master"
curl -fSL "$base/pagila-schema.sql" -o pagila/pagila-schema.sql
curl -fSL "$base/pagila-data.sql"   -o pagila/pagila-data.sql
head -3 pagila/pagila-schema.sql
wc -l pagila/pagila-schema.sql pagila/pagila-data.sql
```
Expected: schema header lines print; both files have thousands of lines.

- [ ] **Step 2: Create `sandbox-pg/load-pagila-db`**

```sh
#!/bin/sh
TMP_DIR=${TMP_DIR:-/tmp}

die() { echo "$1" >&2; exit 1; }

[ -n "$1" ] || die "Usage: load-pagila-db PORT [dbname]"
PORT=$1
DBNAME=${2:-pagila}

inst="$TMP_DIR/$PORT"
[ -d "$inst" ] || die "PostgreSQL sandbox does not exist: $inst"
[ -n "$PERCONA_TOOLKIT_BRANCH" ] || die "PERCONA_TOOLKIT_BRANCH is not set"
[ -d "$PERCONA_TOOLKIT_BRANCH" ] || die "Invalid PERCONA_TOOLKIT_BRANCH: $PERCONA_TOOLKIT_BRANCH"
pagila="$PERCONA_TOOLKIT_BRANCH/sandbox-pg/pagila"

if ! "$inst/use" -tAc "SELECT 1 FROM pg_database WHERE datname='$DBNAME'" | grep -q 1; then
   "$inst/use" -c "CREATE DATABASE $DBNAME;" >/dev/null || die "Failed to create database $DBNAME"
fi

"$inst/use" -d "$DBNAME" -v ON_ERROR_STOP=1 -f "$pagila/pagila-schema.sql" >/dev/null || die "pagila schema load failed"
"$inst/use" -d "$DBNAME" -v ON_ERROR_STOP=1 -f "$pagila/pagila-data.sql"   >/dev/null || die "pagila data load failed"
"$inst/use" -d "$DBNAME" -c "ANALYZE;" >/dev/null
```

- [ ] **Step 3: Make it executable**

Run:
```bash
chmod +x "$PERCONA_TOOLKIT_BRANCH/sandbox-pg/load-pagila-db"
```
Expected: no output.

- [ ] **Step 4: Verify pagila loads and has data**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
./start-sandbox source 15432
./load-pagila-db 15432
"$TMP_DIR/15432/use" -d pagila -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'"
"$TMP_DIR/15432/use" -d pagila -tAc "SELECT count(*) FROM actor"
./stop-sandbox 15432
```
Expected: table count > 0 (pagila has ~20 tables), `actor` count = 200.

- [ ] **Step 5: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add sandbox-pg/pagila sandbox-pg/load-pagila-db
git commit -m "PT-2552 - Vendor pagila dataset and add load-pagila-db"
```

---

## Task 3: Replica support (streaming replication)

**Files:**
- Modify: `sandbox-pg/start-sandbox` (already handles `replica` from Task 1 Step 7)

**Interfaces:**
- Consumes: a running `source` (with role `replicator`) from Task 1.
- Produces: `start-sandbox replica <port> <source_port>` — a hot-standby streaming from the source.

The `replica` path was authored in Task 1 (Step 7) so `start-sandbox` is complete in one file; this task verifies replication end to end. No new code — if the verification fails, fix `start-sandbox`'s `replica` branch.

- [ ] **Step 1: Verify a replica streams from the source**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
./start-sandbox source  15432
./start-sandbox replica 15433 15432
"$TMP_DIR/15433/use" -tAc "SELECT pg_is_in_recovery()"
"$TMP_DIR/15432/use" -c "CREATE TABLE repltest (id int); INSERT INTO repltest VALUES (42);" >/dev/null
sleep 2
"$TMP_DIR/15433/use" -tAc "SELECT id FROM repltest"
./stop-sandbox 15433 15432
```
Expected: `t` (replica is in recovery), then `42` (row replicated to the standby).

- [ ] **Step 2: Verify the replica is read-only**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
./start-sandbox source  15432
./start-sandbox replica 15433 15432
"$TMP_DIR/15433/use" -tAc "CREATE TABLE nope (id int)" 2>&1 | grep -qi "read-only" && echo "READ_ONLY_OK"
./stop-sandbox 15433 15432
```
Expected: `READ_ONLY_OK` (writes rejected on the standby).

- [ ] **Step 3: Commit (no-op if Task 1 already covered it)**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git commit --allow-empty -m "PT-2552 - Verify PostgreSQL streaming replication in sandbox"
```

---

## Task 4: `test-env` orchestration + sentinel replication check

**Files:**
- Create: `sandbox-pg/test-env`

**Interfaces:**
- Consumes: `start-sandbox`, `stop-sandbox`, `load-pagila-db` from Tasks 1–2.
- Produces: `test-env {start|stop|restart|status|kill|checkconfig}` bringing up source `15432` + replicas `15433`,`15434`, loading pagila, and verifying replication via `percona_test.sentinel`.

- [ ] **Step 1: Create `sandbox-pg/test-env`**

```bash
#!/bin/bash
TMP_DIR=${TMP_DIR:-/tmp}

SRC=15432
REP1=15433
REP2=15434

err() { for m in "$@"; do echo "$m" >&2; done; }

checkconfig() {
   local e=0
   if [ -z "$PERCONA_TOOLKIT_BRANCH" ] || [ ! -d "$PERCONA_TOOLKIT_BRANCH" ]; then
      echo "PERCONA_TOOLKIT_BRANCH - INVALID"; e=1
   else
      echo "PERCONA_TOOLKIT_BRANCH=$PERCONA_TOOLKIT_BRANCH - ok"
   fi
   if [ -z "$PT_PG_SANDBOX_BASEDIR" ] || [ ! -x "$PT_PG_SANDBOX_BASEDIR/bin/initdb" ]; then
      echo "PT_PG_SANDBOX_BASEDIR - INVALID"; e=1
   else
      echo "PT_PG_SANDBOX_BASEDIR=$PT_PG_SANDBOX_BASEDIR - ok"
   fi
   return $e
}

instance_alive() {
   local port=$1
   "$PT_PG_SANDBOX_BASEDIR/bin/pg_isready" -h "$TMP_DIR/$port" -p "$port" >/dev/null 2>&1
}

SB="$PERCONA_TOOLKIT_BRANCH/sandbox-pg"

opt=$1
[ -n "$opt" ] || { err "Usage: test-env start|stop|restart|status|kill|checkconfig"; exit 1; }

if [ "$opt" = "checkconfig" ]; then
   checkconfig
   rc=$?
   echo -n "PostgreSQL test environment config is "
   [ $rc -eq 0 ] && echo "ok!" || echo "invalid."
   exit $rc
fi

checkconfig >/dev/null || { err "Invalid config. Run 'test-env checkconfig'."; exit 1; }

case $opt in
   start)
      "$SB/start-sandbox" source  $SRC        || exit 1
      "$SB/start-sandbox" replica $REP1 $SRC  || exit 1
      "$SB/start-sandbox" replica $REP2 $REP1 || exit 1
      echo -n "Loading pagila database... "
      "$SB/load-pagila-db" $SRC >/dev/null || { echo "FAILED"; exit 1; }
      echo "OK"

      ping=$("$TMP_DIR/$SRC/use" -tAc "SELECT md5(random()::text)")
      "$TMP_DIR/$SRC/use" -d percona_test -c \
         "INSERT INTO sentinel (id, ping) VALUES (1,'$ping') ON CONFLICT (id) DO UPDATE SET ping='$ping';" >/dev/null
      echo -n "Waiting for replication... "
      ok=0
      for i in $(seq 60); do
         pong=$("$TMP_DIR/$REP2/use" -d percona_test -tAc "SELECT ping FROM sentinel WHERE id=1" 2>/dev/null)
         [ "$pong" = "$ping" ] && { ok=1; break; }
         sleep 1
      done
      [ $ok -eq 1 ] && echo "OK" || { echo "FAILED"; exit 1; }
      echo "PostgreSQL test environment started."
      ;;
   stop)
      "$SB/stop-sandbox" $REP2 $REP1 $SRC
      echo "PostgreSQL test environment stopped."
      ;;
   kill)
      for port in $REP2 $REP1 $SRC; do
         [ -d "$TMP_DIR/$port" ] || continue
         "$PT_PG_SANDBOX_BASEDIR/bin/pg_ctl" -D "$TMP_DIR/$port/data" -m immediate stop >/dev/null 2>&1
         rm -rf "$TMP_DIR/$port"
      done
      echo "PostgreSQL test environment killed."
      ;;
   restart)
      "$0" stop
      "$0" start
      ;;
   status)
      st=0
      for port in $SRC $REP1 $REP2; do
         echo -n "Instance $port alive - "
         if instance_alive $port; then echo "yes"; else echo "NO"; st=1; fi
      done
      for port in $REP1 $REP2; do
         echo -n "Instance $port in recovery - "
         r=$("$TMP_DIR/$port/use" -tAc "SELECT pg_is_in_recovery()" 2>/dev/null)
         if [ "$r" = "t" ]; then echo "yes"; else echo "NO"; st=1; fi
      done
      echo -n "pagila loaded on $SRC - "
      p=$("$TMP_DIR/$SRC/use" -d pagila -tAc "SELECT count(*) FROM actor" 2>/dev/null)
      if [ "${p:-0}" -gt 0 ] 2>/dev/null; then echo "yes"; else echo "NO"; st=1; fi
      echo -n "PostgreSQL test environment is "
      [ $st -eq 0 ] && echo "ok!" || echo "invalid."
      exit $st
      ;;
   *)
      err "Usage: test-env start|stop|restart|status|kill|checkconfig"
      exit 1
      ;;
esac
```

- [ ] **Step 2: Make it executable**

Run:
```bash
chmod +x "$PERCONA_TOOLKIT_BRANCH/sandbox-pg/test-env"
```
Expected: no output.

- [ ] **Step 3: Verify checkconfig succeeds with a valid env**

Run:
```bash
"$PERCONA_TOOLKIT_BRANCH/sandbox-pg/test-env" checkconfig
```
Expected: both env lines `- ok` and `PostgreSQL test environment config is ok!`, exit 0.

- [ ] **Step 4: Verify checkconfig fails clearly without basedir**

Run:
```bash
env -u PT_PG_SANDBOX_BASEDIR "$PERCONA_TOOLKIT_BRANCH/sandbox-pg/test-env" checkconfig; echo "exit=$?"
```
Expected: `PT_PG_SANDBOX_BASEDIR - INVALID`, `... is invalid.`, `exit=1`.

- [ ] **Step 5: Verify full start → status → stop cycle**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg"
./test-env start
./test-env status
./test-env stop
for p in 15432 15433 15434; do test ! -d "${TMP_DIR:-/tmp}/$p" || echo "LEFTOVER $p"; done
echo "DONE"
```
Expected: start ends `PostgreSQL test environment started.`; status ends `ok!`; stop prints `stopped.`; no `LEFTOVER` lines; `DONE`.

- [ ] **Step 6: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add sandbox-pg/test-env
git commit -m "PT-2552 - Add test-env orchestration with sentinel replication check"
```

---

## Task 5: Wire `pt-pg-summary` tests onto the sandbox (remove Docker)

**Files:**
- Modify: `src/go/pt-pg-summary/internal/tu/tu.go` (replace entire file)
- Modify: `src/go/pt-pg-summary/main_test.go` (replace test wiring)
- Delete: `src/go/pt-pg-summary/docker-compose.yml`

**Interfaces:**
- Consumes: a running sandbox (`test-env start`) exposing a source on `15432`.
- Produces (in package `tu`): `Host`, `Port`, `Username`, `Password`, `Database` string vars read from env with defaults; used by `main_test.go`.

- [ ] **Step 1: Replace `internal/tu/tu.go` with env-based params (no Docker)**

```go
// This program is copyright 2019-2026 Percona LLC and/or its affiliates.
//
// THIS PROGRAM IS PROVIDED "AS IS" AND WITHOUT ANY EXPRESS OR IMPLIED
// WARRANTIES, INCLUDING, WITHOUT LIMITATION, THE IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE.
//
// This program is free software; you can redistribute it and/or modify it under
// the terms of the GNU General Public License as published by the Free Software
// Foundation, version 2.
//
// You should have received a copy of the GNU General Public License, version 2
// along with this program; if not, see <https://www.gnu.org/licenses/>.

package tu // test utils

import "os"

// Connection parameters for the PostgreSQL sandbox source instance.
// Defaults match sandbox-pg/test-env (source on port 15432, user postgres).
var (
	Host     = getVar("PG_HOST", "127.0.0.1")
	Port     = getVar("PG_PORT", "15432")
	Username = getVar("PG_USER", "postgres")
	Password = getVar("PG_PASSWORD", "root")
	Database = getVar("PG_DATABASE", "postgres")
)

func getVar(varname, defaultValue string) string {
	if v := os.Getenv(varname); v != "" {
		return v
	}
	return defaultValue
}
```

- [ ] **Step 2: Replace the test wiring in `main_test.go`**

Replace the `Test` type, the `tests` slice, and the four connection-based tests so they target the single sandbox instance. Keep `TestVersionOption` unchanged. New content for the top of the file through `TestCollectPerDatabaseInfo`:

```go
package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/percona/percona-toolkit/src/go/lib/pginfo"
	"github.com/percona/percona-toolkit/src/go/pt-pg-summary/internal/tu"
)

var logger = logrus.New()

func dsn(dbName string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=disable dbname=%s",
		tu.Host, tu.Port, tu.Username, tu.Password, dbName)
}

// requireSandbox skips the test when the sandbox source instance is not
// reachable, so `go test ./...` does not hard-fail in un-provisioned envs.
// connect returns *sql.DB (see main.go: func connect(dsn string) (*sql.DB, error)).
func requireSandbox(t *testing.T) *sql.DB {
	t.Helper()
	db, err := connect(dsn(tu.Database))
	if err != nil {
		t.Skipf("PostgreSQL sandbox not reachable at %s:%s (%v); run sandbox-pg/test-env start", tu.Host, tu.Port, err)
	}
	return db
}

func TestMain(m *testing.M) {
	logger.SetLevel(logrus.WarnLevel)
	os.Exit(m.Run())
}

func TestConnection(t *testing.T) {
	db := requireSandbox(t)
	db.Close()
}

func TestNewWithLogger(t *testing.T) {
	db := requireSandbox(t)
	defer db.Close()
	if _, err := pginfo.NewWithLogger(db, nil, 30, logger); err != nil {
		t.Errorf("Cannot run NewWithLogger: %s", err)
	}
}

func TestCollectGlobalInfo(t *testing.T) {
	db := requireSandbox(t)
	defer db.Close()
	info, err := pginfo.NewWithLogger(db, nil, 30, logger)
	if err != nil {
		t.Fatalf("Cannot run NewWithLogger: %s", err)
	}
	if errs := info.CollectGlobalInfo(db); len(errs) > 0 {
		for _, e := range errs {
			logger.Error(e)
		}
		t.Errorf("Cannot collect global information")
	}
}

func TestCollectPerDatabaseInfo(t *testing.T) {
	db := requireSandbox(t)
	defer db.Close()
	info, err := pginfo.NewWithLogger(db, nil, 30, logger)
	if err != nil {
		t.Fatalf("Cannot run NewWithLogger: %s", err)
	}
	for _, dbName := range info.DatabaseNames() {
		conn, err := connect(dsn(dbName))
		if err != nil {
			t.Errorf("Cannot connect to the %s database: %s", dbName, err)
			continue
		}
		if err := info.CollectPerDatabaseInfo(conn, dbName); err != nil {
			t.Errorf("Cannot collect information for %s: %s", dbName, err)
		}
		conn.Close()
	}
}
```

`connect` returns `*sql.DB` (confirmed: `main.go:134 func connect(dsn string) (*sql.DB, error)`), so `requireSandbox` returns `*sql.DB` and the test file imports `database/sql`, as written above.

- [ ] **Step 3: Verify the test file compiles against the current `connect` signature**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/src/go"
go vet ./pt-pg-summary/... 2>&1 | tail -5
```
Expected: no compile errors about `connect`, `*sql.DB`, or unused imports. (If `connect`'s signature has since changed, adjust `requireSandbox`'s return type to match.)

- [ ] **Step 4: Delete the docker-compose file**

Run:
```bash
git rm src/go/pt-pg-summary/docker-compose.yml
```
Expected: `rm 'src/go/pt-pg-summary/docker-compose.yml'`.

- [ ] **Step 5: Build the binary (needed by TestVersionOption) and run the tests against the sandbox**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg" && ./test-env start
cd "$PERCONA_TOOLKIT_BRANCH/src/go" && make linux-amd64
go test ./pt-pg-summary/... 2>&1 | tail -20
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg" && ./test-env stop
```
Expected: all tests PASS (build must produce `bin/pt-pg-summary` for `TestVersionOption`).

- [ ] **Step 6: Verify tests SKIP (not fail) when the sandbox is down**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/src/go"
go test ./pt-pg-summary/... -run 'TestConnection|TestCollectGlobalInfo' 2>&1 | tail -10
```
Expected: `--- SKIP` lines with the "sandbox not reachable" message; overall `ok`/no failures.

- [ ] **Step 7: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add src/go/pt-pg-summary/internal/tu/tu.go src/go/pt-pg-summary/main_test.go
git commit -m "PT-2552 - Wire pt-pg-summary tests onto sandbox-pg, remove Docker"
```

---

## Task 6: Phase-1 structural assertions against pagila

**Files:**
- Create: `src/go/pt-pg-summary/phase1_test.go`

**Interfaces:**
- Consumes: `requireSandbox`, `dsn`, `connect`, `logger` from `main_test.go`; `pginfo.PGInfo` exported fields (`ServerVersion`, `ClusterInfo`, `Settings`, `TableAccess`, `TableCacheHitRatio`, `IndexCacheHitRatio`).
- Produces: end-to-end structural tests requiring a loaded `pagila`.

- [ ] **Step 1: Write the failing structural test**

Create `src/go/pt-pg-summary/phase1_test.go`:

```go
package main

import (
	"testing"

	"github.com/percona/percona-toolkit/src/go/lib/pginfo"
)

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestPhase1PagilaCollected asserts that the sandbox's pagila database is
// present and that global + per-database collection returns populated,
// non-empty structures. Structural (shape/presence) assertions only.
func TestPhase1PagilaCollected(t *testing.T) {
	db := requireSandbox(t)
	defer db.Close()

	info, err := pginfo.NewWithLogger(db, nil, 30, logger)
	if err != nil {
		t.Fatalf("NewWithLogger failed: %s", err)
	}

	// pagila must be among the collected databases.
	names := info.DatabaseNames()
	if !contains(names, "pagila") {
		t.Fatalf("expected 'pagila' in DatabaseNames(), got %v", names)
	}

	// Server version parsed and non-empty.
	if info.ServerVersion == nil || info.ServerVersion.String() == "" {
		t.Errorf("expected a parsed ServerVersion, got %v", info.ServerVersion)
	}

	// Global info populates cluster info and settings.
	if errs := info.CollectGlobalInfo(db); len(errs) > 0 {
		for _, e := range errs {
			t.Log(e)
		}
		t.Fatalf("CollectGlobalInfo returned %d errors", len(errs))
	}
	if len(info.ClusterInfo) == 0 {
		t.Errorf("expected non-empty ClusterInfo")
	}
	if len(info.Settings) == 0 {
		t.Errorf("expected non-empty Settings")
	}

	// Per-database info for pagila populates the access/cache-hit maps.
	conn, err := connect(dsn("pagila"))
	if err != nil {
		t.Fatalf("cannot connect to pagila: %s", err)
	}
	defer conn.Close()
	if err := info.CollectPerDatabaseInfo(conn, "pagila"); err != nil {
		t.Fatalf("CollectPerDatabaseInfo(pagila) failed: %s", err)
	}
	if len(info.TableAccess["pagila"]) == 0 {
		t.Errorf("expected non-empty TableAccess for pagila")
	}
	if _, ok := info.TableCacheHitRatio["pagila"]; !ok {
		t.Errorf("expected TableCacheHitRatio entry for pagila")
	}
	if _, ok := info.IndexCacheHitRatio["pagila"]; !ok {
		t.Errorf("expected IndexCacheHitRatio entry for pagila")
	}
}
```

- [ ] **Step 2: Run it against a sandbox WITHOUT pagila (verify it fails/skips correctly)**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg" && ./start-sandbox source 15432   # no pagila
cd "$PERCONA_TOOLKIT_BRANCH/src/go"
go test ./pt-pg-summary/... -run TestPhase1PagilaCollected 2>&1 | tail -10
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg" && ./stop-sandbox 15432
```
Expected: FAIL with "expected 'pagila' in DatabaseNames()" (confirms the assertion is real).

- [ ] **Step 3: Run it against the full test-env (verify it passes)**

Run:
```bash
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg" && ./test-env start
cd "$PERCONA_TOOLKIT_BRANCH/src/go"
go test ./pt-pg-summary/... -run TestPhase1PagilaCollected -v 2>&1 | tail -15
cd "$PERCONA_TOOLKIT_BRANCH/sandbox-pg" && ./test-env stop
```
Expected: `--- PASS: TestPhase1PagilaCollected`.

- [ ] **Step 4: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add src/go/pt-pg-summary/phase1_test.go
git commit -m "PT-2552 - Add phase-1 structural tests for pt-pg-summary against pagila"
```

---

## Task 7: CI job (GitHub Actions, PostgreSQL 17)

**Files:**
- Create: `.github/workflows/pg-tests.yml`

**Interfaces:**
- Consumes: `sandbox-pg/test-env`, `src/go` build, `pt-pg-summary` tests.
- Produces: a CI job that provisions PG 17, runs the harness, and tears it down.

- [ ] **Step 1: Create `.github/workflows/pg-tests.yml`**

```yaml
name: pg-tests

on:
  push:
    branches: [ "3.x" ]
  pull_request:
    branches: [ "3.x" ]

concurrency:
  group: "${{ github.workflow }}-${{ github.ref }}"
  cancel-in-progress: true

jobs:
  pt-pg-summary:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version-file: ${{ github.workspace }}/go.mod

      - name: Install PostgreSQL 17
        run: |
          sudo sh -c 'echo "deb https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
          curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /etc/apt/trusted.gpg.d/pgdg.gpg
          sudo apt-get update
          sudo apt-get install -y postgresql-17
          sudo systemctl stop postgresql || true

      - name: Build Go tools
        run: cd src/go && make linux-amd64

      - name: Run pt-pg-summary sandbox tests
        env:
          PERCONA_TOOLKIT_BRANCH: ${{ github.workspace }}
          PT_PG_SANDBOX_BASEDIR: /usr/lib/postgresql/17
        run: |
          sandbox-pg/test-env checkconfig
          sandbox-pg/test-env start
          cd src/go && go test -v ./pt-pg-summary/...

      - name: Tear down sandbox
        if: always()
        env:
          PERCONA_TOOLKIT_BRANCH: ${{ github.workspace }}
          PT_PG_SANDBOX_BASEDIR: /usr/lib/postgresql/17
        run: sandbox-pg/test-env stop || sandbox-pg/test-env kill || true
```

- [ ] **Step 2: Lint the workflow YAML locally**

Run:
```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/pg-tests.yml')); print('YAML OK')"
```
Expected: `YAML OK`.

- [ ] **Step 3: Sanity-check the CI steps mirror local flow**

Run:
```bash
grep -E "test-env (checkconfig|start|stop|kill)|go test .*pt-pg-summary|make linux-amd64|postgresql-17|PT_PG_SANDBOX_BASEDIR" .github/workflows/pg-tests.yml
```
Expected: lines for checkconfig, start, stop/kill, `go test`, build, install, and the basedir env are all present.

- [ ] **Step 4: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add .github/workflows/pg-tests.yml
git commit -m "PT-2552 - Add CI job running pt-pg-summary against sandbox-pg on PostgreSQL 17"
```

---

## Task 8: Developer documentation

**Files:**
- Modify: `CONTRIBUTING.md`
- Modify: `.claude/skills/percona-toolkit-dev-setup/SKILL.md` (path per repo; confirm in Step 1)

**Interfaces:**
- Consumes: everything above.
- Produces: a documented local workflow for PG sandbox tests.

- [ ] **Step 1: Locate the dev-setup skill file**

Run:
```bash
grep -rl "percona-toolkit-dev-setup" .claude 2>/dev/null; find . -path ./.git -prune -o -name 'SKILL.md' -print 2>/dev/null | grep -i pg-\\\|dev-setup
```
Expected: prints the skill's `SKILL.md` path (use it in Step 3). If none is tracked in-repo, skip Step 3 and note it in the commit.

- [ ] **Step 2: Add a "PostgreSQL sandbox tests" section to `CONTRIBUTING.md`**

Append this section (place it near the existing MySQL sandbox / testing instructions):

```markdown
## PostgreSQL sandbox tests

Percona Toolkit's PostgreSQL tools are tested against a native-binary sandbox
under `sandbox-pg/`, analogous to the MySQL `sandbox/`.

**Prerequisites**
- A local PostgreSQL 13–17 install (Percona Distribution for PostgreSQL or the
  PGDG packages).
- Environment variables:
  - `PERCONA_TOOLKIT_BRANCH` — the repo root.
  - `PT_PG_SANDBOX_BASEDIR` — the PostgreSQL install prefix whose `bin/` holds
    `initdb`, `pg_ctl`, `postgres`, `psql`, `pg_basebackup`, `pg_isready`
    (e.g. `/usr/lib/postgresql/17` on Debian/Ubuntu).

**Run the tests**
```sh
export PERCONA_TOOLKIT_BRANCH=$(git rev-parse --show-toplevel)
export PT_PG_SANDBOX_BASEDIR=/usr/lib/postgresql/17

# Bring up a source + two replicas and load pagila:
sandbox-pg/test-env start

# Build the tools and run the PostgreSQL tool tests:
( cd src/go && make linux-amd64 && go test ./pt-pg-summary/... )

# Tear everything down:
sandbox-pg/test-env stop
```

`sandbox-pg/test-env checkconfig` validates the environment; `status` reports
instance health and replication. Tests skip (not fail) when the sandbox is not
running.
```

- [ ] **Step 3: Mirror the flow in the dev-setup skill**

Add the same "PostgreSQL sandbox tests" steps (prereqs, env vars, `test-env start` → `go test` → `test-env stop`) to the `percona-toolkit-dev-setup` skill file located in Step 1, in its testing section.

- [ ] **Step 4: Verify the docs render and reference real paths**

Run:
```bash
grep -n "sandbox-pg/test-env\|PT_PG_SANDBOX_BASEDIR\|pt-pg-summary" CONTRIBUTING.md
```
Expected: the new section's commands and env var appear.

- [ ] **Step 5: Commit**

```bash
cd "$PERCONA_TOOLKIT_BRANCH"
git add CONTRIBUTING.md .claude 2>/dev/null
git commit -m "PT-2552 - Document PostgreSQL sandbox test workflow for developers"
```

---

## Self-Review Notes

- **Spec coverage:** Sandbox layout (Task 1), env/binary discovery (Task 1 `start-sandbox`), path parameterization (Task 1 Step 11), pagila (Task 2), replication + sentinel (Tasks 3–4), test-env command surface (Task 4), pt-pg-summary wiring + Docker removal (Task 5), structural phase-1 tests (Task 6), CI single-version PG 17 (Task 7), developer docs (Task 8). Acceptance criteria 1–8 from the spec map to Tasks 4, 4, 4, 1, 5, 6, 7, 8 respectively.
- **Per-version configs:** all of 13–17 are created as identical baselines in Task 1 Step 3 so version detection always resolves; per-version divergence can be introduced later without restructuring.
- **Open confirmation in-task:** the `connect` return type (Task 5 Step 3) and the dev-setup skill path (Task 8 Step 1) are confirmed by a command inside the task rather than assumed.
```
