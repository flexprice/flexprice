# FlexPrice Setup Guide

## Prerequisites

- [Golang](https://go.dev/)
- [Docker](https://www.docker.com/) and [Docker Compose](https://docs.docker.com/compose/)
- Supported platforms:
  - Linux-based environment
  - macOS (Darwin)
  - WSL under Windows

## Quick Setup with Docker Compose

The easiest way to get started is using our Docker Compose setup:

```bash
# Clone the repository
git clone https://github.com/flexprice/flexprice
cd flexprice

# Set up the complete development environment
make dev-setup
```

This command will:

1. Start all required infrastructure (PostgreSQL, Kafka, ClickHouse, Temporal)
2. Build the FlexPrice application image
3. Run database migrations and initialize Kafka
4. Start all FlexPrice services (API, Consumer, Worker)

## Accessing Services

Once setup is complete, you can access:

- FlexPrice API: http://localhost:8080
- Temporal UI: http://localhost:8088
- Kafka UI: http://localhost:8084 (with profile 'dev')
- ClickHouse UI: http://localhost:8123

## Useful Commands

```bash
# Restart only the FlexPrice services
make restart-flexprice

# Stop all services
make down

# Clean everything and start fresh
make clean-start

# Build the FlexPrice image separately
make build-image

# Start only the FlexPrice services
make start-flexprice
```

## Running Without Docker

If you prefer to run the application directly:

```bash
# Start the required infrastructure
docker compose up -d postgres kafka clickhouse temporal temporal-ui

# Run the application locally
go run cmd/server/main.go
```

## Development Credentials

### PostgreSQL

- Host: localhost
- Port: 5432
- Database: flexprice
- Username: flexprice
- Password: flexprice123

### ClickHouse

- Host: localhost
- Port: 9000
- Database: flexprice
- Username: flexprice
- Password: flexprice123

### Kafka

- Bootstrap Server: localhost:29092
- UI: http://localhost:8084 (with profile 'dev')

## API Documentation

The API documentation is available in OpenAPI 3.0 format at `docs/swagger/swagger-3-0.json`.

### Setting up Postman Collection

1. Open Postman
2. Click on "Import" in the top left
3. Select "Import File"
4. Choose `docs/swagger/swagger-3-0.json`
5. Click "Import"
6. Create a new environment for local development:

   - Name: Local
   - Variable: `baseUrl`
   - Initial Value: `http://localhost:8080/v1`
   - Current Value: `http://localhost:8080/v1`

   - Variable: `apiKey`
   - Initial Value: `0cc505d7b917e0b1f25ccbea029dd43f4002edfea46b7f941f281911246768fe`
   - Current Value: `0cc505d7b917e0b1f25ccbea029dd43f4002edfea46b7f941f281911246768fe`

## Troubleshooting

### Docker Issues

1. Ensure Docker is running properly:

```bash
docker info
```

2. Check the status of all containers:

```bash
docker compose ps
```

3. View logs for a specific service:

```bash
docker compose logs [service_name]
```

### Database Connection Issues

1. Check database logs:

```bash
docker compose logs postgres
docker compose logs clickhouse
```

2. Verify the database is running:

```bash
docker compose ps postgres
docker compose ps clickhouse
```

### Kafka Issues

1. Verify Kafka is running:

```bash
docker compose logs kafka
```

2. Check topic list:

```bash
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --list
```

3. View Kafka UI at http://localhost:8084

## Additional Resources

- [Contribution Guidelines](https://github.com/flexprice/flexprice/blob/main/CONTRIBUTING.md)
- [API Documentation](https://docs.flexprice.io/)
- [Code of Conduct](https://github.com/flexprice/flexprice/blob/main/CODE_OF_CONDUCT.md)
- [FlexPrice Website](https://flexprice.io)

## Optional S3 Setup

If you wish to enable S3 storage for your FlexPrice API, follow these steps to configure your AWS S3 access:

### 1. Create an S3 Bucket

1. Log in to your AWS Management Console.
2. Navigate to the S3 service.
3. Create a new bucket named `<YOUR BUCKET NAME>` (or use an existing bucket).
4. Ensure that the bucket is in the desired region.
5. It is highly recommended to disable public access to the bucket, the API generates presigned URLs in the response which can be used to download the invoice.

### 2. Configure Bucket Policy

To allow FlexPrice to access the S3 bucket, you need to set up a bucket policy. Use the following policy, remembering to replace the bucket name with the name of your bucket.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "VisualEditor0",
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::<YOUR BUCKET NAME>/*"
    },
    {
      "Sid": "VisualEditor1",
      "Effect": "Allow",
      "Action": "s3:ListBucket",
      "Resource": "arn:aws:s3:::<YOUR BUCKET NAME>"
    }
  ]
}
```

### 3. Set Up AWS Credentials

Ensure that your AWS credentials are configured on the server where the FlexPrice API will run. You can set up your credentials in the `~/.aws/credentials` file as follows:

```ini
[default]
aws_access_key_id = YOUR_ACCESS_KEY_ID
aws_secret_access_key = YOUR_SECRET_ACCESS_KEY
```

If using a different profile, you can set the profile name in the `~/.aws/config` file:

```ini
[<YOUR PROFILE NAME>]
aws_access_key_id = YOUR_ACCESS_KEY_ID
aws_secret_access_key = YOUR_SECRET_ACCESS_KEY
```

Do remember to set the `AWS_PROFILE` environment variable to the profile name you just set up.

### 4. Configure the S3 Settings

Edit the `internal/config/config.yaml` file and set the `s3` section to the following:

```yaml
s3:
  enabled: false
  region: <YOUR BUCKET REGION>
  invoice:
    bucket: <YOUR BUCKET NAME>
    presign_expiry_duration: "1h" # The duration for which the presigned URL is valid (https://pkg.go.dev/time#ParseDuration)
    key_prefix: ""
    # The prefix for the invoice key in the bucket (defaults to empty and the invoice is stored under the tenant id folder - eg. {tenant_id}/{invoice_id}.pdf)
```

### 5. Running the Application

Once you have configured the S3 bucket and set the necessary environment variables, you can start your FlexPrice API. The application will now be able to interact with the S3 bucket for storing and retrieving invoices.

### 6. Troubleshooting

If you encounter issues with S3 access, check the following:

- Ensure that the bucket policy is correctly applied to the bucket.
- Verify that your AWS credentials have the necessary permissions.
- Check the logs for any errors related to S3 operations.

By following these steps, you can successfully set up S3 as an optional storage solution for your self-hosted FlexPrice API.
