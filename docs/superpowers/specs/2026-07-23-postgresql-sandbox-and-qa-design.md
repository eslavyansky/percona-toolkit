# PostgreSQL Sandbox + QA Harness for Percona Toolkit — Design Spec

Date: 2026-07-23
Status: Approved
Tickets:
- PT-2552 — PostgreSQL sandbox for Percona Toolkit (deployment tool)
- Implement Postgres tooling testing/QA (integration test harness + first tests)

## 1. Problem & Goal

Percona Toolkit ships a MySQL sandbox (top-level `sandbox/`) that starts
multiple MySQL servers from pre-defined configuration, loads sample data, and
cleans up. It is the backbone of the Perl test suite. PostgreSQL has no
equivalent, creating a gap in testing parity and automation.

This spec delivers two joined pieces:

- **Sandbox (PT-2552):** a native-binary PostgreSQL sandbox that starts
  multiple instances (standalone + streaming replication) from pre-defined
  per-version configuration, loads the `pagila` sample dataset, and cleans up —
  mirroring the MySQL sandbox in form and command surface.
- **QA harness:** wire `pt-pg-summary` integration tests onto the sandbox
  (replacing its ad-hoc Docker setup), add a first batch of end-to-end tests,
  run them in CI, and document how developers run PG sandbox tests locally.

## 2. Scope

In scope:
- New self-contained top-level directory `sandbox-pg/` (native binaries, no Docker).
- Standalone instances and `source`/`replica` streaming replication.
- Per-version config templates (PostgreSQL 13–17).
- Vendored `pagila` dataset (PostgreSQL analog of `sakila`) + loader.
- Full teardown / cleanup; self-verification (`checkconfig`, `status`, smoke path).
- `pt-pg-summary` test wiring: refactor `internal/tu/tu.go` + `main_test.go`
  onto the sandbox; **remove** the tool's `docker-compose.yml` and Docker code.
- Phase 1 end-to-end tests for `pt-pg-summary` with **structural assertions**.
- CI job (GitHub Actions) running the harness against **one** PG major (17).
- Developer documentation (CONTRIBUTING.md + `percona-toolkit-dev-setup` skill).

Out of scope:
- Any change to the existing MySQL sandbox (`sandbox/`) — it is not touched.
- Multi-primary / cluster topologies (no direct PostgreSQL analog to MySQL
  `source-source` / Galera `cluster` / `channels`).
- Golden-file/snapshot assertions (structural assertions only for phase 1).
- Multi-version CI matrix (phase 1 is single-version; expand later).

## 3. Guiding Principle: MySQL Parity

Mirror the MySQL sandbox in form, command surface, and lifecycle. Where an
early draft diverged, it was aligned to MySQL: replication, per-version
configs, and env-based basedir.

Notes on requirement wording vs. repo reality (to keep expectations accurate):
- The QA ticket mentions **dbdeployer**; this repo does **not** use dbdeployer.
  The MySQL harness is the custom `sandbox/` + `test-env` scripts. "Same
  processes as documented for MySQL" therefore means mirror the `test-env`
  script pattern (which this spec does), not adopt dbdeployer.
- The active CI (`.github/workflows/toolkit.yml`) currently runs **build only**
  — no `go test`. `go test ./src/go/...` lives only in the legacy `.travis.yml`.
  So "executable in CI" is net-new work: a new GitHub Actions test job.

Deliberate difference: default ports `15432–15434` (not MySQL `12345–12347`), so
the PostgreSQL and MySQL sandboxes can run simultaneously without conflict.

---

# Part A — Sandbox (`sandbox-pg/`)

## 4. Directory Layout

```
sandbox-pg/
  start-sandbox            # start one instance: source|replica
  stop-sandbox             # stop + remove one or more instances
  test-env                 # orchestrate the default environment
  load-pagila-db           # load pagila into an instance
  servers/
    13/ 14/ 15/ 16/ 17/    # per-major: postgresql.conf + pg_hba.conf
    start  stop  use        # per-instance script templates
  pagila/
    pagila-schema.sql
    pagila-data.sql
```

`sandbox-pg/` is a sibling of `sandbox/`, not nested under it, so no MySQL
sandbox glob (e.g. `for script in .../sandbox/servers/*`) can pick up
PostgreSQL files.

