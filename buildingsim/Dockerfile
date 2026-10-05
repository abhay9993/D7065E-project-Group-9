FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go generate ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/buildsim ./cmd

FROM scratch
COPY --from=build /out/buildsim /buildsim
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/buildsim"]
CMD ["start", "--host", "0.0.0.0", "--port", "9090"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/buildsim", "health", "--url", "http://127.0.0.1:9090/healthz"]
