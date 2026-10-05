.PHONY: clean

fmt:
	@echo "formatting code..."
	go fmt ./...

permission:
	@echo "checking permission..."
	sudo setfacl -m u:$(USER):rw /dev/kvm
	getfacl /dev/kvm

docs:
	@echo "generate api docs..."
	swag init -g server.go -d ./internal/agent,./cmd/kvm-agent -o cmd/kvm-agent/docs --parseInternal

build: fmt  docs
	@echo "Building kvm-agent..."
	GOOS=windows GOARCH=amd64 go build -o agent.exe cmd/kvm-agent/main.go
	GOOS=linux GOARCH=amd64 go build -o agent cmd/kvm-agent/main.go

start:
	@echo "Starting kvm-agent..."
	./agent -firecracker /usr/local/bin/firecracker -log-level debug -listen :9090

clean:
	rm -rf *.exe
	rm -rf agent




