package server

import (
 "bufio"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os/exec"
 "path/filepath"
 "testing"
 "strings"

 "workbuddy2api/internal/anthropic"
 "workbuddy2api/internal/auth"
 "workbuddy2api/internal/responses"
 "workbuddy2api/internal/upstream"
)

// Local diagnostic only: does not replace Linux container acceptance.
func TestFinalFixNativeSDK(t *testing.T) {
 root := `D:/Code/workbuddy2api-ui`
 python := filepath.Join(root, ".build/protocol-sdk-venv/Scripts/python.exe")
 mock := exec.Command(python, "-u", "-c", `import sys;sys.path.insert(0,'deploy');from mock_upstream import Handler;from http.server import ThreadingHTTPServer;s=ThreadingHTTPServer(('127.0.0.1',0),Handler);print(s.server_port,flush=True);s.serve_forever()`)
 mock.Dir = root
 pipe,err:=mock.StdoutPipe();if err!=nil {t.Fatal(err)}
 if err=mock.Start();err!=nil {t.Fatal(err)}
 defer func(){_ = mock.Process.Kill(); _ = mock.Wait()}()
 scanner:=bufio.NewScanner(pipe);if !scanner.Scan(){t.Fatal("mock startup",scanner.Err())}
 url:="http://127.0.0.1:"+scanner.Text()
 client:=&http.Client{Transport:&http.Transport{}}
 up:=&upstream.Client{HTTP:client,ChatHTTP:client,ChatBaseCN:url,BillingBaseCN:url,ChatBaseGlobal:url,BillingBaseGlobal:url,GlobalEnabled:true}
 pool:=testPoolWith(&auth.Auth{UID:"sdk-synthetic",Domain:"www.workbuddy.ai",AccessToken:"fixture-token",ExpiresAt:9999999999})
 handler:=NewHandler(Config{Pool:pool,Upstream:up,APIKey:"fixture-api"})
 gateway:=httptest.NewServer(responses.New(anthropic.New(handler,"fixture-api",8<<20),"fixture-api",8<<20));defer gateway.Close()
 for _,script:=range []string{"check_anthropic_sdk.py","check_responses_sdk.py"} {
  cmd:=exec.Command(python,filepath.Join(root,"scripts",script),"--url",gateway.URL,"--key","fixture-api","--model","global:mock-model")
  output,err:=cmd.CombinedOutput();t.Logf("%s:\n%s",script,output)
  if err!=nil {t.Fatal(fmt.Errorf("%s: %w",script,err))}
 }
 code := fmt.Sprintf(`from pathlib import Path
code=Path('scripts/check_protocol_gateway.py').read_text()
code=code[:code.index('    # Unsupported state')]+'\nmain()\n'
code=code.replace("'console', 7863", "'127.0.0.1', %s").replace("'mock', 8080", "'127.0.0.1', %s")
exec(code.replace("legacy-api", "fixture-api"))
`,strings.TrimPrefix(gateway.URL,"http://127.0.0.1:"),scanner.Text())
 cmd:=exec.Command(python,"-c",code,gateway.URL,"claude","responses");cmd.Dir=root
 output,err:=cmd.CombinedOutput();t.Logf("outbound assertions:\n%s",output);if err!=nil {t.Fatal(err)}

}
