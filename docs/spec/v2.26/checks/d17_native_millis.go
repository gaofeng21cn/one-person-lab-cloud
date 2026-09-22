package main
import("fmt";"time";"math/big")
func main(){
 raw:=[]string{"2026-09-01T00:00:00.999999999Z","2026-09-01T00:00:01.002000000Z","2026-09-01T00:00:01.001000000Z"};want:=[]int64{1788220800999,1788220801002,1788220801001};ms:=make([]int64,3)
 for i,v:=range raw{t,e:=time.Parse(time.RFC3339Nano,v);if e!=nil{panic(e)};ms[i]=t.UnixMilli();if ms[i]!=want[i]{panic("native canonical milliseconds mismatch")}}
 d:=big.NewInt(ms[1]-ms[0]);r:=big.NewInt(ms[1]-ms[2]);num:=new(big.Int).Mul(big.NewInt(3),r);num.Add(num,new(big.Int).Sub(d,big.NewInt(1)));num.Div(num,d);if num.String()!="1"{panic("wrong final ceil")};fmt.Println("PASS native Go RFC3339Nano/UnixMilli exact boundary and big.Int final ceil = 1 micro")
}
