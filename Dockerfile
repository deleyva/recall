FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags '-extldflags "-static"' -o /recall ./cmd/recall/

FROM alpine:3.18

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
