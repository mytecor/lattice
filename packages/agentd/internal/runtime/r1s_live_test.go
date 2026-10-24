package runtime

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mytecor/lattice/agentd/internal/acp"
	taskcontract "github.com/mytecor/lattice/task-contract"
	r1sv1 "github.com/mytecor/r1s/api/gen/r1s/v1"
	r1s "github.com/mytecor/r1s/client"
)

// Opt-in acceptance on a Linux allocator host. It uses the actual immutable
// Pi/hydra image, a disposable source repository and a deterministic local LLM
// fixture. It requires no provider credentials or container egress.
func TestR1SContainerTurn(t *testing.T) {
	image := os.Getenv("R1S_LIVE_IMAGE")
	if image == "" {
		t.Skip("set R1S_LIVE_IMAGE to run disposable container acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	c, err := r1s.Open(os.Getenv("R1S_LIVE_CLUSTER"), r1s.Config{})
	if err != nil {
		t.Fatal("cannot open joined r1s cluster")
	}
	tunnel, err := c.NewRunTunnel([]r1s.TunnelTarget{{Port: 55514}})
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	bootstrap := os.Getenv("R1S_LIVE_BOOTSTRAP")
	if !strings.HasPrefix(bootstrap, "/nix/store/") || strings.ContainsAny(bootstrap, "'\n") {
		t.Fatal("R1S_LIVE_BOOTSTRAP must name the image bootstrap")
	}
	models := map[string]any{"providers": map[string]any{"lattice-live": map[string]any{
		"baseUrl": "http://127.0.0.1:9208/v1", "api": "openai-completions", "apiKey": "fixture-only",
		"models": []any{map[string]any{"id": "fixture", "name": "fixture", "reasoning": false, "input": []string{"text"}, "contextWindow": 32000, "maxTokens": 1024}},
	}}}
	modelJSON, _ := json.Marshal(models)
	script := `set -eu
mkdir -p /root/.pi/agent /fixture/source
git -C /fixture/source init -q
git -C /fixture/source -c user.name=Fixture -c user.email=fixture@example.org commit --allow-empty -qm fixture
cat > /tmp/fixture-model.cjs <<'MODEL'
const http=require('http');
http.createServer((req,res)=>{
 let body=''; req.on('data',c=>body+=c); req.on('end',()=>{
  const request=JSON.parse(body||'{}');
  const base={id:'fixture-completion',object:'chat.completion.chunk',created:1,model:'fixture'};
  if(request.stream){
   res.writeHead(200,{'content-type':'text/event-stream'});
   res.write('data: '+JSON.stringify({...base,choices:[{index:0,delta:{role:'assistant',content:'OK'},finish_reason:null}]})+'\n\n');
   res.write('data: '+JSON.stringify({...base,choices:[{index:0,delta:{},finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}})+'\n\n');
   res.end('data: [DONE]\n\n');
  }else{res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({...base,object:'chat.completion',choices:[{index:0,message:{role:'assistant',content:'OK'},finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}));}
 });
}).listen(9208,'127.0.0.1');
MODEL
for node in /nix/store/*nodejs*/bin/node; do "$node" /tmp/fixture-model.cjs & break; done
`
	script += "printf '%s' '" + string(modelJSON) + "' > /root/.pi/agent/models.json\n"
	script += "printf '%s' '{\"defaultProvider\":\"lattice-live\",\"defaultModel\":\"fixture\",\"defaultThinkingLevel\":\"off\"}' > /root/.pi/agent/settings.json\n"
	script += "exec '" + bootstrap + "'"
	r, err := NewR1SRuntime(R1SConfig{Client: r1s.Config{ClusterKey: make([]byte, 32)}, ResourceClass: os.Getenv("R1S_LIVE_RESOURCE_CLASS"), OfferWait: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	r.create = func() (runClient, runTunnel, error) {
		return &liveFixtureClient{Client: c, script: script}, tunnel, nil
	}
	spec := &taskcontract.TaskSpec{ID: "f19-02-live", Source: taskcontract.SourceSpec{Repo: "/fixture/source", Revision: "HEAD"}, Runtime: taskcontract.RuntimeSpec{Image: image}, Context: "Connectivity acceptance only. Do not change or publish any source files."}
	instance, err := r.Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Stop(context.Background())
	client := &acp.WebSocketClient{Dial: r.DialACP}
	session, err := client.Connect(ctx, instance.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = session.Initialize(ctx, spec); err != nil {
		t.Fatal(err)
	}
	result, err := session.Prompt(ctx, "Reply with OK only. Do not call tools or change any files.")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != acp.TurnCompleted || !strings.Contains(result.Message, "OK") {
		t.Fatal("agent did not complete ACP turn with fixture response")
	}
	t.Log("Real Pi container completed ACP turn over public r1s run/tunnel API with local LLM fixture")
}

type liveFixtureClient struct {
	*r1s.Client
	script string
}

func (c *liveFixtureClient) Run(ctx context.Context, req *r1sv1.ExecutionRequest, options r1s.RunOptions) (r1s.Result, error) {
	// Fixture-only startup prepares declared source/model inputs inside the
	// disposable image. The production bootstrap and host mounts stay untouched.
	req.Workload.Command = []string{"/bin/bash", "-c", c.script}
	return c.Client.Run(ctx, req, options)
}
