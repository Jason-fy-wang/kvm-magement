

test:
	@echo "Running tests..."
	go test -v ./...

build:
	@echo "Building kvm-manager..."
	GOOS=windows GOARCH=amd64 go build -o server.exe cmd/kvm-manager/main.go
	GOOS=linux GOARCH=amd64 go build -o server cmd/kvm-manager/main.go


.clean:
	rm -rf *.exe
	rm -rf server




