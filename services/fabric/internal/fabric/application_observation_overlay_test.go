package fabric

import (
 "context"
 "encoding/json"
 "reflect"
 "testing"
 contracts "opl-cloud/packages/contracts/go"
)

func TestOverlayApplicationRuntimeDurableOwnerMiss(t *testing.T) {
 ctx := context.Background()
 store := NewMemoryOperationStore()
 creator := runtimeTestService(&recordingApplicationRuntimeProvider{}, store)
 revision := applicationRevisionForTest()
 revision.Dependencies = nil // one all-in-one component, not a legacy Launch runtime
 input := applicationRuntimeInput("app-runtime-owner-repro", revision)
 observation, err := creator.CreateWorkspaceApplicationRuntime(ctx, input)
 if err != nil { t.Fatal(err) }
 rows, err := store.List(ctx)
 if err != nil || len(rows) != 1 { t.Fatalf("durable records=%d err=%v",len(rows),err) }
 op := rows[0]
 var retained workspaceApplicationRuntimeRecord
 if op.ResourceKind != "workspace_application_runtime" || op.Action != "create_workspace_application_runtime" || op.Status != string(contracts.StatusSucceeded) || !decodeOperationResource(op,&retained) || retained.RuntimeID != observation.RuntimeID || retained.Input.AccountID != input.AccountID || retained.Input.WorkspaceID != input.WorkspaceID { t.Fatalf("real create did not retain owner: %#v", op) }
 before := mustJSON(rows)
 var manifest struct { Items []map[string]any `json:"items"` }
 if err := json.Unmarshal(workspaceApplicationManifest(input, creator.computes[input.ComputeID], creator.volumes[input.VolumeID]), &manifest); err != nil { t.Fatal(err) }
 var deployment map[string]any
 for _, item := range manifest.Items { if item["kind"] == "Deployment" { deployment = item } }
 if deployment == nil { t.Fatal("real application manifest missing deployment") }
 meta := deployment["metadata"].(map[string]any)
 meta["uid"], meta["generation"] = "app-deployment-uid", float64(1)
 deployment["status"] = map[string]any{"observedGeneration":float64(1),"updatedReplicas":float64(1),"readyReplicas":float64(1),"availableReplicas":float64(1)}
 template := deployment["spec"].(map[string]any)["template"]
 rs := map[string]any{"kind":"ReplicaSet","metadata":map[string]any{"name":"app-rs","uid":"app-rs-uid","ownerReferences":[]any{map[string]any{"kind":"Deployment","uid":meta["uid"],"controller":true}}},"spec":map[string]any{"replicas":float64(1),"template":template}}
 pod := map[string]any{"kind":"Pod","metadata":map[string]any{"name":"app-pod","uid":"app-pod-uid","labels":meta["labels"],"ownerReferences":[]any{map[string]any{"kind":"ReplicaSet","uid":"app-rs-uid","controller":true}}},"status":map[string]any{"phase":"Running","conditions":[]any{map[string]any{"type":"Ready","status":"True"}}}}
 reader := NewServiceWithOperationStore(inventoryProvider(t,[]any{deployment,rs,pod}),store)
 found, err := store.WorkspaceRuntimeIdentityCandidates(ctx,input.WorkspaceID)
 if err != nil { t.Fatal(err) }
 results,err := reader.RuntimeObservations(ctx)
 if err != nil || len(results.Items)!=1 { t.Fatalf("result=%#v err=%v",results,err) }
 live:=results.Items[0]
 if live.RuntimeID != retained.RuntimeID || live.WorkspaceID != retained.WorkspaceID || live.AccountID != input.AccountID || live.ObservedState != contracts.ResourceObservedRunning { t.Fatalf("discovery mismatched real manifest: %#v",live) }
 after,_ := store.List(ctx)
 if !reflect.DeepEqual(before,mustJSON(after)) { t.Fatal("read mutated journal") }
 t.Logf("real create: durable=%s status=%s runtime=%s; candidates=%d; actual discovery=%s ownership=%s",op.ResourceKind,op.Status,retained.RuntimeID,len(found),live.ObservedState,live.Ownership)
 if live.Ownership != contracts.RuntimeOwnershipVerified { t.Fatalf("BUG: durable application owner exists but discovery reports %s",live.Ownership) }
}
