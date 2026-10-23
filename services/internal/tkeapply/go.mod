module opl-cloud/services/internal/tkeapply

go 1.26.0

require (
	k8s.io/apimachinery v0.37.1
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/internal/protectedresource v0.0.0
)

require (
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
)

replace opl-cloud/packages/contracts/go => ../../../packages/contracts/go

replace opl-cloud/services/internal/protectedresource => ../protectedresource
