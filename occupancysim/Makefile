GO ?= go
NPM ?= npm

.PHONY: all ui build run test docker-build clean

all: build

ui:
	cd ui && $(NPM) install --no-audit --no-fund && $(NPM) run build

build: ui
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/occupancy .

run: build
	./bin/occupancy

test:
	$(GO) test ./... -count=1

docker-build:
	docker build -t occupancy:local .

clean:
	rm -rf bin ui/dist/assets ui/dist/index.html
