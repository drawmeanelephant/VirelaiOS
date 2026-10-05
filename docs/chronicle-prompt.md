# Chronicle authoring prompt

This is the current prompt for new chronicle batches. It supersedes the
earlier grokbot packaging request; do not carry forward that delivery step
from the historical conversation or batch files.

Read the PRs in the requested batch. Write one Markdown chronicle entry per
PR, with honest flaw-finding: separate observed results from inference, and
check claims against the changes and their evidence. Skip issue numbers that
are not PRs. Preserve the established illuminated-plate style, one milestone
banner or Interlude image per entry. Do not mistake a GitHub milestone number
for the project's M-series.

## Delivery

Keep the chronicle in the repository's PR/issue comments. Entries are
Markdown; banners are inline images. Do not package or deliver archives,
PDFs, or local download bundles. Do not use `<picture>`.

Original backlog inputs are read-only. Use the matching PR's entry title as
the banner caption, upload its original image, and leave the supplied prose
and other batches untouched. Do not commit the input collection or images.

For each image, use the repo-local publisher:

```sh
mkdir -p .build artifacts/chronicle
(cd tools/chronicle && CGO_ENABLED=0 go build -o ../../.build/chronicle .)
.build/chronicle --entry 716 --comment EXISTING_ENTRY_COMMENT_ID \
  --state artifacts/chronicle/pr-716.json pr-716.png 'Chronicle banner for PR #716'
```

`--comment` appends only the image Markdown to the existing entry, preserving
its prose. Omit it to create a banner-only chronicle comment on that PR, for
example when the original prose has not been saved to GitHub. Do not migrate
existing prose as part of the image backlog.

The publisher checks push access and obtains the numeric repository ID with
`gh api`. It sends the image with a Bearer token to
`https://uploads.github.com/user-attachments/assets`, saves the returned
`user-attachments` URL in the entry comment body, reads that body back, and
only then checks authenticated resolution: HTTP 302 to signed S3, followed
by HTTP 200 with the uploaded image content-type and image bytes. Only after
those steps does stdout contain the claimed Markdown line.

Keep the receipt at `--state`. If saving or verification fails, retry the
same command with the same receipt, image, caption, entry and comment.
**Never re-upload on the first 404.** The initial upload is not a claim;
the saved body is. A verification failure does not mean the upload failed.
The tool retries post-save 404s, never uploads again from a valid receipt,
and emits no Markdown on failure.

An interrupted upload can leave an attempted-upload receipt without a URL,
or a receipt lock. Stop and inspect these; do not delete them and blindly
upload again. Distinct images must have distinct receipt paths.

Before a batch, run `gh auth status` and confirm push access. Treat attachment
visibility as repository access-controlled; do not depend on anonymous
hotlinking or change repository visibility. If public images are required,
stop and ask the owner on #1977. If the upload endpoint returns 404 or 422,
stop and report on #1977. Do not reverse-engineer a replacement or switch to
the pinned orphan-branch fallback without the owner's decision.

## Tool checks

The tool uses the Go standard library (BSD-3-Clause) and the already-required
GitHub CLI (MIT). It adds no modules or runtime dependencies beyond `gh`.
Build with `CGO_ENABLED=0`; recipients do not need a Go installation to run
the resulting host binary. It accepts PNG, JPEG, GIF and WebP by content,
not merely by filename, and does not store credentials or signed S3 URLs.

```sh
(cd tools/chronicle && go test -race ./... && go vet ./...)
.build/chronicle --entry 1977 --state artifacts/chronicle/red.json \
  --upload-endpoint http://127.0.0.1:1/user-attachments/assets \
  tests/fixtures/png/solid_rgb_4x4.png 'Chronicle endpoint failure probe'
```

The second command must exit nonzero with empty stdout. The test endpoint
override accepts only HTTP loopback; it cannot send the Bearer token to an
arbitrary replacement service. A green run uses the normal endpoint, saves
the image-only comment, and prints its verified Markdown. Keep logs in
gitignored `artifacts/`, not in the source tree.
