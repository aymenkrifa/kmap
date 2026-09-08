# kmap

**Your services, your names, every cluster.**

kmap maps the names you actually say onto the workloads Kubernetes actually
runs, so `kmap logs api` tails the right deployment whether `api` is
`api-server` on your laptop, `backend` in staging, or `api-gateway` in the
`backend` namespace in production. It shells out to `kubectl` — or to whatever
wrapper you already use — so there is no new client, no new credentials and
nothing to authenticate.

```
$ kmap pods
local — 5 alias(es)  22:08:45

ALIAS   WORKLOAD    READY  STATUS       REASON    RESTARTS  AGE  DOCS
api     api-server  1/1    Running      —         0         3h   https://api.example.com/docs
auth    auth        1/1    Running      —         2         2d   —
worker  worker      0/1    Pending      Init:1/2  0         4m   —
queue   queue       —      scaled to 0  —         —         —    —
cache   cache       1/1    Running      —         0         9d   —
```

One table, five services, three namespaces, one alias per line — and the row
that has no pods says *why* it has none.

## Install

Requires Go 1.26 or newer.

```sh
git clone https://github.com/aymenkrifa/kmap && cd kmap
go build -o ~/.local/bin/kmap .
```

Or, with read access to the repo:

```sh
go install github.com/aymenkrifa/kmap@latest
```

## Quick start

```sh
kmap init                 # writes a starter config from your kubeconfig
$EDITOR ~/.config/kmap/config.yaml
kmap config validate      # every problem at once, with line numbers
kmap pods
```

`kmap init` reads your kubeconfig and turns every context into an environment.
It picks the context `kubectl` is already pointed at as the default, and marks
anything named like production as protected:

```yaml
version: 1

defaults:
  environment: k3s
  selector: "app={{.Workload}}"
  columns: [alias, workload, ready, status, reason, restarts, age, docs]

environments:
  k3s:
    context: k3s
  Staging:
    context: Staging
  Production:
    context: Production
    protected: true
```

