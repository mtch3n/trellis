package cli

import (
 "encoding/json/v2"
 "testing"
 "github.com/mtch3n/trellis/internal/core"
)

func TestKnowledgeEditMetadataFlags(t *testing.T) {
 projectEnv(t)
 runCmd(t,"label","new","reviewed","--description","Reviewed")
 runCmd(t,"knowledge","new","--title","Metadata")
 runCmd(t,"knowledge","edit","metadata","--type","decision","--private=true","--tag","one","--tag","two","--label","reviewed","--if-version","1")
 var got core.Knowledge
 if err:=json.Unmarshal([]byte(runCmd(t,"knowledge","show","metadata","--json")),&got);err!=nil {t.Fatal(err)}
 if got.DocType!="decision" || !got.Private || len(got.Tags)!=2 || len(got.Labels)!=1 {t.Fatalf("%+v",got)}
 runCmd(t,"knowledge","edit","metadata","--private=false","--tag=","--label=","--if-version","2")
 if err:=json.Unmarshal([]byte(runCmd(t,"knowledge","show","metadata","--json")),&got);err!=nil {t.Fatal(err)}
 if got.Private || len(got.Tags)!=0 || len(got.Labels)!=0 {t.Fatalf("clear: %+v",got)}
}
