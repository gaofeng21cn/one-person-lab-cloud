package main
import (
 "bytes"
 "encoding/json"
 "fmt"
 "os"
 c "opl-cloud/packages/contracts/go"
)
func main(){
 b,e:=os.ReadFile("/Users/huangrende/Desktop/应聘/chatgpt-share-6ab13ad3/opl_v226_execution_spec/contracts/publisher-contract.schema.json");if e!=nil{panic(e)}
 var doc struct {Examples []json.RawMessage `json:"examples"`};if e=json.Unmarshal(b,&doc);e!=nil{panic(e)}
 n:=0
 for _,example:=range doc.Examples {
  var head struct{Kind string `json:"kind"`; Revision json.RawMessage `json:"applicationRevisionTemplate"`};if e=json.Unmarshal(example,&head);e!=nil{panic(e)};if head.Kind!="runtime"{continue}
  var r c.WorkspaceApplicationRevision;decoder:=json.NewDecoder(bytes.NewReader(head.Revision));decoder.DisallowUnknownFields();if e=decoder.Decode(&r);e!=nil{panic(e)}
  if e=c.ValidateWorkspaceApplicationRevision(r);e!=nil{panic(fmt.Sprintf("%s canonical validation: %v",r.ApplicationID,e))}
  if _,e=c.WorkspaceApplicationStartupOrder(r);e!=nil{panic(e)}
  fmt.Println("PASS canonical Go validator:",r.ApplicationID,r.Platform,r.EntryPort);n++
 }
 if n!=2{panic("expected official and third-party runtime examples")}
}
