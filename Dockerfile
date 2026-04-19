FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /recall ./cmd/recall

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /recall /app/recall
COPY templates/ /app/templates/
COPY static/ /app/static/
COPY migrations/ /app/migrations/

EXPOSE 8080

VOLUME ["/app/data"]

ENV RECALL_DB_PATH=/app/data/recall.db
ENV RECALL_PORT=8080

ENTRYPOINT ["/app/recall"]
