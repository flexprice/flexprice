#!/bin/bash
set -e

# Load environment variables from .env file
if [ -f .env ]; then
  # More robust way to load env vars, handling comments properly
  while IFS= read -r line || [[ -n "$line" ]]; do
    # Skip empty lines and comments
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    # Export valid environment variables
    if [[ "$line" =~ ^[[:alpha:]][[:alnum:]_]*= ]]; then
      export "$line"
    fi
  done < .env
else
  echo "Error: .env file not found"
  exit 1
fi

# Check if required variables exist
if [[ -z "$ECR_REGISTRY" || -z "$ECR_REPOSITORY" ]]; then
  echo "Error: ECR_REGISTRY or ECR_REPOSITORY not found in .env file"
  exit 1
fi

# Check if AWS CLI is installed
if ! command -v aws &> /dev/null; then
  echo "Error: AWS CLI is not installed"
  exit 1
fi

# Login to Amazon ECR
echo "Logging in to Amazon ECR..."
aws ecr get-login-password --region ap-south-1 | docker login --username AWS --password-stdin "$ECR_REGISTRY"

# Get commit hash
COMMIT_HASH=$(git rev-parse --short HEAD)

# Build full image URI
IMAGE_URI="${ECR_REGISTRY}/${ECR_REPOSITORY}:${COMMIT_HASH}"

echo "Building image: ${IMAGE_URI}"

# Set up Docker buildx if not already set up
if ! docker buildx inspect builder &> /dev/null; then
  docker buildx create --name builder --use
fi

# Build and push with specific platform
echo "Building Docker image..."
docker buildx build \
  --platform linux/arm64 \
  --load \
  -t "${IMAGE_URI}" \
  -f Dockerfile.ecs .

# Push the image
echo "Pushing image to ECR: ${IMAGE_URI}"
docker push "${IMAGE_URI}"

echo "Image successfully built and pushed to ECR: ${IMAGE_URI}" 