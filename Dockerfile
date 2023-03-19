# Start from the latest golang base image
FROM golang:1.19-alpine as builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy everything from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
RUN --mount=type=cache,target=/go/pkg/mod go build -o main .

FROM alpine
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser
WORKDIR /app/

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/main .

# Command to run the executable
ENTRYPOINT ["./main"]
