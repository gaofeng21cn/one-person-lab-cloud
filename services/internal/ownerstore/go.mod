module opl-cloud/services/internal/ownerstore

go 1.25.0

require (
	github.com/lib/pq v1.12.3
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/internal/postgresmigrate v0.0.0
)

replace opl-cloud/packages/contracts/go => ../../../packages/contracts/go
replace opl-cloud/services/internal/postgresmigrate => ../postgresmigrate
