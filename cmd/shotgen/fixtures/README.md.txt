# Revenue service

The service aggregates **order events** into the numbers the dashboard shows.
It is deliberately boring: one *ingest* worker, one rollup job, one report.

## Running it

1. Start the ingest worker with `make ingest`.
2. Wait for the rollup timer, or force one with `make rollup`.
3. Open <http://localhost:8080/report> — or see [the API notes](api/orders.http).

> Rollups are idempotent. Running one twice is boring, not dangerous.

| Region | Q3 revenue | Trend |
| ------ | ---------- | ----- |
| EMEA   | 1,204,880  | up    |
| AMER   | 998,140    | up    |
| APAC   | 613,020    | flat  |

```go
totals := rollup(events)
fmt.Println(report(totals, 3))
```

Anything not covered here lives in `docs/` — start with `docs/ingest.md`.
