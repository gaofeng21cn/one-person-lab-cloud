module opl-cloud/services/ledger

go 1.26.0

require (
	github.com/lib/pq v1.12.3
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/internal/ownerservice v0.0.0-00010101000000-000000000000
	opl-cloud/services/internal/postgresmigrate v0.0.0
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate

require (
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	opl-cloud/services/internal/ownerstore v0.0.0-00010101000000-000000000000 // indirect
)

replace opl-cloud/services/internal/ownerservice => ../internal/ownerservice

replace opl-cloud/services/internal/ownerstore => ../internal/ownerstore
