package coordination_test

import (
	"context"
	"opl-cloud/services/fabric/coordination"
	"testing"
)

func TestProcessKeepsTargetSurfaceExplicit(t *testing.T) {
	bootstrap, err := coordination.Start(context.Background(), func(key string) string {
		if key == "DATABASE_URL" {
			return "legacy-database"
		}
		return ""
	})
	if err != nil || bootstrap != nil {
		t.Fatalf("legacy setting unexpectedly enables target=%v %v", bootstrap, err)
	}
	_, err = coordination.Start(context.Background(), func(key string) string {
		if key == "OPL_FABRIC_DATABASE_URL" {
			return "explicit-target-database"
		}
		return ""
	})
	if err == nil || err.Error() != "OPL_RESOURCE_CATALOG_ADDR is required for Fabric resource acceptance" {
		t.Fatalf("Catalog dependency not required: %v", err)
	}
}
