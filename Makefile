BIN := $(HOME)/bin/plannotator-review-gate

.PHONY: build vet test install

build:
	go build -o plannotator-review-gate .

vet:
	go vet ./...

test:
	go test -race ./...

# install refuses to ship a binary that doesn't vet and pass its tests.
install: vet test
	mkdir -p $(HOME)/bin
	go build -o $(BIN) .
	@echo "installed $(BIN)"
	@echo "next: $(BIN) hook-config"
