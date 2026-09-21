# Scope — project status and project write actions

Enrich what the MCP can answer about a project: whether its service report
(*acta*) exists, was sent and signed; whether the project has been billed; which
proposals and orders belong to it; and how many days a signed report has been
waiting to be billed. Plus the three write actions the workflow needs: create a
project, change its reference, attach files.

Drafted 2026-09-21. Every number below was measured against the local
`dolibarr-test` instance (Dolibarr 23, container `dolitest-db`) on that date, not
inferred from documentation. Re-measure before relying on it against production.

## 1. Why a new tool rather than a richer `dolibarr_get`

`dolibarr_get` is entity-generic: one code path serves ten entities and would pay
the cost of five extra queries on every call. More importantly, the fields this
scope is about — *billed yes/no*, *days since the report* — are not columns. They
are derived across four tables. Pushing them into `SearchResult`, the struct all
ten entities share, would distort a shared contract to serve one entity.

The repository already has the precedent: `dolibarr_pep_budget` is a
domain-specific tool alongside the generic CRUD verbs.

## 2. Baseline — what exists today

Registered in `internal/tools/registry.go:39`, nine tools. Relevant to this scope:

| Tool | Channel | Covers |
|---|---|---|
| `dolibarr_get` (`get.go:26`) | DB | `fetchProject` (`doldb/fetch.go:371`) + extrafields (`:458`) + tasks (`:499`) |
| `dolibarr_search` (`search.go:32`) | DB | `searchProjects` (`doldb/search.go:370`) |
| `dolibarr_create` (`create.go:15`) | REST | projects — sends `ref: "auto"` (`validate.go:23`) |
| `dolibarr_update` (`update.go:17`) | REST | `PUT /projects/{id}`, no ref handling |
| `dolibarr_document` (`document.go:71`) | REST | upload/list, `modulepart=project` |

What is missing: no `invoices` entity anywhere in `mapper.ValidEntities()`
(`mapper/fields.go:178`); no read of `llx_facture`, `llx_propal`, `llx_commande`
or `llx_fichinter` from a project; and no link traversal in either direction —
a proposal knows its project, a project knows nothing.

## 3. Ground truth — measured, not assumed

Three measurements drive the whole design.

### 3.1 Sales invoicing does not live in Dolibarr

| Table | Rows | Most recent |
|---|---|---|
| `llx_propal` | 3297 | 2026-07-18 |
| `llx_commande` | 1252 | 2026-07-14 |
| `llx_projet` | 459 | 2026-07-17 |
| `llx_fichinter` | 217 | — |
| **`llx_facture`** | **17** | **2025-10-10** |

Only 11 of 459 projects carry an invoice through `llx_facture.fk_projet`, and
`llx_element_element` holds 12 links toward `facture` in total (10 from
`commande`, 2 from `propal`). `llx_commande.facture = 1` is set on 2 of 1252
orders.

The real signal is the **proposal**: 477 proposals sit in `fk_statut = 4`
(classified billed), covering **120 projects**, and `llx_actioncomm` holds 484
`AC_PROPAL_CLASSIFY_BILLED` events carrying the date of that classification.

**Decision: a project counts as billed when any of its proposals is in
`fk_statut = 4`.** The date comes from the matching `AC_PROPAL_CLASSIFY_BILLED`
event, not from an invoice.

### 3.2 The *acta* is a `fichinter`

The project page lists them as "actas de entregas asociadas al proyecto". They
are Dolibarr intervention sheets, linked by `llx_fichinter.fk_projet`. The
lifecycle columns, verified against `information_schema`:

| Column | Meaning here | Coverage (217 rows) |
|---|---|---|
| `datec` | row created | all |
| `date_valid` | report validated | 199 |
| `last_main_doc` | **the PDF was generated** | 176 |
| `datei` | date the work was done | — |
| `fk_statut` | 0 draft / 1 validated / 2 billed / 3 closed | 14 / 199 / 0 / 4 |
| `signed_status` | native signature flag | NULL·0 / 3 / 9 |

`signed_status` is a **separate column from `fk_statut`**. `sgsignrequest` calls
`setSignedStatus($user, 9, ...)`, which writes `signed_status = 9`, leaving
`fk_statut` untouched. Any query that conflates the two is wrong.

The value `signed_status = 3` appears on 2 rows and is not yet explained — see
§9, open decisions.

### 3.3 "Sent" is barely recorded, "delivered" does not exist

