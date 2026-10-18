package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
 "github.com/noosphere/backend/internal/config"
 "github.com/noosphere/backend/internal/core"
 "github.com/noosphere/backend/internal/nebula/engine"
)

func TestSlowEmbeddingRebuildDoesNotBlockModuleReadiness(t *testing.T) {
 release:=make(chan struct{});started:=make(chan struct{},1)
 provider:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  select{case started<-struct{}{}:default:}
  select {case <-release: w.Header().Set("Content-Type","application/json");w.Write([]byte(`{"data":[{"embedding":[1,0]}]}`));case <-r.Context().Done():}
 }));defer provider.Close();defer close(release)
 dir:=t.TempDir();seed,err:=engine.Open(engine.DefaultOptions(dir));if err!=nil{t.Fatal(err)};seed.PutText("fixture:memory",[]byte("source"),"source");seed.Close()
 cfg,_:=config.Load("");cfg.Modules.Nebula.DataDir=dir;cfg.Modules.Nebula.Embedding.Provider="openai";cfg.Modules.Nebula.Embedding.APIKey="fixture";cfg.Modules.Nebula.Embedding.BaseURL=provider.URL
 ready:=make(chan *engine.Engine,1)
 go func(){ready<-initNebula(cfg,core.New(),http.NewServeMux(),nil)}()
 select {
 case eng:=<-ready:
  if eng==nil {t.Fatal("engine missing")};defer eng.Close()
  select {case <-started:case <-time.After(time.Second):t.Fatal("rebuild did not start")}
 case <-time.After(300*time.Millisecond):t.Fatal("remote rebuild blocks startup readiness")
 }
}