## 5. Environment & Binary Discovery

Mirrors the MySQL sandbox env contract; no PATH auto-detection.

- `PERCONA_TOOLKIT_BRANCH` — repo root. Shared with the MySQL sandbox. Required.
- `PT_PG_SANDBOX_BASEDIR` — a PostgreSQL install dir with `bin/initdb`,
  `bin/pg_ctl`, `bin/postgres`, `bin/psql` (Percona Distribution for PostgreSQL
  or a system install). Required; die if missing. Analog of
  `PERCONA_TOOLKIT_SANDBOX`.
- `TMP_DIR` — base dir for instance data, default `/tmp`
  (`TMP_DIR=${TMP_DIR:-/tmp}`). Honored consistently everywhere (see §8).

Major version detected from `bin/postgres --version` → `servers/<major>/`; die
with a clear message if that directory is absent (same as MySQL selecting
`servers/<version>/`).

## 6. Instance Lifecycle

Each instance lives in `${TMP_DIR}/<port>/`:

```
${TMP_DIR}/<port>/
  data/                    # PGDATA (initdb target)
  data/postgresql.conf     # rendered from template (overwrites initdb default)
  data/pg_hba.conf         # rendered from template
  data/pg.log              # server log (logging_collector)
  .s.PGSQL.<port>          # unix socket (socket dir = ${TMP_DIR}/<port>)
  start  stop  use         # rendered per-instance scripts
```

### 6.1 `start-sandbox <source|replica> <port> [source_port]`

`source` (standalone or replication primary):
1. Sanity-check args, `PERCONA_TOOLKIT_BRANCH`, `PT_PG_SANDBOX_BASEDIR`, port > 1024.
2. `rm -rf ${TMP_DIR}/<port>` then `mkdir`.
3. `initdb -D ${TMP_DIR}/<port>/data --username=postgres`.
4. Render `postgresql.conf` / `pg_hba.conf` from `servers/<major>/` into PGDATA
   (overwriting initdb defaults) and the per-instance `start`/`stop`/`use` into
   `${TMP_DIR}/<port>/`, substituting placeholders (`PORT`, `TMP_DIR`, `BASEDIR`)
   via `sed`.
5. Start via the per-instance `start` script (`pg_ctl start -w`, waits for ready).
6. Set the `postgres` role password to `root`; create the replication role.
7. Create the `percona_test` database with a `sentinel` table (replication check
   target; analog of MySQL `percona_test.sentinel`).

`replica`:
1. Same sanity checks; require `source_port`; require `${TMP_DIR}/<source_port>` exists.
2. `rm -rf` / `mkdir` the instance dir.
3. `pg_basebackup` from the source into `data/` (replication role).
4. Write `standby.signal`; set `primary_conninfo` (host=127.0.0.1,
   port=<source_port>, replication user) in `postgresql.auto.conf`.
5. Render config with `hot_standby=on`; start via per-instance `start`. The
   replica comes up read-only automatically (hot standby).

### 6.2 `stop-sandbox <port…>`

For each port: run `${TMP_DIR}/<port>/stop` (`pg_ctl stop -m fast`) if present,
then `rm -rf ${TMP_DIR}/<port>`. Mirrors MySQL `stop-sandbox`.

### 6.3 Per-instance scripts (`servers/{start,stop,use}`)

- `start` — `pg_ctl -D .../data -l .../data/pg.log start -w`, idempotent
  (liveness via `pg_isready` before starting).
- `stop` — `pg_ctl -D .../data stop -m fast -w`.
- `use` — `psql "host=${TMP_DIR}/<port> port=<port> user=postgres" "$@"`
  (unix socket by default; analog of MySQL `use`).

## 7. Configuration Templates

`servers/<major>/postgresql.conf` (placeholders substituted at render time):
- `port = PORT`
- `listen_addresses = '127.0.0.1'`
- `unix_socket_directories = 'TMP_DIR/PORT'`
- `logging_collector = on`, `log_directory = 'TMP_DIR/PORT/data'`,
  `log_filename = 'pg.log'`
- Replication-ready on every instance (any can be a primary): `wal_level = replica`,
  `max_wal_senders`, `hot_standby = on`
