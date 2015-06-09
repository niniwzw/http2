// Copyright 2015 The Go Authors.
// See https://go.googlesource.com/go/+/master/CONTRIBUTORS
// Licensed under the same terms as Go itself:
// https://go.googlesource.com/go/+/master/LICENSE

package http2

import (
	"flag"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
    "bytes"
    "fmt"
	"log"
    "compress/gzip"
)

var (
	extNet        = flag.Bool("extnet", false, "do external network tests")
	transportHost = flag.String("transporthost", "http2.golang.org", "hostname to use for TestTransport")
	insecure      = flag.Bool("insecure", false, "insecure TLS dials")
)
var _  = log.Println
func TestTransportExternal(t *testing.T) {
	if !*extNet {
		t.Skip("skipping external network test")
	}
	req, _ := http.NewRequest("GET", "https://"+*transportHost+"/", nil)
	rt := &Transport{
		InsecureTLSDial: *insecure,
	}
	res, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("%v", err)
	}
	res.Write(os.Stdout)
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (w gzipResponseWriter) Flush() {
	w.Writer.(*gzip.Writer).Flush()
	w.ResponseWriter.(http.Flusher).Flush()
}

func makeGzipHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			fn(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/javascript")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		fn(gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	}
}

func TestTransportGzip(t *testing.T) {
	st := newServerTester(t, makeGzipHandler(func(w http.ResponseWriter, r *http.Request) {
        buf := bytes.NewBufferString(strings.Repeat("a", 1 << 20))
		buf.WriteTo(w)
	}), optOnlyServer)
	defer st.Close()
	tr := &Transport{InsecureTLSDial: true, Timeout: 2 * time.Second}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequest("GET", st.ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Length") != "1056" {
		t.Fatal("Content-Length error.")
	}
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 << 20 {
		t.Fatal("data length error.")
	}
}

func TestTransportStreamServer(t *testing.T) {
	st := newServerTester(t, makeGzipHandler(func(w http.ResponseWriter, r *http.Request) {
        for {
            buf := bytes.NewBufferString(strings.Repeat("a", 1 << 20))
		    _, err := buf.WriteTo(w)
            if err != nil {
                log.Println(err)
                break
            }
        }
	}), optOnlyServer)
	defer st.Close()
	select{}
}

func TestTransportStreamClient(t *testing.T) {
retry:
	tr := &Transport{InsecureTLSDial: true, Timeout: 2 * time.Second, DisableCompression:true}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequest("GET", "https://115.231.103.9:8000", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
    var read [1024]byte
    var readn int
    i := 0
    for {
        n , err := res.Body.Read(read[:])
	    if err != nil {
		    goto retry
	    }
        readn += n
        if i % 102400 == 0 {
            log.Println("read", readn / (1024 * 1024), "MB")
        }
        i++
    }
}

func TestTransportGet(t *testing.T) {
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {
        buf := bytes.NewBufferString("")
        io.Copy(buf, r.Body)
		buf.WriteTo(w)
        time.Sleep(20 * time.Second)
	}, optOnlyServer)
	defer st.Close()

	tr := &Transport{InsecureTLSDial: true, Timeout: 2 * time.Second}
	defer tr.CloseIdleConnections()
    const body = "hello world"
    reqbody := bytes.NewBufferString(body)
	req, err := http.NewRequest("GET", st.ts.URL, reqbody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	t.Logf("Got res: %+v", res)
	if g, w := res.StatusCode, 200; g != w {
		t.Errorf("StatusCode = %v; want %v", g, w)
	}
	if g, w := res.Status, "200 OK"; g != w {
		t.Errorf("Status = %q; want %q", g, w)
	}
	wantHeader := http.Header{
		"Content-Length": []string{fmt.Sprint(len(body))},
		"Content-Type":   []string{"text/plain; charset=utf-8"},
	}
	if !reflect.DeepEqual(res.Header, wantHeader) {
		t.Errorf("res Header = %v; want %v", res.Header, wantHeader)
	}
	if res.Request != req {
		t.Errorf("Response.Request = %p; want %p", res.Request, req)
	}
	if res.TLS == nil {
		t.Error("Response.TLS = nil; want non-nil")
	}
	slurp, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Errorf("Body read: %v", err)
	} else if string(slurp) != body {
		t.Errorf("Body = %q; want %q", slurp, body)
	}
}

func TestTransportReusesConns(t *testing.T) {
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.RemoteAddr)
	}, optOnlyServer)
	defer st.Close()
	tr := &Transport{InsecureTLSDial: true}
	defer tr.CloseIdleConnections()
	get := func() string {
		req, err := http.NewRequest("GET", st.ts.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		slurp, err := ioutil.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("Body read: %v", err)
		}
		addr := strings.TrimSpace(string(slurp))
		if addr == "" {
			t.Fatalf("didn't get an addr in response")
		}
		return addr
	}
	first := get()
	second := get()
	if first != second {
		t.Errorf("first and second responses were on different connections: %q vs %q", first, second)
	}
}

func TestTransportAbortClosesPipes(t *testing.T) {
	shutdown := make(chan struct{})
	st := newServerTester(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			<-shutdown
		},
		optOnlyServer,
	)
	defer st.Close()
	defer close(shutdown) // we must shutdown before st.Close() to avoid hanging

	done := make(chan struct{})
	requestMade := make(chan struct{})
	go func() {
		defer close(done)
		tr := &Transport{
			InsecureTLSDial: true,
		}
		req, err := http.NewRequest("GET", st.ts.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		close(requestMade)
		_, err = ioutil.ReadAll(res.Body)
		if err == nil {
			t.Error("expected error from res.Body.Read")
		}
	}()

	<-requestMade
	// Now force the serve loop to end, via closing the connection.
	st.closeConn()
	// deadlock? that's a bug.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}