`llx_actioncomm` is the event log — `fk_element`, `elementtype`, `datep`,
`email_to`, `email_msgid`. For interventions it holds 354 `AC_FICHINTER_VALIDATE`
events but only **7** `AC_FICHINTER_SENTBYMAIL`. Reports are validated in
Dolibarr; they are not emailed from it.

There is no delivery acknowledgement anywhere — not in core, not in
`sgsignrequest`. LibreSign does not report one and the module does not model one.
**Delivery is out of scope** (§7) and left as an open decision (§9).

## 4. Decisions taken

1. The *acta de cierre* is the `fichinter`, linked by `fk_projet`.
2. `sgsignrequest` is merged to `develop` **before** this work starts (§8).
3. Billed = any proposal of the project in `fk_statut = 4`.
4. The ageing clock falls back through a cascade and always reports which source
   it used (§5.4).

## 5. Scope A — the read surface

### 5.1 Tool contract

New tool `dolibarr_project_status`, registered in `internal/tools/registry.go`,
reading through `Deps.DB` only. No REST call; it is a query, and the repository's
split puts reads on the database.

Two modes, selected by whether an identifier is supplied.

```
Input:
  project_id  int64    detail mode — the project
  ref         string   detail mode — alternative to project_id
  customer_id int64    list mode — filter
  status      int      list mode — llx_projet.fk_statut
  has_acta    bool     list mode — project has at least one fichinter
  acta_signed bool     list mode — at least one with signed_status = 9
  billed      bool     list mode — at least one proposal in fk_statut = 4
  min_days    int      list mode — ageing at or above N days
  limit       int      default 25, max 100
  offset      int
```

Detail mode returns the blocks of §5.2 for one project. List mode returns one row
per project carrying only `proyecto`, `facturacion` and `alertas` — the heavy
per-document blocks are skipped, so the list stays one query per block rather
than one per project (§8, N+1).

Supplying both an identifier and a list filter is an error, not a silent
precedence rule.

### 5.2 Response blocks

**`proyecto`** — from `llx_projet`, already covered by `fetchProject`. Adds
`date_close`, which the current fetch does not read.

**`actas[]`** — one entry per `llx_fichinter` of the project:

```sql
SELECT fi.rowid, fi.ref, fi.ref_client, fi.datei, fi.datec, fi.date_valid,
       fi.fk_statut, fi.signed_status, fi.duree,
       fi.last_main_doc, fi.description
  FROM llx_fichinter fi
 WHERE fi.entity IN (...) AND fi.fk_projet = ?
 ORDER BY fi.datei DESC, fi.rowid DESC
```

Derived per entry: `pdf_generado` (`last_main_doc` non-empty), `estado`
(label for `fk_statut`), `firmada` (`signed_status = 9`).

**`actas[].enviada`** — from the event log, one grouped query for all of the
project's reports:

```sql
SELECT a.fk_element, MIN(a.datep) AS primera, MAX(a.datep) AS ultima,
       COUNT(*) AS envios
  FROM llx_actioncomm a
 WHERE a.elementtype = 'fichinter'
   AND a.code = 'AC_FICHINTER_SENTBYMAIL'
   AND a.fk_element IN (...)
 GROUP BY a.fk_element
```

Expect this to be empty for almost every report (§3.3). Empty means *not
recorded*, which is not the same as *not sent*; the field says so explicitly
rather than reporting `false`.

**`actas[].firma`** — from `sgsignrequest`, and **only when its tables exist**
(§8). Joined on `fk_fichinter`:

```sql
SELECT r.fk_fichinter, r.status, r.date_creat, r.date_sent, r.date_completed,
       r.signed_file_name, r.libresign_status, r.error_message,
       r.poll_count, r.date_last_poll
  FROM llx_sgsignrequest_request r
 WHERE r.entity IN (...) AND r.fk_fichinter IN (...)
```

Signers come from `llx_sgsignrequest_signer` by `fk_request`: `display_name`,
`signer_type`, `status`, `date_signed`, `sort_order`.

Expiry is **not stored**. It is derived as
`date_sent + SGSIGNREQUEST_REQUEST_TTL_DAYS` (default 30, read from `llx_const`,
never hardcoded) and reported as `vence_el` plus `vencida`. A status already
written as `expired` by the module's cron wins over the derivation.

The signed PDF is **not indexed in ECM**. Its path is rebuilt as
`<ficheinter dir_output>/<fichinter.ref>/<signed_file_name>`. The tool reports
the filename; resolving it to bytes is out of scope.