- Small resource footprint for tests (modest `shared_buffers`, etc.)

`servers/<major>/pg_hba.conf`:
- `trust` for local unix socket and `127.0.0.1/32` for the `postgres` superuser.
- A `replication` entry for the replication role from `127.0.0.1/32`.

Per-version dirs exist so version-specific settings can differ where needed;
otherwise the file is the common baseline. Supported majors: 13, 14, 15, 16, 17.

## 8. Path Handling (parameterization)

All instance paths are parameterized through placeholders resolved by `sed` at
render time — no hardcoded `/tmp`:
- `TMP_DIR` → `${TMP_DIR}` (default `/tmp`)
- `PORT` → the instance port
- `BASEDIR` → `${PT_PG_SANDBOX_BASEDIR}`

`TMP_DIR=/some/other/dir` must fully relocate an instance (data, socket, log,
config, scripts) with no residual `/tmp`. Clean-slate decision for the new
sandbox; the MySQL sandbox is not modified.

## 9. Sample Data: pagila

`pagila` is the PostgreSQL port of `sakila`, under a PostgreSQL-compatible
license. Schema + data SQL are vendored into `sandbox-pg/pagila/` (analog of the
vendored `sandbox/sakila.sql`).

`load-pagila-db <port> [dbname]` (mirrors `load-sakila-db`):
1. Sanity-check the instance dir and `PERCONA_TOOLKIT_BRANCH`.
2. Create database `pagila` (or `dbname`).
3. Apply `pagila/pagila-schema.sql` then `pagila/pagila-data.sql` via `use`.
4. `ANALYZE` the loaded database.

## 10. `test-env` Orchestration

Command surface mirrors MySQL `test-env`:
`start | stop | restart | status | kill | checkconfig`.

- `checkconfig` — validate `PERCONA_TOOLKIT_BRANCH` and `PT_PG_SANDBOX_BASEDIR`
  (binaries present); print resolved config; non-zero exit on problems.
- `start` — source on `15432` + two replicas (`15433`→15432, `15434`→15433),
  then `load-pagila-db 15432`. Verify replication via the `percona_test.sentinel`
  ping/pong from source to the last replica.
- `stop` — `stop-sandbox 15434 15433 15432`.
- `kill` — best-effort forced teardown when `stop` fails.
- `restart` — `stop` then `start`.
- `status` — per instance: `pg_isready` / `SELECT 1`; on replicas check
  `pg_is_in_recovery()` is true and streaming; confirm pagila on the source.

## 11. Authentication Model

- initdb superuser `postgres`; password `root` (matches `pt-pg-summary` test
  defaults: user `postgres`, password `root`).
- Dedicated replication role for `pg_basebackup` / streaming.
- `pg_hba.conf`: `trust` locally / on `127.0.0.1`; password auth
  (`scram-sha-256`, `md5` fallback on older majors) available for TCP.

---

# Part B — QA Harness

## 12. `pt-pg-summary` Test Wiring

Replace the tool's Docker-based test setup with the sandbox.

- **Remove** `src/go/pt-pg-summary/docker-compose.yml`.
- **Refactor `internal/tu/tu.go`:** drop `getContainerIP` / `docker inspect`
  and the hardcoded per-container ports (`go_postgres9_1`, 6432–6435, …).
  Expose sandbox connection parameters read from env with sensible defaults:
  - `PG_HOST` (default `127.0.0.1`)
  - `PG_PORT` (default `15432` — the sandbox source)
  - `PG_USER` (default `postgres`), `PG_PASSWORD` (default `root`)
  Optionally a small list of instance ports (source + replicas) for tests that
  want more than one instance.
- **Refactor `main_test.go`:** replace the hardcoded 9/10/11/12 matrix with the
  sandbox instance(s) from env. Keep the existing test functions
  (`TestConnection`, `TestNewWithLogger`, `TestCollectGlobalInfo`,
  `TestCollectPerDatabaseInfo`, `TestVersionOption`) working against the sandbox.

## 13. Phase 1 End-to-End Tests (structural assertions)

Build on the existing tests; add assertions on collected content (not just
"no error"), using `pagila` as the known dataset. Structural, version-tolerant:

