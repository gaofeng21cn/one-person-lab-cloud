module opl-cloud/apps/console-bff

go 1.25.0

require (
	github.com/modelcontextprotocol/go-sdk v1.4.1
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
	opl-cloud/packages/contracts/go v0.0.0
)

require (
	github.com/dlclark/regexp2 v1.11.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/gateway-integration => ../../services/gateway-integration

replace opl-cloud/services/internal/ownerservice => ../../services/internal/ownerservice

replace opl-cloud/services/internal/ownerstore => ../../services/internal/ownerstore

replace opl-cloud/services/internal/postgresmigrate => ../../services/internal/postgresmigrate
