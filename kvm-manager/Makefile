
fmt:
	@echo "Formatting code..."
	go fmt ./...

test:
	@echo "Running tests..."
	go test -v ./...

build: fmt test
	@echo "Building kvm-manager..."
	GOOS=windows GOARCH=amd64 go build -o server.exe cmd/kvm-manager/main.go
	GOOS=linux GOARCH=amd64 go build -o server cmd/kvm-manager/main.go

start:
	./server -listen :8080 -db ./kvm-manager.db -network-cidr 172.20.0.0/16 -log-level debug
	
.clean:
	rm -rf *.exe
	rm -rf server




