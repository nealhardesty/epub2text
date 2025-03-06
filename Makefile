GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod
GOTEST=$(GOCMD) test

build:
	$(GOBUILD) .

run:
	go run .

clean:
	$(GOCLEAN)

mod:
	$(GOMOD) tidy
	$(GOMOD) vendor

test:
	$(GOTEST) -v ./...

.PHONY: build run clean mod test