- **Databases:** `info.DatabaseNames()` includes `pagila` (and `postgres`).
- **Global info:** cluster info populated — non-empty server version, a valid
  `pg_is_in_recovery()` result, start time present; settings/counters collected
  without error and key sections non-empty.
- **Per-database info (pagila):** table/index counts > 0; a known pagila table
  (e.g. `film`, `actor`) is present in the collected schema info; row counts ≥ 0
  and the collection returns no error.
- **`--version`:** existing SemVer check retained.

Assertions check shape and presence (counts > 0, expected names present, no
errors), not exact values — robust across PG majors and environments. No
golden-file snapshots in phase 1.

Tests skip cleanly (with a clear message) when the sandbox is not running, so
`go test ./...` in an un-provisioned environment does not hard-fail.

## 14. CI Integration (GitHub Actions)

Add a test job (new workflow or a job in `toolkit.yml`) — the active CI runs no
tests today, so this is net-new.

Phase 1 job (single major, PG 17):
1. Check out; set up Go from `go.mod`.
2. Install PostgreSQL 17 (Percona Distribution or the PGDG apt repo); set
   `PT_PG_SANDBOX_BASEDIR` to its install prefix and `PERCONA_TOOLKIT_BRANCH`
   to the workspace.
3. Build tools (`cd src/go; make linux-amd64`) so `--version` test has a binary.
4. `sandbox-pg/test-env start`.
5. `go test ./src/go/pt-pg-summary/...`.
6. `sandbox-pg/test-env stop` (always, even on failure).

The matrix is intentionally single-version for phase 1; the job is structured so
a `strategy.matrix` over majors can be added later without redesign.

## 15. Developer Documentation

- **CONTRIBUTING.md:** add a "PostgreSQL sandbox tests" section paralleling the
  MySQL sandbox instructions: install PostgreSQL, set `PT_PG_SANDBOX_BASEDIR`
  and `PERCONA_TOOLKIT_BRANCH`, `sandbox-pg/test-env start`, run
  `go test ./src/go/pt-pg-summary/...`, `sandbox-pg/test-env stop`.
- **`percona-toolkit-dev-setup` skill:** mirror the same PG sandbox flow so the
  local-dev guidance stays in sync.

---

## 16. Acceptance Criteria (mapped)

Sandbox (PT-2552):
1. `test-env checkconfig` passes with a valid env; fails clearly when
   `PT_PG_SANDBOX_BASEDIR` is unset or binaries are missing.
2. Smoke: `test-env start` → all three instances answer `SELECT 1`; `pagila`
   exists with rows on the source; replicas report `pg_is_in_recovery()=true`
   and stream; the sentinel ping propagates source → last replica.
3. `test-env stop` removes `${TMP_DIR}/1543{2,3,4}` completely.
4. `TMP_DIR=/other test-env start` works with zero residual `/tmp`.

QA harness:
5. `pt-pg-summary` tests run against the sandbox with **no Docker**
   (`docker-compose.yml` and `docker inspect` code removed).
6. Phase 1 end-to-end tests pass with structural assertions against `pagila`.
7. CI job spins up PG 17 + sandbox, runs the tests, tears down — green.
8. CONTRIBUTING.md (and the dev-setup skill) document the local PG sandbox flow.

## 17. Sequencing

Within one delivery, but ordered:
1. Part A — sandbox (`sandbox-pg/`) + `pagila` + self-verification.
2. Part B — `pt-pg-summary` wiring → structural tests → CI job → docs.

Part B depends on Part A being functional. Each part is independently testable.

## 18. Risks & Notes

- Requires a local PostgreSQL install via `PT_PG_SANDBOX_BASEDIR` (intentional
  parity with MySQL; no PATH magic). CI installs PG 17 explicitly.
- Streaming replication differs only slightly across 13–17 (all
  `standby.signal` + `primary_conninfo`); pre-12 `recovery.conf` unsupported.
- `pagila` is larger than a hand-rolled dataset; acceptable and it is the direct
  `sakila` analog the task calls for. It is vendored, so CI works offline.
- Removing the Docker path means contributors must install PostgreSQL locally;
  documented in CONTRIBUTING.md. This is the accepted trade-off (Docker fully
  removed, sandbox is the single path).
