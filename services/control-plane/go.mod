module opl-cloud/services/control-plane

go 1.26.0

require (
	github.com/lib/pq v1.12.3
	github.com/mattn/go-sqlite3 v1.14.53
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/internal/postgresmigrate v0.0.0
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate

require (
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
)

require golang.org/x/text v0.42.0 // indirect
