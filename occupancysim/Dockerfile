# Stage 1: build the Svelte UI
FROM node:22-alpine AS ui
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm ci 2>/dev/null || npm install
COPY ui/ ./
RUN npm run build

# Stage 2: build the Go binary with the UI embedded
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
COPY --from=ui /ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/occupancy .

# Stage 3: minimal runtime image
FROM scratch
COPY --from=build /out/occupancy /occupancy
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/occupancy"]