Then add your aliases. See [`config.example.yaml`](config.example.yaml) for a
fully commented file, or the [Configuration](#configuration) section below.

For shell completion and short commands, in `~/.zshrc`:

```sh
alias pods='kmap pods'
alias logs='kmap logs'
source <(kmap completion zsh)
```

## Commands

Every command takes the same positional shape: an **optional environment**,
then **aliases**, then anything you want handed to `kubectl` verbatim.

```sh
kmap pods                 # default environment, default aliases
kmap pods staging         # staging, default aliases
kmap pods staging api     # staging, one alias
kmap pods api -l tier=web # -l reaches kubectl untouched
```

The first argument starting with `-` ends kmap's arguments and begins
kubectl's. Use `--` to forward a flag that kmap also defines.

### `kmap pods`

One table for every alias you asked about, grouped so each namespace costs one
`get pods` call rather than one per service.

| Flag | Meaning |
| --- | --- |
| `-w`, `--watch` | refresh in place until interrupted |
| `-f`, `--follow` | synonym for `--watch` |
| `--interval N` | seconds between refreshes (default 2) |
| `--columns a,b,c` | choose the columns for this run |

With `--watch`, a bare trailing number is taken as the interval, so
`kmap pods staging api -f 5` works. A number that names a real alias still wins.

`-o`/`--output` is dropped with a note: kmap asks for JSON and renders its own
table, so forwarding yours would send the flag twice.

**An empty row explains itself.** Rather than a single grey `absent`, kmap asks
the namespace what it knows and distinguishes `scaled to 0` (deployment exists,
zero replicas wanted), `no pods` (replicas wanted, none running) and
`not deployed` (no such deployment). That extra `get deploy` happens only for
namespaces that actually have an unexplained empty row.

### `kmap logs`

```sh
kmap logs api                # follow one service
kmap logs api worker auth    # follow three at once, colour-labelled
kmap logs prod api -j        # pretty-print JSON log lines
kmap logs api --tail=100     # --tail is forwarded to kubectl
```

Several aliases stream into one terminal, each line labelled with the service it
came from. One alias gets no prefix, because there is nothing to disambiguate:

```
$ kmap logs api worker
[api] {"timestamp":"2026-09-08T22:04:11.203Z","status":"info","message":"GET /v1/items HTTP/1.1\" 200"}
[api] {"timestamp":"2026-09-08T22:04:12.881Z","status":"warning","message":"cache miss for items:list"}
[worker] {"timestamp":"2026-09-08T22:04:11.512Z","status":"info","message":"picked up job 4192"}
[worker] plain text lines pass through untouched
[worker] {"timestamp":"2026-09-08T22:04:13.004Z","status":"error","message":"job 4192 failed"}
```

`-j` renders structured logs as text, colouring the level and any HTTP status
code in the message. Lines that are not JSON pass through untouched, so it is
safe on any stream:

```
$ kmap logs api -j
08-09-2026 22:04:11 - INFO - GET /v1/items HTTP/1.1" 200
08-09-2026 22:04:12 - WARNING - cache miss for items:list
```

An alias naming several workloads gets one follow per workload, labelled
`alias/workload`. One stream failing does not tear down the others.

### `kmap docs`

Where an alias carries a `docs:` path, kmap hangs it off that service's ingress
origin — so the URL is a fact about the cluster, not a bookmark that rots.

```sh
kmap docs           # print every documented service's URL
kmap docs prod api  # one service, in production
kmap docs --open    # open them in your browser
```

In a terminal the URLs are OSC 8 hyperlinks; piped or redirected, they come out
as plain greppable text.

You do not have to find those paths yourself. `kmap docs discover` probes each
service's ingress for the usual documentation paths — FastAPI, NestJS,
springdoc and plain Swagger UI are all covered by the defaults — and says what
answered:

```
$ kmap docs discover
  api            /docs
  auth           unreachable
  worker         /docs
  queue          no ingress
  cache          no ingress

2 of 5 have documentation; re-run with --write to record it
```

A miss is never a bare "no": you get `unreachable`, `no ingress`,
`not serving (503) — scaled to zero?`, or
`answers 200 everywhere — catch-all route, not documentation`. That last one is
why discovery sniffs the response body rather than trusting a status code — a
single-page app happily returns 200 for `/openapi.json`.

| Flag | Meaning |
| --- | --- |
| `--write` | record the discovered paths in the config file |
| `--insecure` | skip TLS verification, for self-signed dev certs |

`--write` edits through the file's parse tree and rewrites only the lines that
change, so every comment, blank line and bit of alignment survives. The
previous file is kept alongside as `config.yaml.bak`.

```diff
-  worker: worker
+  worker: {workload: worker, docs: /docs}
```

An alias written as a multi-line block is reported rather than mangled: kmap
tells you which one to edit by hand.

### `kmap config`

- `kmap config validate` — check the file and report **every** problem, with
  line numbers, rather than one per run.
- `kmap config show` — print the config as kmap understands it, defaults
  applied. Output is valid input: aliases come back in the same grammar you
  wrote them in.

### `kmap init`

Generate a starter config from your kubeconfig. `--kubeconfig` picks the file
(default `$KUBECONFIG`, else `~/.kube/config`); `--force` overwrites an
existing config.

### `kmap completion`

`kmap completion zsh|bash|fish` writes a completion script to stdout,
completing kmap's own commands and flags.

## Configuration

One YAML file. Looked up in this order:

1. `--config <path>`
2. `$KMAP_CONFIG`
3. `$XDG_CONFIG_HOME/kmap/config.yaml`
4. `~/.config/kmap/config.yaml`

### `defaults`

| Key | Meaning |
| --- | --- |
| `environment` | what a bare `kmap pods` targets. Optional if there is only one environment |
| `aliases` | what a bare `kmap pods` shows. Defaults to every alias, sorted |
| `selector` | how to find a workload's pods. Defaults to `app={{.Workload}}` |
| `columns` | default columns for `kmap pods` |
| `docs_paths` | paths `kmap docs discover` probes |
| `insecure` | skip TLS verification while probing |

### `environments`

An environment is reached one of two ways, and **exactly one** must be set:

```yaml
environments:
  local:
    command: [klocal]      # any argv prefix — a wrapper, not necessarily kubectl
  staging:
    context: Staging       # run as: kubectl --context Staging
    namespace: staging     # optional default namespace for this environment
  prod:
    context: Production
    protected: true        # red [protected] badge in the header
```

`command:` is the escape hatch that makes kmap work with whatever you already
have. If your cluster is reached through a wrapper script that injects a
kubeconfig, a namespace and three flags, kmap will call that script — it never
links client-go and has no opinion about how you authenticate.

### `aliases`

Three shapes, mixed freely.

**A bare name** — the same workload everywhere, optionally `workload@namespace`:

```yaml
aliases:
  queue: queue
  cache: cache@data
```

**An object** — one workload everywhere, plus its details:

```yaml
aliases:
  sdk:
    workload: vendor-sdk
    docs: /docs
  legacy:
    workload: legacy-api
    selector: "app.kubernetes.io/name=legacy-api"   # labelled differently
  bundle:
    workloads: [api-server, worker]                 # several at once
```

**A map keyed by environment** — a different workload per cluster. Each value is
itself a bare name, a `name@namespace`, or an object:

```yaml
aliases:
  api:
    local:   {workload: api-server, docs: /docs}
    staging: backend
    prod:    api-gateway@backend
```

### Resolution rules

- **Namespace**, most specific first: the mapping's `namespace`, then the
  environment's, then `default`.
- **Selector**: the mapping's `selector`, else `defaults.selector`.
  `{{.Workload}}` expands to the resolved workload name.
- **Several workloads**: a `key={{.Workload}}` selector widens to
  `key in (a,b)` automatically. Any other template needs an explicit
  `selector:` on that alias, and says so if you forget.
- **A typo** gets a suggestion:

```
$ kmap pods aoi
error: unknown alias "aoi" (have: api, auth, cache, queue, worker)
did you mean api?
```

## Columns

`--columns` for one run, `defaults.columns` for good.

| Column | Shows | Needs |
| --- | --- | --- |
| `alias` | your name for the service | |
| `workload` | the real workload name | |
| `pod` | pod name | |
| `ready` | ready/total containers | |
| `status` | pod phase, or why there are no pods | `get deploy` (empty rows only) |
| `reason` | waiting/terminated reason, or init progress | |
| `restarts` | restart count | |
| `age` | pod age | |
| `namespace` | resolved namespace | |
| `image` | container image, registry path trimmed | |
| `cpu`, `mem` | current usage | `top pods` (metrics-server) |
| `url` | ingress URL | `get ingress` |
| `docs` | API documentation URL | `get ingress` |

Default: `alias, workload, ready, status, reason, restarts, age, docs`.

Each extra dataset costs one call per namespace, and kmap fetches only the union
of what the chosen columns actually asked for. None of it is fatal — RBAC is
granted per resource, and metrics-server need not be installed at all. A dataset
the cluster refuses costs its own column and is named in a note:

```
local — 3 alias(es)  22:12:21
cannot read metrics, ingresses here — those columns stay blank
```

## Output

Colour and terminal hyperlinks are emitted only when stdout is a terminal, and
never when `NO_COLOR` is set — so `kmap pods | grep`, `kmap logs -j > file` and
anything scraping the output all see clean text. `kmap pods -w` refreshes in
place without eating your scrollback. Exit status is the child process's own, so
kmap composes in scripts.

## Why not k9s?

k9s shows you the cluster. kmap shows you **your** services, under **your**
names, and nothing else — five rows instead of two hundred, in one table that
spans namespaces, with the same command against every environment. It is a
terminal command rather than a terminal application: no TUI to enter and leave,
and the output pipes.

If you already keep a wall of shell aliases and `kubectl -n … -l app=…`
incantations for the six services you care about, that is the thing kmap
replaces.

## Development

```sh
go test ./...
go vet ./...
gofmt -l .
```

Commands never contact a cluster in tests: everything runs through the `Runner`
interface in `internal/kube`, which tests swap for a fake, so the whole suite is
hermetic and fast.

```
cmd/                  one file per subcommand, each self-registering
internal/config/      the YAML schema, its shapes and its validation
internal/registry/    (alias, environment) → workload, namespace, selector
internal/kube/        argv construction and process execution
internal/logfmt/      structured log rendering
internal/ui/          tables, colour, hyperlinks, line prefixes
```

Adding a subcommand means adding a file to `cmd/`; adding a column means adding
an entry to the map in `cmd/columns.go`.
