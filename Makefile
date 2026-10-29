IMAGE := structmap:local

.PHONY: container up fmt test bench cover lint

container:
	@docker build -t $(IMAGE) .

up:
	@docker run -v ${PWD}:/go/src/devel --rm -it $(IMAGE) bash

fmt:
	@golangci-lint fmt ./...

test:
	@go test -race -count=1 ./...

bench:
	@go test -run '^$$' -bench . -benchmem ./...

cover:
	@go test -coverprofile=cover.out ./... && go tool cover -func=cover.out

lint:
	@golangci-lint run ./...
