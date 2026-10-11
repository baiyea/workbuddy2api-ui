package server

import (
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/QuantumNous/new-api/relaykit/types"
 "workbuddy2api/internal/auth"
 "workbuddy2api/internal/protocol"
 "workbuddy2api/internal/upstream"
)

const reviewText = `data: {"id":"chat-review","model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}` + "\n\n"
const reviewFinish = `data: {"id":"chat-review","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
const reviewDone = "data: [DONE]\n\n"

type reviewReadFailure struct{}
func (reviewReadFailure) Read([]byte) (int,error) { return 0,io.ErrUnexpectedEOF }
type reviewCountingReader struct { io.Reader; n int }
func (r *reviewCountingReader) Read(p []byte)(int,error) { n,e:=r.Reader.Read(p);r.n+=n;return n,e }

func reviewRealHandler(t *testing.T, format types.RelayFormat, stream bool, body io.Reader, input string) (*httptest.ResponseRecorder, map[string]any) {
 t.Helper()
 var sent map[string]any
 transport:=roundTripFunc(func(r *http.Request)(*http.Response,error){
  if err:=json.NewDecoder(r.Body).Decode(&sent);err!=nil { t.Fatal(err) }
  return &http.Response{StatusCode:200,Header:http.Header{"Content-Type":[]string{"text/event-stream"}},Body:io.NopCloser(body)},nil
 })
 client:=&http.Client{Transport:transport}
 up:=&upstream.Client{HTTP:client,ChatHTTP:client,ChatBaseCN:"https://fixture.invalid",BillingBaseCN:"https://fixture.invalid"}
 p:=testPoolWith(&auth.Auth{UID:"review-synthetic",AccessToken:"fixture-token",ExpiresAt:9999999999})
 next:=NewHandler(Config{Pool:p,Upstream:up,APIKey:"fixture-api"})
 if input=="" {
  if format==types.RelayFormatClaude { input=fmt.Sprintf(`{"model":"glm-5.2","max_tokens":32,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`,stream) } else { input=fmt.Sprintf(`{"model":"glm-5.2","stream":%t,"input":"hi"}`,stream) }
 }
 w:=httptest.NewRecorder()
 protocol.Serve(w,httptest.NewRequest("POST","/",strings.NewReader(input)),next,"fixture-api",1<<20,format,"")
 return w,sent
}

func reviewSuccess(w *httptest.ResponseRecorder) bool { s:=w.Body.String();return w.Code==200&&(strings.Contains(s,`"status":"completed"`)||strings.Contains(s,`"stop_reason":"end_turn"`)||strings.Contains(s,"event: message_stop")) }

func TestFinalReviewRealTerminal(t *testing.T) {
 for _,f:=range []types.RelayFormat{types.RelayFormatOpenAIResponses,types.RelayFormatClaude} {
  for _,tc:=range []struct{name string;stream bool;source func()io.Reader}{
   {"ordinary_missing_finish",false,func()io.Reader{return strings.NewReader(reviewText+reviewDone)}},
   {"ordinary_bad_data_after_finish",false,func()io.Reader{return strings.NewReader(reviewText+reviewFinish+"data: {broken\n\n"+reviewDone)}},
   {"stream_data_after_DONE",true,func()io.Reader{return strings.NewReader(reviewText+reviewFinish+reviewDone+reviewText)}},
   {"stream_read_error_after_finish",true,func()io.Reader{return io.MultiReader(strings.NewReader(reviewText+reviewFinish),reviewReadFailure{})}},
  } { t.Run(string(f)+"/"+tc.name,func(t *testing.T){w,_:=reviewRealHandler(t,f,tc.stream,tc.source(),""); if reviewSuccess(w){t.Errorf("false success: status=%d body=%s",w.Code,w.Body.String())}}) }
 }
}

func TestFinalReviewRealPartialUsage(t *testing.T) {
 raw:=reviewText+`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`+"\n\n"+`data: {"choices":[],"usage":{"completion_tokens":0}}`+"\n\n"+reviewDone
 w,_:=reviewRealHandler(t,types.RelayFormatOpenAIResponses,false,strings.NewReader(raw),"")
 var obj map[string]any;_ = json.Unmarshal(w.Body.Bytes(),&obj)
 usage,_:=obj["usage"].(map[string]any)
 if usage["input_tokens"]!=float64(5)||usage["output_tokens"]!=float64(0)||usage["total_tokens"]!=float64(12){t.Errorf("explicit input/total lost: %s",w.Body.String())}
}

func TestFinalReviewRealRawBounds(t *testing.T) {
 t.Run("ordinary_total",func(t *testing.T){
  raw:=reviewText+strings.Repeat(":"+strings.Repeat("x",1022)+"\n\n",33000)+reviewFinish+reviewDone
  counted:=&reviewCountingReader{Reader:strings.NewReader(raw)}
  w,_:=reviewRealHandler(t,types.RelayFormatOpenAIResponses,false,counted,"")
  if reviewSuccess(w)||counted.n>32<<20+65536 {t.Errorf("raw cumulative bound bypassed: bytes_read=%d status=%d body=%s",counted.n,w.Code,w.Body.String())}
 })
 t.Run("stream_unterminated_frame",func(t *testing.T){
  counted:=&reviewCountingReader{Reader:strings.NewReader("data: "+strings.Repeat("x",9<<20))}
  w,_:=reviewRealHandler(t,types.RelayFormatOpenAIResponses,true,counted,"")
  if counted.n>8<<20+65536 {t.Errorf("frame allocated/read before bound: bytes_read=%d status=%d",counted.n,w.Code)}
 })
}

func TestFinalReviewClaudeStreamArguments(t *testing.T){
 for _,args:=range []string{"",`{"q":`,`[]`} {t.Run(args,func(t *testing.T){
  a,_:=json.Marshal(args)
  raw:=`data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"check","arguments":`+string(a)+`}}]}}]}`+"\n\n"+strings.Replace(reviewFinish,`"stop"`,`"tool_calls"`,1)+reviewDone
  w,_:=reviewRealHandler(t,types.RelayFormatClaude,true,strings.NewReader(raw),"")
  if strings.Contains(w.Body.String(),"event: message_stop"){t.Errorf("invalid object arguments completed: %s",w.Body.String())}
 })}
}

func TestFinalReviewClaudeInterleavedTools(t *testing.T){
 raw:=`data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"check","arguments":"{\"q\":"}}]}}]}`+"\n\n"+reviewText+`data: {"id":"chat-review","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}}]}}]}`+"\n\n"+strings.Replace(reviewFinish,`"stop"`,`"tool_calls"`,1)+reviewDone
 w,_:=reviewRealHandler(t,types.RelayFormatClaude,true,strings.NewReader(raw),"")
 if strings.Contains(w.Body.String(),"event: message_stop")&&!strings.Contains(w.Body.String(),`\"hi\"}`){t.Errorf("argument continuation dropped before success: %s",w.Body.String())}
}

