FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY recall-linux /app/recall
COPY templates/ /app/templates/
COPY static/ /app/static/
COPY migrations/ /app/migrations/

EXPOSE 8080

VOLUME ["/app/data"]

ENV RECALL_DB_PATH=/app/data/recall.db
ENV RECALL_PORT=8080

ENTRYPOINT ["/app/recall"]
