


build:
	@echo "Building kvm-agent..."
	GOOS=windows GOARCH=amd64 go build -o agent.exe cmd/kvm-agent/main.go
	GOOS=linux GOARCH=amd64 go build -o agent cmd/kvm-agent/main.go


.clean:
	rm -rf *.exe
	rm -rf agent




