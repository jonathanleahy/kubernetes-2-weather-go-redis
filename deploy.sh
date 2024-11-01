#!/bin/bash

# Set your Docker Hub username
DOCKER_USERNAME="jonathanleahy"
APP_NAME="weather"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print status messages
print_status() {
    echo -e "${GREEN}>>> $1${NC}"
}

# Function to print error messages
print_error() {
    echo -e "${RED}ERROR: $1${NC}"
    exit 1
}

# Function to print debug messages
print_debug() {
    echo -e "${YELLOW}DEBUG: $1${NC}"
}

# Function to read value from .env file
get_env_value() {
    local key=$1
    local value=$(grep "^${key}=" .env | cut -d '=' -f2)
    echo "$value"
}

# Function to clean up Kubernetes resources
cleanup_kubernetes() {
    print_status "Cleaning up Kubernetes resources..."

    print_debug "Current resources before cleanup:"
    kubectl get all -l app=weather
    kubectl get all -l app=redis

    # Delete deployments
    kubectl delete deployment weather-dashboard --ignore-not-found
    kubectl delete deployment redis-cache --ignore-not-found

    # Delete services
    kubectl delete service weather-service --ignore-not-found
    kubectl delete service redis-service --ignore-not-found

    # Delete secrets
    kubectl delete secret weather-secrets --ignore-not-found

    # Delete any stuck pods
    kubectl delete pods -l app=weather --force --ignore-not-found
    kubectl delete pods -l app=redis --force --ignore-not-found

    # Wait for everything to be deleted
    print_status "Waiting for resources to be cleaned up..."
    kubectl wait --for=delete pod -l app=weather --timeout=60s 2>/dev/null || true
    kubectl wait --for=delete pod -l app=redis --timeout=60s 2>/dev/null || true

    print_debug "Resources after cleanup:"
    kubectl get all -l app=weather
    kubectl get all -l app=redis

    print_status "Cleanup completed"
}

# Function to clean up local resources
cleanup_local() {
    print_status "Cleaning up local resources..."
    rm -f main secrets.yaml
    print_status "Local cleanup completed"
}

# Function to wait for service IP
wait_for_service_ip() {
    print_status "Waiting for service IP..."
    local attempts=0
    local max_attempts=30
    local service_ip=""

    while [ $attempts -lt $max_attempts ]; do
        if command -v minikube &> /dev/null; then
            service_ip=$(minikube service weather-service --url)
            if [ ! -z "$service_ip" ]; then
                echo $service_ip
                return 0
            fi
        else
            service_ip=$(kubectl get service weather-service -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
            if [ ! -z "$service_ip" ]; then
                echo $service_ip
                return 0
            fi
        fi

        attempts=$((attempts + 1))
        sleep 2
    done

    return 1
}

# Function to test service
test_service() {
    local service_url=$1
    print_status "Testing service at $service_url"

    # Test health endpoint
    print_status "Testing health endpoint..."
    curl -s "${service_url}/api/health"
    echo ""

    # Test weather endpoint
    print_status "Testing weather endpoint..."
    curl -s "${service_url}/api/weather/london"
    echo ""
}

# Check if .env file exists
if [ ! -f .env ]; then
    print_error ".env file not found"
fi

# Get API key from .env
API_KEY=$(get_env_value "WEATHER_API_KEY")

if [ -z "$API_KEY" ]; then
    print_error "WEATHER_API_KEY not found in .env file"
fi

# Check if Docker Hub username is set
if [ "$DOCKER_USERNAME" = "YOUR_DOCKERHUB_USERNAME" ]; then
    print_error "Please set your Docker Hub username in the script"
fi

# Perform cleanup
cleanup_local
cleanup_kubernetes

# Create Kubernetes secrets yaml
print_status "Creating Kubernetes secrets..."
ENCODED_API_KEY=$(echo -n "$API_KEY" | base64)

# Create a separate secrets file
cat << EOF > secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: weather-secrets
type: Opaque
data:
  api-key: $(echo -n "$API_KEY" | base64)
EOF

# Build Go application
print_status "Building Go application..."
go build -o main . || print_error "Go build failed"

# Build Docker image
print_status "Building Docker image..."
docker build -t $APP_NAME:latest . || print_error "Docker build failed"

# Tag Docker image
print_status "Tagging Docker image..."
docker tag $APP_NAME:latest $DOCKER_USERNAME/$APP_NAME:latest || print_error "Docker tag failed"

# Push to Docker Hub
print_status "Pushing to Docker Hub..."
docker push $DOCKER_USERNAME/$APP_NAME:latest || print_error "Docker push failed"

# Apply Kubernetes configurations
print_status "Applying Kubernetes configurations..."

# Apply secrets first
kubectl apply -f secrets.yaml || print_error "Failed to apply secrets"

# Remove secrets.yaml for security
rm secrets.yaml

# Apply deployment and services
kubectl apply -f deployment.yaml || print_error "Failed to apply deployment"

# Wait for deployment to be ready
print_status "Waiting for deployment to be ready..."
kubectl rollout status deployment/weather-dashboard --timeout=300s || {
    print_debug "Deployment failed to roll out. Checking status:"
    kubectl get pods -l app=weather
    kubectl describe pods -l app=weather
    print_error "Deployment failed"
}

print_status "Getting service URL..."
SERVICE_URL=$(wait_for_service_ip)

if [ $? -eq 0 ] && [ ! -z "$SERVICE_URL" ]; then
    print_status "Service is available at: ${SERVICE_URL}"

    # Wait a few seconds for the service to be fully ready
    sleep 5

    # Test the service
    test_service "${SERVICE_URL}"

    echo ""
    print_status "Deployment Summary:"
    echo "- Service URL: ${SERVICE_URL}"
    echo "- Weather endpoint: ${SERVICE_URL}/api/weather/{city}"
    echo "- Health endpoint: ${SERVICE_URL}/api/health"
    echo "- Cache stats: ${SERVICE_URL}/api/cache/stats"

    # Display pod status
    echo ""
    print_status "Pod Status:"
    kubectl get pods -l app=weather
    kubectl get pods -l app=redis

    echo ""
    print_status "You can monitor pods with: kubectl get pods --watch"
    print_status "Access the application using: ${SERVICE_URL}"
else
    print_error "Failed to get service URL after waiting"
fi