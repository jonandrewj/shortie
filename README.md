## Shortie
An API for creating, using, and deleting short urls.

### Implementation Assumptions
1. Use the URL shortener just for the host and path, that way the short url can be used with parameters without needing a new entry.
This design decision could be changed fairly easily or a parameter could allow both options.
2. The statistics are collected on a best-effort bases - as in, they might not be fully accurate under high load or errors.
Implementations that require full accuracy in a distributed system should be built with an event based system that can be idempotent.
3. URL shorteners are for GET requests? I'm not super sure about this, but I suppose I could support other HTTP verbs :shrug:.

### Testing
run `go test ./...`

### Run Locally
run `go run .`

### Run Locally with Persistent Backend
In two terminals run:
1. `docker run --rm -it -p 4566:4566 localstack/localstack:0.14.2`
2. `AWS_REGION=us-west-2 AWS_ACCESS_KEY_ID=dev AWS_SECRET_ACCESS_KEY=dev AWS_CUSTOM_DYNAMO_ENDPOINT=http://127.0.0.1:4566 go run .`

### Building Locally
run `go build .`

### Building with Docker
1. run `docker build -t shortie:latest .`
2. run `docker run -p 8421:8421 shortie:latest`
