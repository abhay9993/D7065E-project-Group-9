FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sensor .

FROM alpine:3.20
RUN adduser -D -u 65532 appuser
COPY --from=build /out/sensor /app
USER appuser
# One image, four containers: SENSOR_TYPE and LISTEN_ADDR are set per container in docker-compose.yaml.
ENV LISTEN_ADDR=:8095
ENTRYPOINT ["/app"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O- "http://127.0.0.1${LISTEN_ADDR}/healthz" || exit 1
