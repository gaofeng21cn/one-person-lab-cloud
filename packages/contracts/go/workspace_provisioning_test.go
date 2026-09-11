package contracts

import (
	"reflect"
	"testing"
)

func TestWorkspaceProvisioningStagesKeepLegacyFullLaunch(t *testing.T) {
	stages, err := WorkspaceProvisioningStages(WorkspaceProvisioningFull)
	if err != nil || !reflect.DeepEqual(stages, AllLaunchStages()) {
		t.Fatalf("full mode must retain legacy stages: %v %#v", err, stages)
	}
}

func TestWorkspaceProvisioningStagesResourceOnlyHasNoApplicationStages(t *testing.T) {
	stages, err := WorkspaceProvisioningStages(WorkspaceProvisioningResourceOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range stages {
		if stage == StageKey || stage == StageSecret || stage == StageRuntime {
			t.Fatalf("resource-only mode included application stage %q", stage)
		}
	}
	want := []Stage{StageDebit, StageCompute, StageStorage, StageAttachment, StageActivation, StageReceipt, StageSucceeded}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("unexpected resource-only stage plan: %#v", stages)
	}
}
