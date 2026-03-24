# Deploy workflow – GitHub Actions inputs

Configure these in the repo: **Settings → Secrets and variables → Actions**.

---

## Secrets (sensitive – use **Secrets**, not Variables)

| Name | Example value | Description |
|------|----------------|-------------|
| `AWS_ACCOUNT_ID` | `123456789012` | Your AWS account ID (12 digits). Used in OIDC role ARN. |
| `AWS_REGION` | `ap-south-1` | Default AWS region for build (ECR login). Deploy uses region from targets. |

---

## Variables (non-sensitive – use **Variables**)

| Name | Example value | Description |
|------|----------------|-------------|
| `ECR_REGISTRY` | `123456789012.dkr.ecr.ap-south-1.amazonaws.com` | ECR registry host (no `https://`, no trailing slash). |
| `ECR_REPOSITORY` | `my-app` | ECR repository name. Image tag is `${{ github.sha }}`. |
| `STAGING_DEPLOY_TARGETS` | See JSON below | JSON array of ECS targets for **staging** (used on `develop` and when manual run chooses staging). |
| `PROD_DEPLOY_TARGETS` | See JSON below | JSON array of ECS targets for **production** (used on `main` and when manual run chooses production). |

---

## Deploy targets JSON format

One object per region. Each object must have: `region`, `cluster`, `api_service`, `consumer_service`, `worker_service`.

**Single region (staging):**

```json
[
  {
    "region": "ap-south-1",
    "cluster": "my-staging-cluster",
    "api_service": "my-api",
    "consumer_service": "my-consumer",
    "worker_service": "my-worker"
  }
]
```

**Single region (production):**

```json
[
  {
    "region": "ap-south-1",
    "cluster": "my-prod-cluster",
    "api_service": "my-api",
    "consumer_service": "my-consumer",
    "worker_service": "my-worker"
  }
]
```

**Multiple regions:** add more objects to the array:

```json
[
  {
    "region": "ap-south-1",
    "cluster": "prod-cluster-1",
    "api_service": "api",
    "consumer_service": "consumer",
    "worker_service": "worker"
  },
  {
    "region": "us-west-2",
    "cluster": "prod-cluster-2",
    "api_service": "api",
    "consumer_service": "consumer",
    "worker_service": "worker"
  }
]
```

Paste the full JSON (no comments, valid JSON) into the Variable value for `STAGING_DEPLOY_TARGETS` and `PROD_DEPLOY_TARGETS`.

---

## Checklist

- [ ] **Secrets:** `AWS_ACCOUNT_ID`, `AWS_REGION`
- [ ] **Variables:** `ECR_REGISTRY`, `ECR_REPOSITORY`
- [ ] **Variables:** `STAGING_DEPLOY_TARGETS`, `PROD_DEPLOY_TARGETS` (valid JSON)
- [ ] **AWS:** IAM role `github-cicd` with OIDC trust for your repo; permissions for ECR push and ECS `DescribeServices`, `DescribeTaskDefinition`, `RegisterTaskDefinition`, `UpdateService`
- [ ] **Runners:** Workflow uses `runs-on: self-hosted`; ensure a self-hosted runner is registered
