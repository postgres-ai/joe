# AGENTS.md

## New features (PostgresAI team)

For a new feature or any change spanning several repos, use the `/feature-loop` skill. It passes
every story end to end before review and caps REV at 3 rounds (about 1 hour per repo), so it
replaces a separate open-ended `/rev-loop`. Skip it for small fixes.

The skill lives in [postgres-ai/air](https://gitlab.com/postgres-ai/air/-/tree/main/skills)
(team access only). Install it with:

```bash
claude plugin install feature-loop --marketplace git@gitlab.com:postgres-ai/air.git
```

If the skill isn't installed, carry on without it.
