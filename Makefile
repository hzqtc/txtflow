BINARY_NAME = txtflow
SRC = main.go
GOBIN = $(HOME)/.local/bin

all: build

$(BINARY_NAME): $(SRC)
	go mod tidy
	go build -o $(BINARY_NAME) $(SRC)

build: $(BINARY_NAME)

run: build
	./$(BINARY_NAME)

fmt:
	gofmt -w -s $(SRC)

clean:
	rm -f $(BINARY_NAME)

install: build
	GOBIN=$(GOBIN) go install

.PHONY: all build run clean install
