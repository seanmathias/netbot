BINARY  := netbot
GOCMD   := go
GOBUILD := $(GOCMD) build
GOTEST  := $(GOCMD) test
GOVET   := $(GOCMD) vet

.PHONY: all build test vet clean

all: test build

build: vet test
	$(GOBUILD) -o $(BINARY) .

test:
	$(GOTEST) ./...

vet:
	$(GOVET) ./...

clean:
	rm -f $(BINARY)