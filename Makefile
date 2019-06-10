gen-proto: install-go-proto-plugin
	protoc --go_out=plugins=grpc:./protos/gen/ ./protos/rpc.proto

install-go-proto-plugin:
	go get -u github.com/golang/protobuf/protoc-gen-go