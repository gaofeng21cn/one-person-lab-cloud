module opl-cloud/services/fabric

go 1.26.0

require (
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs v1.3.179
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common v1.3.217
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm v1.3.213
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke v1.3.212
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc v1.3.213
	golang.org/x/sys v0.50.0
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	k8s.io/apimachinery v0.37.2
	k8s.io/cri-api v0.37.2
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/internal/ownerservice v0.0.0
	opl-cloud/services/internal/ownerstore v0.0.0
	opl-cloud/services/internal/postgresmigrate v0.0.0
	opl-cloud/services/internal/protectedresource v0.0.0
)

require (
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.2 // indirect
)

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate

replace opl-cloud/services/internal/protectedresource => ../internal/protectedresource

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/internal/ownerservice => ../internal/ownerservice

replace opl-cloud/services/internal/ownerstore => ../internal/ownerstore

require (
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/google/uuid v1.6.0
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lib/pq v1.12.3
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
)

require opl-cloud/services/internal/tkeapply v0.0.0

replace opl-cloud/services/internal/tkeapply => ../internal/tkeapply
