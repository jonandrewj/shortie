FROM golang:1.21-alpine3.18 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o shortie

FROM alpine:3.18

WORKDIR /app

COPY --from=builder /app/shortie .

EXPOSE 8421

CMD ["./shortie"]