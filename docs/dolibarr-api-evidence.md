# Dolibarr REST API — measured behaviour

What the Dolibarr REST API actually does for the project lifecycle, established by
sending real requests to a Dolibarr **23** instance and reading the responses.
Nothing here is inferred from documentation.

Recorded 2026-09-10 while building the project capabilities. Re-verify before
relying on any of it against a different Dolibarr version — several entries below
are version-specific, and one is an upstream bug that may be fixed later.

## Why this file exists

Two of these findings are silent failures: the API answers `200 OK` and discards
the work. No amount of reading the response tells you it happened. They cost real
debugging time to find, so they are written down rather than rediscovered.

## Summary

| Capability | Endpoint | Verdict |
|---|---|---|
| Create project | `POST /projects` | Works — **requires `ref: "auto"`** |
| Update project | `PUT /projects/{id}` | Works — **`description`, never `desc`** |
| Project extrafields | `PUT /projects/{id}` with `array_options` | Works |
| Validate project | `POST /projects/{id}/validate` | Works — needs `notrigger` as an **int** |
| Close project | `POST /projects/{id}/close` | **Does not exist** (404) |
| List project tasks | `GET /projects/{id}/tasks` | Works |
| Create task | `POST /tasks` | Works — requires `ref: "auto"` **and** `label` |
| Read time entries | `GET /tasks/{id}/timespent` | Works |
| **Log time** | `POST /tasks/{id}/addtimespent` | **Broken upstream** (500) |
| Upload document | `POST /documents/upload` | Works — base64 in JSON, **keyed by ref** |
| List documents | `GET /documents?modulepart=X&id=N` | Works — 404 means "none", not "no endpoint" |
| Link document to project | `PUT /<document>/{id}` with `fk_project` | Works |
| Document ↔ document links | — | **No REST API** |

## The two silent failures

### A project description sent as `desc` is discarded

```
PUT /projects/{id}  {"desc": "texto"}         -> 200 OK, description UNCHANGED
PUT /projects/{id}  {"description": "texto"}  -> 200 OK, description updated
```

The API accepts the payload, reports success, and drops the field. `desc` is
correct for proposal and order *lines*, which is what makes this easy to get
wrong: one global rename looks right until a project loses its description.

Handled by `mapper.MapEntityToDolibarr`, which is entity-aware.

### A project cannot be created without a ref

```
POST /projects  {"title": "x"}                -> 400 Bad Request: ref field missing
POST /projects  {"ref": "auto", "title": "x"} -> 200, returns the new id
POST /proposals {"socid": 42}                 -> no complaint about ref
```

Projects and proposals genuinely differ. Proposals want the key **absent**;
projects want the literal string `"auto"`, which is what asks Dolibarr to apply
its numbering mask. Sending a real ref overrides the mask, which is why it cannot
simply be passed through.

Tasks behave like projects. Handled by `mapper.AutoRefValue`.

## Tasks

`POST /tasks` is a first-class resource, so tasks need no dedicated tool — they
go through the generic CRUD pipe.

```
POST /tasks {"label": "x", "fk_project": 1}                 -> 400 ref field missing
POST /tasks {"ref": "auto", "fk_project": 1}                -> 400 label field missing
POST /tasks {"ref": "auto", "label": "x", "fk_project": 1}  -> 200
```

The parent project is keyed **`fk_project`** in the API even though the column is
`fk_projet`. Workload is in seconds: `planned_workload: 28800` is 8 hours.

## Logging hours is broken in Dolibarr 23

`POST /tasks/{id}/addtimespent` returns `500` for every payload shape tried,
including query-string parameters. The server log shows a PHP fatal raised
**before** the handler runs:

```
Uncaught TypeError: Illegal offset type in isset or empty
  in includes/restler/framework/Luracast/Restler/Data/Validator.php:427
```

`Validator::validate()` evaluates `isset(static::$preFilters[$info->type])`, and
`addTimeSpent`'s docblock declares union types — `@param datetime|string $date`
and `@param int|null $progress` (`projet/class/api_tasks.class.php:596-627`).
Restler parses those into an array and uses it as an array offset, which is fatal
on modern PHP.

Reading time entries works, so only the write path is affected. Options if this
capability is needed: a custom module endpoint (as `sgcosting` did for the PEP
budget), or an upstream docblock fix.

## Documents

Uploads travel as an ordinary JSON body with the bytes base64-encoded — **no
multipart support is required** in the HTTP client.

```json
POST /documents/upload
{
  "filename": "informe.pdf",
  "modulepart": "project",
  "ref": "PJ2012-0080",
  "filecontent": "<base64>",
  "fileencoding": "base64"
}
```

Keyed by **ref**, not id: the file is stored in a directory named after the
document reference. `GET /documents?modulepart=project&id=1` lists them and
answers `404` when the entity has no files — that is "none", not a missing
endpoint.

The listing returns PHP-serialised objects carrying private-property keys (with
`\x00` bytes). The usable fields are `filename`, `size`, `date`, `fullname` and
`level1name`.

`modulepart` accepts readable aliases (`project`, `project_task`, `proposal`,
`order`, `supplier_order`, `thirdparty`, ...) per `api_documents.class.php`.

## Linking documents

Attaching a document to a project is done on the **document**, through its own
`fk_project` column:

```
PUT /proposals/{id} {"fk_project": 483}  -> 200, verified in llx_propal
```

Document-to-document links (`llx_element_element`, e.g. order ↔ invoice) have no
REST API: `/projects/{id}/linked`, `/linkedobjects` and `/documents/link` all
answer 404, and `/setobjectlinked` answers 501.

## Extrafields

`PUT /projects/{id}` with `{"array_options": {"options_<field>": "value"}}` works
and round-trips.

On read, note that `llx_*_extrafields` tables carry Dolibarr's own columns
(`rowid`, `fk_object`, `tms`, `import_key`) alongside the configured fields. They
must be filtered out, or the row's modification timestamp looks like a business
field.

## Reproducing this

A disposable Dolibarr 23 in Docker (`dolibarr/dolibarr:23` plus MariaDB) is
enough, and its container also carries the ERP source, which is how the Restler
bug above was traced. API keys live in plaintext in `llx_user.api_key`.

Never run write probes against production.
