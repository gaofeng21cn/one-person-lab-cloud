module opl-cloud/services/workspace

go 1.25.0

require opl-cloud/services/internal/ownerservice v0.0.0

require (
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	opl-cloud/packages/contracts/go v0.0.0 // indirect
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/internal/ownerservice => ../internal/ownerservice

replace opl-cloud/services/internal/ownerstore => ../internal/ownerstore

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate
