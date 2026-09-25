# Joe - Postgres query optimization
Boost your backend development process

![Joe Bot demonstration](./assets/demo.gif)

Joe gives developers a safe way to experiment on an automatically provisioned,
production-size database testing replica. It provides recommendations for
query optimization and the ability to roll back changes.

## Status

The project is in an early stage. However, some teams already use it
extensively in their daily work. Because production is not involved, it is
easy to try and start using it.

Please support the project giving a GitLab star (it's on [the main page](https://gitlab.com/postgres-ai/joe),
at the upper right corner):

![Add a star](./assets/star.gif)

## Installation
Follow the [tutorial](https://postgres.ai/docs/tutorials/joe-setup) to install Joe Bot and start using it for Postgres query optimization.

## Concurrent Web UI experiments

The Web UI can open another experiment in a separate browser tab. Each experiment
has its own DBLab clone, connections, reset/termination commands, and idle lifecycle.
The experiment ID stays in that tab's URL so reloads resume the same experiment.
Closing a tab does not cancel a running query. DBLab capacity and idle policies
still apply; host CPU, storage and memory remain shared. Request quotas remain
per user, not per experiment. Slack behavior is unchanged.

The `/webui/channels` response advertises `multiple_experiments: true`. The matching
Platform UI only exposes the new action when the selected Joe instance advertises
support. Deploy the Platform ownership-check migration and upgraded Joe before
exposing the UI. Existing persisted Web UI sessions are reindexed without replacing
their clones. PostgreSQL 18 database subclones are not part of this implementation.

For rollback, first remove the new UI action, then drain active experiments before
rolling back Joe: old Joe versions cannot route multiple experiments for one user.
Preserve `sessions.json` and do not delete clones as an automatic rollback step.
The Platform migration has a revert, but reverting its ownership checks is not
required to disable the feature and should not be part of routine rollback.

## Development
See our [GitLab Container Registry](https://gitlab.com/postgres-ai/joe/container_registry) for develop builds. 

### Development tools
- [`cmd/explainrender`](./cmd/explainrender) — converts `EXPLAIN (FORMAT JSON)` into PostgreSQL's standard text plan via `pkg/pgexplain` (the JSON→text translation Joe uses because psql can't convert a JSON plan back to text). Handy for debugging that translation and diffing it against PostgreSQL's own text `EXPLAIN` across versions.

## Community

Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe

To discuss Joe, [join our community Slack](https://slack.postgres.ai/)
