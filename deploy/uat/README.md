# Flexprice UAT deploy

Jenkins builds `tapfin/flexprice-uat` from the `uat` branch on every push, then runs
`deploy.sh` on the UAT host (`187.127.165.207`). The dashboard has its own pipeline in
`flexprice-front/deploy/uat`.

| Container | Mode | Port |
|---|---|---|
| `flexprice-uat-api` | `api` | host `3020` → `8080` |
| `flexprice-uat-consumer` | `consumer` | none |
| `flexprice-uat-worker` | `temporal_worker` | none |
| `flexprice-front-uat` | nginx | host `3021` → `8080` |

Each deploy runs, in order: Postgres extensions, `migrate postgres up`, `migrate clickhouse`,
`migrate kafka`, Temporal namespace create (if missing), then replaces the worker, consumer
and API, and checks `/health`. All steps are safe to re-run.

## One-time setup

1. **Postgres:** create an empty database and owner. Migrations refuse a database that
   already has tables.
   ```sql
   CREATE ROLE flexprice_uat LOGIN PASSWORD '<password>';
   CREATE DATABASE flexprice_uat OWNER flexprice_uat;
   ```
2. **ClickHouse:** give the Flexprice user rights on a database named `flexprice`
   (the name is fixed in the migrations; `migrate clickhouse` creates it if allowed).
3. **Redis:** pick an unused DB index for `FLEXPRICE_REDIS_DB`.
4. **Kafka:** set `replicationFactor` in `FLEXPRICE_KAFKA_TOPICS` to fit your cluster. All
   topics and consumer groups are prefixed `flexprice_uat_`. If the cluster uses ACLs,
   allow the Flexprice user to create and use `flexprice_uat_*` topics and groups.
5. **Temporal:** nothing to do. The namespace from `FLEXPRICE_TEMPORAL_NAMESPACE` is
   created on the first deploy.
6. **Infisical:** create env `flexprice-uat-backend` from `flexprice-uat.env.example` and env
   `flexprice-front-uat` from the dashboard's example.
7. **Jenkins:** set both jobs to "Pipeline script from SCM", branch `main`, script path
   `deploy/uat/Jenkinsfile`, so the job always runs the Jenkinsfile in the repo.
8. **GitHub:** add a webhook on both repos to `https://codepipeline.tapfin.in/github-webhook/`
   (push events), and run each Jenkins job once by hand so the push trigger registers.
9. **Reverse proxy:** route the UAT API domain to port `3020` and the dashboard domain to
   `3021`, with TLS. Block `POST /v1/auth/signup` after the first tenant is created.

## First tenant

```bash
curl -X POST https://<api-domain>/v1/auth/signup -H 'Content-Type: application/json' \
  -d '{"email":"<admin email>","password":"<password>","tenant_name":"Tapfin UAT"}'
```

Then sign in to the dashboard and create API keys under Developers.

## Logs

```bash
docker logs -f flexprice-uat-api
docker logs -f flexprice-uat-consumer
docker logs -f flexprice-uat-worker
```
