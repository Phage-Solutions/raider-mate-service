# Raider Mate

Backend service for Raider Mate, a WoW raid and Mythic+ signup system built around the
fact that raiders play more than one role. Owns the schema, the domain logic, and the
REST + HATEOAS API. `raider-mate-bot` and `raider-mate-dashboard` are clients of this
API and hold no domain logic of their own.

## Stack

Go, Postgres via `pgx`, `sqlc` for queries, `goose` for migrations. No ORM.

## Running locally

Requires PostgreSQL 17 that can be run with this command:
```shell
docker run --name raider-mate-db -p 5432:5432 -d --env "POSTGRES_PASSWORD=raider-mate" --env "POSTGRES_USER=raider-mate" postgres:17 postgres
```

Requires Go 1.26+ and Docker.

```
cp .env.example .env   # make exports this to the targets below
make up            # start Postgres
make migrate       # apply migrations
make run           # start the API on :8080
```

Then:

```
curl localhost:8080/healthz
```

## API testing

The `bruno/` directory is a [Bruno](https://www.usebruno.com/) collection for manually
exercising the API. Open it in the Bruno app, select the `local` environment, and run
requests against `make run`. Every endpoint ships its `.bru` file in the same commit
that adds the endpoint.

## Development

```
make test          # go test ./...
make lint           # golangci-lint
make migrate        # apply pending migrations
make sqlc           # regenerate query code from internal/db/queries
```

See `AGENTS.md` for the conventions this repo follows, `docs/design.md` for the schema
and algorithms, and `docs/style.md` for writing style.

## License

AGPLv3. See `LICENSE`. Free to self-host; the hosted instance is what's monetised.
