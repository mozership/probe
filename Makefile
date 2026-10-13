CERT_DIR := e2e/certs

default: build

build:
	env CGO_ENABLED=0 go build -ldflags="-s -w" -o probe ./cmd/probe/...

test:
	go test ./... -coverprofile=coverage.out -covermode=count

lint:
	golangci-lint run ./...

licenses:
	go run ./internal/thirdparty/gen -o THIRD_PARTY_LICENSES

key:
	@rm -rf keys/*.pem
	@mkdir keys
	@openssl req -x509 -days 10 -newkey ED25519 -nodes -out ./keys/cert.pem -keyout ./keys/key.pem -subj "/C=/ST=/L=/O=/OU=/CN=example.local" &>/dev/null

code:
	#@which buf || brew install bufbuild/buf/buf
	buf generate

http_server:
	go run github.com/mccutchen/go-httpbin/v2/cmd/go-httpbin@latest -host 127.0.0.1 -port 8080

http_server_tls:
	go run github.com/mccutchen/go-httpbin/v2/cmd/go-httpbin@latest -host 127.0.0.1 -port 8080 \
		-https-cert-file ./e2e/certs/server.crt \
		-https-key-file ./e2e/certs/server.key


grpc_server:
	go run actions/grpc/testserver/*.go

grpc_server_tls:
	go run actions/grpc/testserver/*.go \
		-tls \
		-cert="./e2e/certs/server.crt" \
		-key="./e2e/certs/server.key" \
		-port=50052

gen_server_keys:
	e2e/gen.sh

gen_grpc_server:
	@cd actions/grpc/testserver && \
		protoc --go_out=. --go-grpc_out=. ./pb/user_service.proto