**`ofertas[]`** — `llx_propal` by `fk_projet`: `ref`, `ref_client`, `datep`,
`fin_validite`, `fk_statut` with its label, `total_ht`, `total_ttc`. Plus
`facturada` (`fk_statut = 4`) and `fecha_facturacion` from the grouped
`AC_PROPAL_CLASSIFY_BILLED` events, same shape as the send query above.

**`pedidos[]`** — `llx_commande` by `fk_projet`: `ref`, `ref_client`,
`date_commande`, `fk_statut` with its label, `total_ht`, `facture`. The `facture`
flag is reported as measured and explicitly **not** used as the billing signal
(§3.1).

**`facturacion`** — derived, never a raw column:

```
facturado          bool     any proposal in fk_statut = 4
fecha              date     earliest AC_PROPAL_CLASSIFY_BILLED among those
monto_ht           decimal  sum of total_ht of the billed proposals
fuente             string   "propal_statut_4" — always stated
facturas_dolibarr  []       llx_facture rows by fk_projet, when any exist,
                            reported for completeness, not used for `facturado`
```

`fuente` is mandatory. A consumer must never have to guess where a yes came from.

### 5.3 Alerts

Computed, one boolean plus supporting dates each:

- `acta_sin_facturar` — a signed report, no billed proposal. Carries `dias`.
- `acta_sin_firmar` — validated report, `signed_status` not 9. Carries `dias`.
- `proyecto_cerrado_sin_acta` — `fk_statut = 2` and no `fichinter`.
- `proyecto_cerrado_sin_facturar` — `fk_statut = 2` and no billed proposal.
- `firma_vencida` — derived expiry passed, still not signed.
- `firma_en_error` — request `status` is `error` or `rejected`, with the message.

### 5.4 The ageing cascade

The clock for `dias` starts at the first of these that exists, and the response
always names which one was used in `origen_fecha`:

1. `AC_FICHINTER_SENTBYMAIL` — the report was emailed. `origen_fecha: "enviada"`.
2. `llx_fichinter.date_valid` — validated. `origen_fecha: "validada"`.
3. `llx_fichinter.datei` — the day the work was done. `origen_fecha: "intervencion"`.

If none exists, `dias` is null and `origen_fecha` is `"sin_fecha"`. A missing
clock is reported as missing; it is never defaulted to zero.

Given §3.3, expect `"validada"` in practice. That is the point of naming the
source: the number is usable today and becomes sharper by itself the day the team
starts sending reports from Dolibarr.

## 6. Scope B — the write actions

### 6.1 Create a project — already works, close the gaps

`dolibarr_create` with `entity: "projects"` works: `validateCreate` requires
`title`, `stripAutoNumberRef` supplies `ref: "auto"`, verified in
`docs/dolibarr-api-evidence.md:53`.

Scope here is only: confirm `customer_id` → `socid` and the date aliases survive
the round trip, under the gated end-to-end test (§11). No code change expected —
and if none is needed, that is the finding, recorded in the evidence file.

### 6.2 Change the reference — **the highest risk in this scope**

A `ref` in `dolibarr_update`'s `data` travels untouched to
`PUT /projects/{id}` (`update.go:22`). Nobody has verified Dolibarr honours it.
`docs/dolibarr-api-evidence.md` documents renaming a thirdparty and nothing about
renaming a project.

