# Design Template Overview

Design docs are now the only planning artefact. Every doc must use the single lightweight template `design/TEMPLATE.md`, which keeps scope tight and implementation-ready.

## Template Sections
- **Why** — explain the problem or opportunity and link to upstream evidence.
- **What to do** — summarize the change in plain terms so reviewers know the intended behaviour.
- **Where to change** — call out the code paths, files, or services that will be edited.
- **How to test** — outline the verification steps, automated or manual, that must pass before closing the doc.

## Using the Template
- Copy `design/TEMPLATE.md` into `docs/design/<subject>/README.md` and fill in each section with concise, actionable details.
- Keep the document minimal; if it grows beyond this format, split the work into multiple design docs instead of expanding the template.
- Add or update the entry for the doc inside `docs/design/QUEUE.md` so collaborators can reserve it before implementation.
- Once the doc is approved, update `docs/design/README.md` with the summary, status checkbox, dependency notes, and a dated verification line. Then log the verification evidence in `CHANGELOG.md`.
