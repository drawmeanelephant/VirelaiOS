# App audits

One file per shipped app, retired from the `polish` audit issues by #3793
(2026-10-27). Audits are living documentation — what the app does, the
interface it ships, coverage, observed defects, and **improvement
candidates** — so they live in the repo, not the ticket queue. Tickets are
for work: the interface-review lane files bounded drift cards off these
audits, and anyone mining for improvement candidates reads
`docs/audits/*.md`, not open issues.

## Format

```markdown
<!-- iface-review:v1 app=<BIN> issue=<retired audit #> -->
# App audit: <name> (<BIN>)

<the audit body — What it is / Interface today / Coverage /
 Observed defects / Improvement candidates / Evidence>
```

The `iface-review:v1` header marks the file as a rotation subject for
`tools/status/interface-review.py` (`just interface-review`): `app=` is the
binary the lane reviews, `issue=` is the retired audit's number so cards
filed before the retirement ("from polish audit #N") still resolve.

## Lane state

Rotation state (last-reviewed stamps + each pass's review notes) lives as
`iface-review` comments on the lane's standing state-tracker issue —
discovered by the `lane-state` label, see the tool's docstring. Deliberately
*not* in these files: the hourly conveyor invokes the lane from any
checkout, and a stamp committed to one checkout's branch would strand the
rotation (this was the design constraint in #3784; #3793 keeps it).
