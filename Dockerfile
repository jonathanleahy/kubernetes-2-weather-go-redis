FROM golang:1.23-alpine

WORKDIR /app

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o weather-dashboard

# Run the application
EXPOSE 8080
CMD ["./weather-dashboard"]
