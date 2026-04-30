APP     := math_trainer
BINARY  := ./bin/$(APP)
MAIN    := ./cmd/server/main.go
PORT    := 8080

.PHONY: all build run dev clean setup config lan-url tidy help

all: build

## Build the server binary
build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -o $(BINARY) $(MAIN)

## Build and run
run: build
	$(BINARY)

## Run with go run (no binary, slower start)
go-run:
	CGO_ENABLED=1 go run $(MAIN)

## Run tests
test:
	CGO_ENABLED=1 go test ./...

## Delete database (data will be recreated on next start)
db-reset:
	@echo "Deleting math_trainer.db..."
	@rm -f math_trainer.db
	@echo "Done. Database will be recreated on next server start."

## Copy example config if config.yaml doesn't exist
config:
	@if [ ! -f config.yaml ]; then \
		cp config.example.yaml config.yaml; \
		echo "Created config.yaml — edit it to add your API keys."; \
	else \
		echo "config.yaml already exists."; \
	fi

## Print LAN URL for iPad/phone access
lan-url:
	@IP=$$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null); \
	if [ -n "$$IP" ]; then \
		echo "Open on iPad/phone: http://$$IP:$(PORT)"; \
	else \
		echo "Could not determine LAN IP"; \
	fi

## First-time setup
setup: config build
	@echo ""
	@echo "Setup complete!"
	@echo "1. Edit config.yaml and add your API keys"
	@echo "2. Run: make run"
	@echo ""
	@$(MAKE) lan-url

## Remove build artifacts
clean:
	rm -rf bin/

## Tidy go.mod
tidy:
	go mod tidy

## Build release binary (stripped)
release:
	@mkdir -p bin
	CGO_ENABLED=1 go build -ldflags="-s -w" -o $(BINARY) $(MAIN)

help:
	@grep -E '^##' Makefile | sed 's/## /  /'