func TestFinalReviewInlineResponsesFile(t *testing.T){
 input:=`{"model":"glm-5.2","input":[{"role":"user","content":[{"type":"input_file","filename":"note.txt","file_data":"data:text/plain;base64,aGk="}]}]}`
 w,_:=reviewRealHandler(t,types.RelayFormatOpenAIResponses,false,strings.NewReader(reviewText+reviewFinish+reviewDone),input)
 if w.Code!=200 { t.Errorf("inline file unavailable: %d %s",w.Code,w.Body.String()) }
}

func TestFinalReviewResponsesImageDetail(t *testing.T){
 input:=`{"model":"glm-5.2","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://fixture.invalid/image.png","detail":"high"}]}]}`
 w,sent:=reviewRealHandler(t,types.RelayFormatOpenAIResponses,false,strings.NewReader(reviewText+reviewFinish+reviewDone),input)
 encoded,_:=json.Marshal(sent)
 if w.Code==200&&!strings.Contains(string(encoded),`"detail":"high"`){t.Errorf("image detail silently dropped: %s",encoded)}
}

func TestFinalReviewPairedHistorySurvivesFullHandler(t *testing.T){
 for _,f:=range []types.RelayFormat{types.RelayFormatClaude,types.RelayFormatOpenAIResponses}{t.Run(string(f),func(t *testing.T){
  input:=`{"model":"glm-5.2","input":[{"role":"user","content":"hi"},{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]},{"role":"assistant","content":"checking"},{"type":"function_call","call_id":"call_a","name":"check","arguments":"{\"q\":\"old\"}"},{"type":"function_call_output","call_id":"call_a","output":"checked"}]}`
  if f==types.RelayFormatClaude {input=`{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"thinking","thinking":"think"},{"type":"text","text":"checking"},{"type":"tool_use","id":"call_a","name":"check","input":{"q":"old"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":"checked"}]}]}`}
  w,sent:=reviewRealHandler(t,f,false,strings.NewReader(reviewText+reviewFinish+reviewDone),input)
  if w.Code!=200 {t.Fatal(w.Code,w.Body.String())}
  messages:=sent["messages"].([]any);if len(messages)!=3{t.Fatal(messages)}
  assistant:=messages[1].(map[string]any);tool:=messages[2].(map[string]any)
  calls:=assistant["tool_calls"].([]any);call:=calls[0].(map[string]any);fn:=call["function"].(map[string]any)
  if assistant["reasoning_content"]!="think"||call["id"]!="call_a"||fn["name"]!="check"||fn["arguments"]!=`{"q":"old"}`||tool["tool_call_id"]!="call_a"||tool["content"]!="checked"{t.Fatal(sent)}
  raw,_:=json.Marshal(sent);t.Logf("paired history preserved: %s",raw)
 })}
}
