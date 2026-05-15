# AWS IAM Policies for FlexPrice on EKS

Per-workload IAM roles via [IRSA](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
(IAM Roles for Service Accounts) are the AWS-recommended pattern. The chart
supports it via `serviceAccount.perComponent=true` + per-component
annotations.

## Prerequisites

1. EKS cluster with an OIDC provider associated.
2. Three IAM roles (api, consumer, worker) with trust policies that allow
   the cluster's OIDC provider to assume them for the chart's
   ServiceAccounts.

```bash
# Get the OIDC provider URL for the cluster:
aws eks describe-cluster --name flexprice-prod --query 'cluster.identity.oidc.issuer' --output text
# → https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B71EXAMPLE
```

## Trust policy (one per role)

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B71EXAMPLE"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B71EXAMPLE:sub": "system:serviceaccount:flexprice:flexprice-api",
        "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B71EXAMPLE:aud": "sts.amazonaws.com"
      }
    }
  }]
}
```

Repeat with `:sub` updated to `flexprice-consumer` and `flexprice-worker` for
the other two roles.

## Permission policies

### `flexprice-api`

The api needs:
- S3 bucket for invoice PDF storage (if `s3.enabled=true`).
- KMS decrypt for `secrets-manager` if you use IAM-auth Secrets Manager
  (otherwise the ESO controller's role handles that).
- SES `SendEmail` if `email.enabled=true` (you use SES instead of Resend).

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "InvoiceBucket",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::flexprice-invoices",
        "arn:aws:s3:::flexprice-invoices/*"
      ]
    },
    {
      "Sid": "InvoiceBucketKMS",
      "Effect": "Allow",
      "Action": ["kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey"],
      "Resource": "arn:aws:kms:us-east-1:123456789012:key/flexprice-invoice-kms"
    },
    {
      "Sid": "SES",
      "Effect": "Allow",
      "Action": ["ses:SendEmail", "ses:SendRawEmail"],
      "Resource": "*",
      "Condition": {
        "StringEquals": { "ses:FromAddress": "no-reply@flexprice.yourcompany.com" }
      }
    }
  ]
}
```

### `flexprice-consumer`

The consumer needs:
- MSK access if you self-host Kafka (cluster + topic-level for the consumer
  group).
- The S3 + KMS perms above if it also writes raw events to S3 export.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "MSKClusterConnect",
      "Effect": "Allow",
      "Action": ["kafka-cluster:Connect"],
      "Resource": "arn:aws:kafka:us-east-1:123456789012:cluster/flexprice-prod/*"
    },
    {
      "Sid": "MSKTopicAccess",
      "Effect": "Allow",
      "Action": [
        "kafka-cluster:DescribeTopic",
        "kafka-cluster:ReadData",
        "kafka-cluster:DescribeGroup",
        "kafka-cluster:AlterGroup"
      ],
      "Resource": [
        "arn:aws:kafka:us-east-1:123456789012:topic/flexprice-prod/*/events",
        "arn:aws:kafka:us-east-1:123456789012:topic/flexprice-prod/*/events_lazy",
        "arn:aws:kafka:us-east-1:123456789012:topic/flexprice-prod/*/events_post_processing",
        "arn:aws:kafka:us-east-1:123456789012:group/flexprice-prod/*/flexprice-consumer*"
      ]
    },
    {
      "Sid": "S3RawEvents",
      "Effect": "Allow",
      "Action": ["s3:PutObject"],
      "Resource": "arn:aws:s3:::flexprice-raw-events/*"
    }
  ]
}
```

> **NOTE**: The MSK actions assume MSK IAM auth, which the **app does not
> currently support** (SCRAM only). Keep the MSK actions here for the day
> the app adds IAM auth, but for now the consumer doesn't actually use
> these permissions — SCRAM credentials come from
> `secrets.existingSecret`.

### `flexprice-worker`

The worker mainly needs to talk to Temporal Cloud (API-key auth, no AWS
IAM) and read/write data. If your workflows include AWS side-effects
(SQS publish, S3 file generation, etc.), add them here:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "InvoiceBucketWrite",
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject"],
      "Resource": "arn:aws:s3:::flexprice-invoices/*"
    },
    {
      "Sid": "SESForInvoiceEmail",
      "Effect": "Allow",
      "Action": ["ses:SendEmail", "ses:SendRawEmail"],
      "Resource": "*"
    }
  ]
}
```

If you don't run workflows that touch AWS, this role can be empty — but
keep it created so the ServiceAccount annotation points at *something*.

## Wiring into the chart

```yaml
serviceAccount:
  create: true
  perComponent: true
  components:
    api:
      annotations:
        eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-api
    consumer:
      annotations:
        eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-consumer
    worker:
      annotations:
        eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-worker
```

After `helm install`, verify each pod is using the right role:

```bash
NS=flexprice
for c in api consumer worker; do
  POD=$(kubectl get pod -n $NS -l app.kubernetes.io/component=$c -o name | head -1)
  echo "=== $c ==="
  kubectl exec -n $NS $POD -- env | grep AWS_ROLE_ARN
done
```

You should see three different role ARNs.

## Pod Identity (alternative, simpler)

EKS now supports
[EKS Pod Identity](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html),
which removes the OIDC trust policy step. Same `serviceAccount.perComponent`
flow; instead of annotating the SA, you create a Pod Identity Association
via the console / Terraform / CLI:

```bash
aws eks create-pod-identity-association \
  --cluster-name flexprice-prod \
  --namespace flexprice \
  --service-account flexprice-api \
  --role-arn arn:aws:iam::123456789012:role/flexprice-api
```

The `eks.amazonaws.com/role-arn` annotation isn't needed in this mode.

## Troubleshooting

```bash
# Look for the SA token volume + projected aud
kubectl describe pod -n flexprice $POD | grep -A5 'token-amazonaws'

# Did the pod assume the role?
kubectl logs -n flexprice $POD | grep -i 'iam\|credentials\|sts'

# Hit STS directly from the pod
kubectl exec -n flexprice $POD -- sh -c '
  aws sts get-caller-identity 2>&1 || \
  echo "no aws cli — install aws-cli in your image or curl the STS endpoint manually"
'
```

Common failure: trust policy `:sub` mismatch — the SA name in the policy
condition must exactly match `system:serviceaccount:<ns>:<sa-name>` where
`<sa-name>` is `<fullname>-api` / `-consumer` / `-worker` (with
`serviceAccount.perComponent=true`).
