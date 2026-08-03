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

## Development
See our [GitLab Container Registry](https://gitlab.com/postgres-ai/joe/container_registry) for develop builds. 

### Development tools
- [`cmd/explainrender`](./cmd/explainrender) — converts `EXPLAIN (FORMAT JSON)` into PostgreSQL's standard text plan via `pkg/pgexplain` (the JSON→text translation Joe uses because psql can't convert a JSON plan back to text). Handy for debugging that translation and diffing it against PostgreSQL's own text `EXPLAIN` across versions.

## Community

Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe

To discuss Joe, [join our community Slack](https://slack.postgres.ai/)
