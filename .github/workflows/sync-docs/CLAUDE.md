# CLAUDE.md — Documentation sync to dash0.com/docs

This directory holds the implementation behind the GitHub Actions workflow
`.github/workflows/sync-docs-to-website.yaml`. Together they copy the operator's Helm chart documentation
into the documentation that powers https://www.dash0.com/docs, applying a declared set of modifications and
opening a pull request against the documentation repository.

## What the workflow does

`.github/workflows/sync-docs-to-website.yaml` defines a single job, `sync-docs`, that runs the following steps:

1. **Checks out** this repository (`dash0-operator`) and sets up Python 3.x.
2. **Installs dependencies** from `requirements.txt` (just PyYAML).
3. **Applies the doc transformations** by running `apply-transformations.py` with three arguments:
   - source root: `helm-chart/dash0-operator`
   - transformation declarations: `transformations.yaml`
   - output directory: `${RUNNER_TEMP}/transformed-docs`
4. **Checks out the target repository** (the docs repo) using a fine-grained PAT.
5. **Copies** the transformed tree (`about-kubernetes.md` plus `dash0-operator/*.md`) into the configured target directory.
6. **Creates or updates a pull request** in the target repository: it stages the target directory, and if there
   is a meaningful diff, commits to the branch `sync-dash0-operator-helm-chart-docs`, force-pushes it, and opens
   a PR against `main` (or relies on the force-push to update an already-open PR).

### Configuration (repository secrets)

- `DASH0_DOCS_REPO_GITHUB_PAT` — fine-grained PAT scoped to the target repo with **Contents: Read and write**
  (to push the branch) and **Pull requests: Read and write** (to open the PR).
- `SYNC_DOCUMENTATION_TARGET_REPOSITORY` — `owner/name` of the documentation repository.
- `SYNC_DOCUMENTATION_TARGET_DIRECTORY` — path of the directory within that repo holding the documentation pages.

## How it is implemented in this directory

| File | Role |
| --- | --- |
| `transformations.yaml` | **Source of truth.** Declares the common and per-file transformations, the source→target file mapping, and the frontmatter `title`/`description` for each page. |
| `apply-transformations.py` | The engine. Reads `transformations.yaml`, applies the transformations to each source file, and writes the results to the output directory. Knows *how* to apply transformations; it does not hard-code *which* ones. |
| `requirements.txt` | Python dependencies (PyYAML). |
| `test-locally.sh` | Runs the transformation step locally in a throwaway venv, writing output to `helm-chart/dash0-operator/.transformed-docs` for inspection — mirrors the workflow without touching the docs repo. |

### `transformations.yaml` structure

- **`common`** — a list of transformations applied to every synced file, in order, before the file-specific ones.
  Currently it strips the leading top-level heading (the generated frontmatter `title` replaces it).
- **`files`** — one entry per page. Each entry has:
  - `source` — path relative to `helm-chart/dash0-operator`.
  - `target` — path relative to the target directory in the docs repo.
  - `title` / `description` — rendered into the generated frontmatter.
  - `transformations` — optional list of transformations applied to this file only, after the common ones.

The documentation is the top-level `README.md` (renamed to the section landing page `about-kubernetes.md`) plus
the topic files in `docs/*.md` (which keep their names but move into a `dash0-operator/` subdirectory in the target
repo — i.e. the source `docs/` directory is renamed to `dash0-operator/`). The topic files move together, so the
sibling links between them keep pointing at the right page; all relative markdown links — both the sibling links and
the ones that cross a rename boundary — have their `.md` suffix dropped, because the target website serves pages under
extensionless URLs (see below).

### Supported transformation types

- `prepend` — insert `content` at the very beginning of the document.
- `replace-regex` — replace matches of the Python regex `find` with `replace`. Optional `flags`: `multiline`,
  `dotall`, `ignorecase`.
- `remove-line` — remove the whole line containing the literal `line` marker; collapses any resulting run of
  blank lines to a single blank line.

By default every `replace-regex` / `remove-line` must match at least once, otherwise the workflow **fails** —
this guards against the docs drifting away from `transformations.yaml` so a modification silently becomes a
no-op. Set `required: false` on an individual transformation to allow zero matches.

### Per-file processing pipeline (`apply-transformations.py`)

For each `files` entry the script:

1. Applies the `common` transformations, then the file-specific `transformations`.
2. Rewrites relative markdown links, dropping the `.md` suffix (the target website serves pages extensionless):
   links that cross a rename boundary (e.g. `../README.md#…` → `../about-kubernetes#…`), links pointing into the
   renamed `docs/` directory (e.g. `docs/configuration.md#…` → `dash0-operator/configuration#…`), and same-directory
   sibling links between topic files (e.g. `configuration.md#…` → `configuration#…`).
3. Prepends a generated frontmatter block (`title`, `description`, `lastUpdated`).
4. Writes the result to `target` inside the output directory.

### Placeholders

Inserted/replacement text (the `content` of `prepend` and the `replace` of `replace-regex`) may use placeholders,
expanded when applied (not in `find` patterns). The only one currently supported is `$timestamp` — the current
UTC date/time, computed once per run so all occurrences render identically.

## When editing

- **Adding or renaming a docs page** in `helm-chart/dash0-operator/docs`: add or update the corresponding
  `files` entry in `transformations.yaml` (otherwise the completeness guard fails the run).
- **Changing how docs are adapted**: edit `transformations.yaml`, not the Python script.
- **Verify locally** with `./test-locally.sh` and inspect the output under
  `helm-chart/dash0-operator/.transformed-docs`.
- After editing `test-locally.sh`, run `make shellcheck-lint`.
