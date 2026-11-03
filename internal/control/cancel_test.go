package control

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "sync/atomic"
    "testing"
    "time"
)

type cancelTestExecutor struct {
    started chan struct{}
    release chan struct{}
    invoked atomic.Int32
}
func (e *cancelTestExecutor) Run(ctx context.Context, binary string, args ...string)(string,error){
    e.invoked.Add(1)
    select {case e.started<-struct{}{}:default:}
    select {case <-ctx.Done():return "interrupted",ctx.Err();case <-e.release:return "finished",nil}
}
func cancelStubEngine(t *testing.T) {
    t.Helper()
    dir:=t.TempDir()
    if err:=os.WriteFile(filepath.Join(dir,"kopia"),[]byte("#!/bin/sh\nexit 0\n"),0700);err!=nil{t.Fatal(err)}
    t.Setenv("PATH",dir)
}
func enqueueCancelTestJob(t *testing.T,srv http.Handler)string{
    t.Helper()
    req:=httptest.NewRequest("POST","http://localhost:8787/api/v1/jobs",strings.NewReader("{\"type\":\"snapshots\"}"))
    req.Header.Set("Authorization","Bearer "+testToken)
    req.Header.Set("Content-Type","application/json")
    rec:=httptest.NewRecorder()
    srv.ServeHTTP(rec,req)
    if rec.Code!=202{t.Fatalf("enqueue status=%d body=%s",rec.Code,rec.Body.String())}
    var result struct{ID string `json:"id"`}
    if err:=json.Unmarshal(rec.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
    if result.ID==""{t.Fatal("empty job ID")}
    return result.ID
}
func cancelTestStatus(t *testing.T,srv http.Handler,id string) Job{
    t.Helper()
    req:=httptest.NewRequest("GET","http://localhost:8787/api/v1/jobs/"+id,nil)
    req.Header.Set("Authorization","Bearer "+testToken)
    rec:=httptest.NewRecorder()
    srv.ServeHTTP(rec,req)
    if rec.Code!=200{t.Fatalf("GET job returned %d",rec.Code)}
    var job Job
    if err:=json.Unmarshal(rec.Body.Bytes(),&job);err!=nil{t.Fatal(err)}
    return job
}
func requestCancel(t *testing.T,srv http.Handler,id,token string)int{
    t.Helper()
    req:=httptest.NewRequest("POST","http://localhost:8787/api/v1/jobs/"+id+"/cancel",nil)
    if token!=""{req.Header.Set("Authorization","Bearer "+token)}
    rec:=httptest.NewRecorder()
    srv.ServeHTTP(rec,req)
    return rec.Code
}
func awaitCancelledStatus(t *testing.T,srv http.Handler,id,status string)Job{
    t.Helper()
    until:=time.Now().Add(3*time.Second)
    for time.Now().Before(until){
        job:=cancelTestStatus(t,srv,id)
        if job.Status==status{return job}
        time.Sleep(5*time.Millisecond)
    }
    t.Fatalf("job failed to reach %s: %+v",status,cancelTestStatus(t,srv,id))
    return Job{}
}
func TestCancelRunningOperation(t *testing.T){
    cancelStubEngine(t)
    engine:=&cancelTestExecutor{started:make(chan struct{},2),release:make(chan struct{})}
    srv:=NewServer(testToken,engine)
    id:=enqueueCancelTestJob(t,srv)
    select{case <-engine.started:case <-time.After(2*time.Second):t.Fatal("worker never started")}
    if got:=requestCancel(t,srv,id,"");got!=401{t.Fatalf("unauthenticated cancel: %d",got)}
    if got:=requestCancel(t,srv,id,testToken);got!=202{t.Fatalf("cancel returned %d",got)}
    job:=awaitCancelledStatus(t,srv,id,"cancelled")
    if !job.CancelRequested||job.CompletedAt==nil{t.Fatalf("cancel metadata: %+v",job)}
    if got:=requestCancel(t,srv,id,testToken);got!=409{t.Fatalf("cancel finished returned %d",got)}
}
func TestCancelQueuedNeverRuns(t *testing.T){
    cancelStubEngine(t)
    engine:=&cancelTestExecutor{started:make(chan struct{},2),release:make(chan struct{})}
    srv:=NewServer(testToken,engine)
    first:=enqueueCancelTestJob(t,srv)
    select{case <-engine.started:case <-time.After(2*time.Second):t.Fatal("first never started")}
    second:=enqueueCancelTestJob(t,srv)
    if got:=requestCancel(t,srv,second,testToken);got!=202{t.Fatalf("queued cancel returned %d",got)}
    awaitCancelledStatus(t,srv,second,"cancelled")
    close(engine.release)
    awaitCancelledStatus(t,srv,first,"completed")
    if got:=engine.invoked.Load();got!=1{t.Fatalf("queued cancelled command ran: %d",got)}
}
func TestCancelledJobPersists(t *testing.T){
    cancelStubEngine(t)
    engine:=&cancelTestExecutor{started:make(chan struct{},1),release:make(chan struct{})}
    dir:=filepath.Join(t.TempDir(),"state")
    srv,err:=NewServerWithState(testToken,engine,dir)
    if err!=nil{t.Fatal(err)}
    id:=enqueueCancelTestJob(t,srv)
    select{case <-engine.started:case <-time.After(2*time.Second):t.Fatal("worker not started")}
    if got:=requestCancel(t,srv,id,testToken);got!=202{t.Fatalf("cancel returned %d",got)}
    awaitCancelledStatus(t,srv,id,"cancelled")
    reopened,err:=NewServerWithState(testToken,&fakeExecutor{},dir)
    if err!=nil{t.Fatal(err)}
    job:=cancelTestStatus(t,reopened,id)
    if job.Status!="cancelled"||job.CompletedAt==nil{t.Fatalf("journal state: %+v",job)}
    if job.Source!=""||job.Destination!=""||job.Output!=""{t.Fatal("sensitive fields persisted")}
}
func TestProcessTimeoutStopsChild(t *testing.T){
    dir:=t.TempDir()
    if err:=os.WriteFile(filepath.Join(dir,"kopia"),[]byte("#!/bin/sh\nexec /bin/sleep 30\n"),0700);err!=nil{t.Fatal(err)}
    t.Setenv("PATH",dir)
    ctx,cancel:=context.WithTimeout(context.Background(),100*time.Millisecond)
    defer cancel()
    started:=time.Now()
    _,err:=(OSExecutor{}).Run(ctx,"kopia","snapshot","list")
    if err==nil{t.Fatal("engine should have been interrupted")}
    if time.Since(started)>3*time.Second{t.Fatal("context failed to stop child")}
}
func TestNoTokenInJobOutput(t *testing.T){
    s:=&Server{token:testToken}
    job:=&Job{}
    s.finishJob(job,"log: "+testToken,nil,nil)
    if strings.Contains(job.Output,testToken)||!strings.Contains(job.Output,"[redacted]"){t.Fatal("management token visible in output")}
}