There is direct precedent for this failing silently: the mapper used to rename
`name` to `nom`, and every thirdparty rename returned `200 OK` while discarding
the change (issue #19, `evidence:68`).

Therefore:

- Measure first. `PUT /projects/{id}` with a new `ref`, then **re-read the
  object** and compare. A `200` is not evidence.
- Record the result in `docs/dolibarr-api-evidence.md` either way.
- If the API drops it, do not paper over it in the mapper. Report the finding and
  decide the route then — this scope does not pre-authorise a workaround.
- The test asserts the **re-read value**, not the response code. This is
  non-negotiable and is the acceptance criterion for this item.
- Renaming collides with the numbering mask that produced `ref: "auto"` at
  creation. Whether Dolibarr guards against a duplicate ref is part of the
  measurement.

### 6.3 Attach files — works, two gaps to close

`dolibarr_document` uploads and lists with `modulepart=project`.

- **Gap 1** — `list` returns 404 when the entity has no files. That means *no
  documents*, but `writeError` surfaces it as an API failure
  (`evidence:32,141`). Translate it to an empty list for the document tool only,
  leaving the generic error path untouched.
- **Gap 2** — upload requires `ref` because the file lands in a directory named
  after it (`document.go:92`). Accept `id` and resolve it to the ref through the
  database before building the request, keeping the existing error when neither
  is supplied.
- The listing returns serialised PHP objects with private-property keys
  containing NUL bytes; only `filename`, `size`, `date`, `fullname`, `level1name`
  are usable (`evidence:145`). Normalise to those five and drop the rest.

## 7. Out of scope

- Issuing invoices, or any write to `llx_facture`.
- Modelling *delivered* — the data does not exist anywhere (§3.3, §9).
- Writing time entries: `POST /tasks/{id}/addtimespent` is broken upstream
  (`evidence:103`).
- Downloading the signed PDF's bytes.
- Reading the PEP budget — `dolibarr_pep_budget` stays write-only.
- Any change to `sgsignrequest` itself.

## 8. Risks and dependencies

| Risk | Impact | Handling |
|---|---|---|
| `sgsignrequest` not merged | The `firma` block has no tables | **Hard prerequisite** — merge its 9 PRs first (decided). The tool still probes for the tables at startup and degrades with a stated reason, because production and `dolibarr-test` will not upgrade on the same day. |
| Changing a ref silently ignored | The action looks like it works | §6.2 — re-read assertion, no exceptions |
| N+1 in list mode | 459 projects × 5 blocks | One grouped query per block over the page's ids, never a query per project. Asserted by a test that counts queries. |
| Billing signal changes | If S&G starts issuing real invoices, `propal.fk_statut = 4` stops being the whole truth | `fuente` is always reported, and `facturas_dolibarr` already carries the real rows when they exist |
| `signed_status = 3` unexplained | 2 rows classify wrong | §9 — resolve before release; until then it is reported raw, not bucketed |
| Entity scoping | Multi-entity installs leak rows | Every query filters `entity IN (...)`, as `doldb` already does |

## 9. Open decisions

1. **Delivery.** Nothing records it. Options: leave it out; add an extrafield on
   `fichinter`; or treat the client's signature as the proxy. Not decided, and
   not blocking §5.
2. **`signed_status = 3`.** Meaning unconfirmed. Needs checking against
   `CommonObject`'s constants in Dolibarr 23 before it is given a label.
3. **Reports not attached to a project.** Some rows have no `fk_projet`. They
   cannot surface through this tool at all. Acceptable, or worth a separate
   orphan check?

## 10. Phases and acceptance

**Phase 0 — prerequisite.** `sgsignrequest` merged to `develop`.

**Phase 1 — read, core only.** Blocks `proyecto`, `actas[]` (without `firma`),
`ofertas[]`, `pedidos[]`, `facturacion`, `alertas`, plus the ageing cascade. Both
modes.
*Done when:* detail mode reproduces, for a known project, the same reports the
project page lists; `facturacion.facturado` is true for exactly the 120 projects
holding a proposal in `fk_statut = 4`; list mode issues a bounded number of
queries independent of the page size.

**Phase 2 — read, signature.** The `firma` block, signers, derived expiry, and
the runtime probe for the module's tables.
*Done when:* a project whose report is signed reports `firmada: true` with its
`date_completed` and per-signer dates; with the tables absent, the block is
omitted with a stated reason and no other block degrades.

**Phase 3 — write.** §6.1, §6.2, §6.3.
*Done when:* the ref change is **measured and written into
`docs/dolibarr-api-evidence.md`**, whatever the outcome; the document listing
returns an empty list instead of an error for an entity with no files; upload
accepts an `id`.

## 11. Testing

Following the repository's existing split:

- **Unit** — `go test -count=1 ./...`. Write paths assert the **outgoing
  payload** via the `newFakeDolibarr` helper, matching `project_test.go` and
  `document_test.go`.
- **Read, gated** — `DOLIBARR_IT=1 go test ./internal/doldb/`, against the local
  instance. Covers the block queries and the derived fields.
  `DOLIBARR_IT_SNAPSHOT=<path>` dumps the result so a change can be diffed before
  and after.
- **Write, gated** — `DOLIBARR_E2E=1 go test ./internal/tools/`, real writes,
  never against production. The ref change lives here and asserts the re-read.
- **Registration guards** — `TestRegisterDoesNotPanic` and
  `TestOutputSchemasHaveNoBooleanSubschemas` already cover a new tool, provided
  every `any` field carries a `jsonschema` description (`get.go:16`).
- The hardcoded tool count in `cmd/dolibarr-mcp/main.go:63` moves from 9 to 10.
