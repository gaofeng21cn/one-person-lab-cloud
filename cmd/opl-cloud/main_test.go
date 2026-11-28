package main

import (
	"path/filepath"
	"testing"

	"opl-cloud/services/cloud-composition/composition"
)

func TestAllContractOwnersAreRegisteredInSingleBinary(t *testing.T) {
	topology, err := composition.Load("../../deploy/portable/opl-cloud-owner-topology.json")
	if err != nil {
		t.Fatalf("Load topology: %v", err)
	}
	if len(allModules) != len(topology.Processes) {
		t.Fatalf("allModules has %d entries, contract declares %d processes", len(allModules), len(topology.Processes))
	}
	if len(binaryDispatch) != len(topology.Processes) {
		t.Fatalf("binaryDispatch has %d entries, contract declares %d processes", len(binaryDispatch), len(topology.Processes))
	}
	for _, process := range topology.Processes {
		if allModules[process.Service] == nil {
			t.Fatalf("missing allModules entry for declared service %q", process.Service)
		}
		binaryName := filepath.Base(process.Binary)
		if binaryDispatch[binaryName] == nil {
			t.Fatalf("missing binaryDispatch entry for declared binary %q (%s)", binaryName, process.Service)
		}
	}
}
