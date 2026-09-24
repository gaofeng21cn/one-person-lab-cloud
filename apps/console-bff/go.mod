module opl-cloud/apps/console-bff

go 1.25.0

require (
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
	opl-cloud/packages/contracts/go v0.0.0
)

require (
	github.com/dlclark/regexp2 v1.11.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go
