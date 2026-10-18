package lsm

import (
 "errors"
 "os"
 "path/filepath"
 "testing"
)

func TestFailedSSTPublicationLeavesNoFinalPartialFile(t *testing.T) {
 dir:=t.TempDir(); called:=false
 _,err:=writeSSTable(dir,1,[]kvPair{{key:[]byte("key"),value:[]byte("value")}},func(temp,target string)error{
  called=true
  if _,err:=os.Stat(target);!os.IsNotExist(err){t.Fatal("final file visible before publication")}
  sst,err:=OpenSSTable(temp);if err!=nil{t.Fatal(err)};defer sst.Close()
  value,ok:=sst.Get([]byte("key"));if !ok||string(value)!="value"{t.Fatal("publication received invalid table")}
  return errors.New("injected publication failure")
 })
 if err==nil||!called{t.Fatal("publication failure not propagated")}
 files,_:=filepath.Glob(filepath.Join(dir,"*"));if len(files)!=0{t.Fatalf("partial table leaked: %v",files)}
}
