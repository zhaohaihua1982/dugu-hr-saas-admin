OUTPUT := main
BINARY := dugu-hr-saas-admin

.PHONY: build run test fmt vet clean

build:
	@echo "build project to ./$(OUTPUT)"
	go build -o ./$(OUTPUT) ./cmd

run:
	@echo "start $(BINARY) ..."
	go build -o /tmp/$(BINARY) ./cmd && /tmp/$(BINARY)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -f $(OUTPUT)
