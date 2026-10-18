package lsm

import (
 "testing"
 "os"
 "path/filepath"
)

func TestWALFailureDoesNotReportSuccessfulWrite(t *testing.T) {
	l, err := OpenLSM(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.wal.Close()
	if err = l.Put([]byte("key"), []byte("value")); err == nil {
		t.Fatal("write succeeded after WAL closed")
	}
	if _, ok := l.Get([]byte("key")); ok {
		t.Fatal("failed write visible in memory")
	}
}

func TestPartialWALTailDoesNotConsumeNextSuccessfulWrite(t *testing.T) {
 dir:=t.TempDir();path:=filepath.Join(dir,"nebula.wal")
 if err:=os.WriteFile(path,[]byte("{\"op\":\"put\",\"key\":\"old\",\"value\":\"b2s=\"}\n{\"op\":\"put\",\"key\":"),0600);err!=nil {t.Fatal(err)}
 l,err:=OpenLSM(dir);if err!=nil{t.Fatal(err)}
 if err=l.Put([]byte("new"),[]byte("committed"));err!=nil {t.Fatal(err)}
 // Simulate another crash without the normal flush-on-close.
 close(l.stopCh);l.wal.Close()
 recovered,err:=OpenLSM(dir);if err!=nil{t.Fatal(err)};defer recovered.Close()
 if value,ok:=recovered.Get([]byte("new"));!ok||string(value)!="committed" {t.Fatalf("acknowledged write lost: %q %v",value,ok)}
}

func TestMalformedCompleteWALRecordFailsClosed(t *testing.T) {
 dir:=t.TempDir();os.WriteFile(filepath.Join(dir,"nebula.wal"),[]byte("broken\n"),0600)
 l,err:=OpenLSM(dir);if err==nil{l.Close();t.Fatal("silently accepted corrupt complete record")}
}
