// Copyright 2015 The Go Authors.
// See https://go.googlesource.com/go/+/master/CONTRIBUTORS
// Licensed under the same terms as Go itself:
// https://go.googlesource.com/go/+/master/LICENSE

package http2

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	extNet        = flag.Bool("extnet", false, "do external network tests")
	transportHost = flag.String("transporthost", "http2.golang.org", "hostname to use for TestTransport")
	insecure      = flag.Bool("insecure", false, "insecure TLS dials")
)
var _ = log.Println

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

func TestTransportPanic(t *testing.T) {
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.NewBufferString(strings.Repeat("a", 1<<1))
		buf.WriteTo(w)
		time.Sleep(time.Second)
	}, optOnlyServer)
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
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	log.Println(string(data))
}

func TestTransportGzip(t *testing.T) {
	st := newServerTester(t, makeGzipHandler(func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.NewBufferString(strings.Repeat("a", 1<<20))
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
	if len(data) != 1<<20 {
		t.Fatal("data length error.")
	}
}

func TestRemote(t *testing.T) {
	tr := &Transport{InsecureTLSDial: true, Timeout: 2 * time.Second}
	defer tr.CloseIdleConnections()
	for {
		req, err := http.NewRequest("GET", "https://127.0.0.1:19927/addrs", nil)
		if err != nil {
			log.Println(err)
			continue
		}
		res, err := tr.RoundTrip(req)
		if err != nil {
			log.Println(err)
			continue
		}
		data, err := ioutil.ReadAll(res.Body)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Println(string(data))
		res.Body.Close()
	}

}

func TestTransportPost(t *testing.T) {
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, 16384)
		for {
			_, err := io.ReadFull(r.Body, data)
			if err != nil {
				log.Println("post server read body error", err)
				break
			}
		}
		w.Write([]byte("OK"))
	}, optOnlyServer)
	defer st.Close()
	tr := &Transport{InsecureTLSDial: true, Timeout: 2 * time.Second}
	defer tr.CloseIdleConnections()
	for {
		reader := bytes.NewBufferString(strings.Repeat("a", 1<<20))
		req, err := http.NewRequest("POST", st.ts.URL, reader)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		var data [2]byte
		_, err = res.Body.Read(data[:])
		if err != nil {
			log.Println("read err:", err)
			break
		}
		log.Println("read:", string(data[:]))
		res.Body.Close()
	}
}

func TestTransportGzipLoop(t *testing.T) {
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.NewBufferString(strings.Repeat("a", 1<<7))
		_, err := buf.WriteTo(w)
		if err != nil {
			log.Println("write:", err)
		}
		//log.Println("write:", n, err)
	}, optOnlyServer)
	defer st.Close()
	tr := &Transport{InsecureTLSDial: true, Timeout: 2 * time.Second}
	defer tr.CloseIdleConnections()
	for {
		req, err := http.NewRequest("GET", st.ts.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := ioutil.ReadAll(res.Body)
		if err != nil {
			log.Println("read:", len(data), err)
			break
		}
		fmt.Print("*")
		//log.Println("read:", len(data), err)
		res.Body.Close()
	}
}

type tick struct {
	TimeGen  time.Time `json:"gen"`
	TimeSend time.Time `json:"send"`
	TimeRecv time.Time `json:"recv"`
	Ask      float64   `json:"ask"`
	Bid      float64   `json:"bid"`
	Askv     float64   `json:"askv"`
	Bidv     float64   `json:"bidv"`
}

var r = rand.New(rand.NewSource(time.Now().UnixNano()))

func gentick() *tick {
	ti := &tick{}
	ti.TimeGen = time.Now()
	ti.Ask = r.Float64()
	ti.Bid = r.Float64()
	ti.Askv = r.Float64()
	ti.Bidv = r.Float64()
	return ti
}

func ticksource(ch chan *tick) chan struct{} {
	quit := make(chan struct{})
	go func(quit chan struct{}) {
		for {
			select {
			case ch <- gentick():
			case <-quit:
				return
			}
		}
	}(quit)
	return quit
}

func tserver(t *testing.T) (*serverTester, chan struct{}) {
	quitserver := make(chan struct{})
	st := newServerTester(t, makeGzipHandler(func(w http.ResponseWriter, r *http.Request) {
		ti := make(chan *tick)
		quit := ticksource(ti)
		encoder := json.NewEncoder(w)
		for {
			t := <-ti
			err := encoder.Encode(t)
			if err != nil {
				log.Println(err)
				quit <- struct{}{}
				break
			}
		}
	}), optOnlyServer)
	return st, quitserver
}

func TestTransportStreamServer(t *testing.T) {
	return
	st, quitserver := tserver(t)
	defer st.Close()
	<-quitserver
}

func getdata(tr *Transport, url string, sleep time.Duration) {
retry:
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Println(err)
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		log.Println(err)
		goto retry
	}
	defer res.Body.Close()
	i := 0
	decoder := json.NewDecoder(res.Body)
	var avg time.Duration
	var high time.Duration
	var low = 3600 * time.Second
	for {
		time.Sleep(sleep)
		var ti tick
		err := decoder.Decode(&ti)
		if err != nil {
			log.Println(err)
			goto retry
		}
		ti.TimeRecv = time.Now()
		dt := ti.TimeRecv.Sub(ti.TimeGen)
		avg += dt
		if dt > high {
			high = dt
		}
		if dt < low {
			low = dt
		}
		i++
		if i%10000 == 0 && i > 0 {
			log.Println("read", i)
			log.Println("avg", avg/10000)
			log.Println("high", high)
			log.Println("low", low)
			avg = time.Duration(0)
			high = time.Duration(0)
			low = 3600 * time.Second
		}
	}
}

func tclient(t *testing.T, url string, n int) {
	tr := &Transport{InsecureTLSDial: true, Timeout: 5 * time.Second, DisableCompression: false}
	defer tr.CloseIdleConnections()
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			getdata(tr, url, 0)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
}

func TestTransportReadSleep(t *testing.T) {
	return
	st, _ := tserver(t)
	tr := &Transport{InsecureTLSDial: true, Timeout: 5 * time.Second, DisableCompression: false}
	defer tr.CloseIdleConnections()

	go getdata(tr, st.ts.URL, 10*time.Second)
	getdata(tr, st.ts.URL, 0*time.Second)
}

func TestTransportStreamClient(t *testing.T) {
	return
	tclient(t, "https://115.231.103.9:8000", 10)
}

func TestTransportServerClient(t *testing.T) {
	return
	st, _ := tserver(t)
	defer st.Close()
	tclient(t, st.ts.URL, 10)
}

func TestTransportGet(t *testing.T) {
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.NewBufferString("")
		log.Println("wait ready body")
		io.Copy(buf, r.Body)
		buf.WriteTo(w)
		time.Sleep(10 * time.Second)
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
	log.Println("read begin")
	slurp, err := ioutil.ReadAll(res.Body)
	log.Println("read end")
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

func TestPingTime(t *testing.T) {
	ping1 := newPingTime(time.Millisecond * (1024 - 1))
	data := ping1.Bytes()
	ping2 := newPingTimeBytes(data[:])
	t1, dt1 := ping1.GetTime()
	t2, dt2 := ping2.GetTime()
	if t1 != t2 || dt1 != dt2 {
		t.Error("copy ping time error.")
		return
	}
}
