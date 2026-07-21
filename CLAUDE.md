# CLAUDE.md — guidance for Claude Code in this repo

## GitLab labels — never use `Security` except for incidents

Do **NOT** apply the GitLab **`Security`** label to any issue or merge request
unless it is a genuine, confirmed **security incident** — and then only with
**explicit human approval**. Applying it to ordinary feature / hardening / CI /
dev work **breaks the SOC2 process**: such tickets fail the SOC2 gate.

- Default to `Feature`, `Bug`, `CI/CD`, etc. Work that merely *touches* security
  (auth, sanitizers, audit, secret handling) is still `Feature`/`Bug`, not `Security`.
- Using `[Sec]` in a title/description is fine — it is just text; the GitLab
  **label** is what trips SOC2.
