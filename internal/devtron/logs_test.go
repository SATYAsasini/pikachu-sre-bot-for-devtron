package devtron

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fxLogText is what the download route returns: every line rewritten to
// "<RFC1123 GMT timestamp> <message>" by the handler.
func fxLogText() string {
	return "Thu, 25 Sep 2026 06:41:00 GMT starting pgvector\n" +
		"Thu, 25 Sep 2026 06:41:02 GMT FATAL: could not map anonymous shared memory\n" +
		"Thu, 25 Sep 2026 06:41:02 GMT HINT: reduce shared_buffers\n"
}

// logServer stands in for the orchestrator's log download route, including
// its behaviour on a refusal: the handler writes 403 and then streams the
// logs anyway, appended to the error it just wrote.
type logServer struct {
	srv *httptest.Server

	status int
	body   string
	// leak is appended after the body on a non-200, reproducing the handler
	// that keeps going after writing its refusal.
	leak string

	gotQuery url.Values
	gotPath  string
	gotToken string
}

func newLogServer(status int, body, leak string) (*logServer, *Client) {
	l := &logServer{status: status, body: body, leak: leak}
	mux := http.NewServeMux()
	mux.HandleFunc("/orchestrator/k8s/pods/logs/download/", func(w http.ResponseWriter, r *http.Request) {
		l.gotQuery = r.URL.Query()
		l.gotPath = r.URL.Path
		l.gotToken = r.Header.Get("token")

		if l.status != http.StatusOK && l.status != http.StatusNoContent {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(l.status)
			_, _ = w.Write([]byte(l.body))
			_, _ = w.Write([]byte(l.leak))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(l.status)
		_, _ = w.Write([]byte(l.body))
	})
	l.srv = httptest.NewServer(mux)
	return l, New(Options{BaseURL: l.srv.URL, Token: "t", Timeout: 5 * time.Second})
}

func (l *logServer) Close() { l.srv.Close() }

func fxOptions() PodLogOptions {
	return PodLogOptions{ClusterID: 1, Namespace: "devtroncd", Pod: "pgvector-0", Container: "pgvector"}
}

func TestPodLogsReadsTheDownloadRoute(t *testing.T) {
	t.Parallel()

	l, c := newLogServer(http.StatusOK, fxLogText(), "")
	defer l.Close()

	got, err := c.PodLogs(t.Context(), fxOptions())
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if !strings.Contains(got, "could not map anonymous shared memory") {
		t.Errorf("got %q", got)
	}
	if !strings.HasSuffix(l.gotPath, "/pgvector-0") {
		t.Errorf("path: %q", l.gotPath)
	}
	// The orchestrator's route only matches when containerName is present,
	// and its validator rejects a tailLines of zero.
	if l.gotQuery.Get("containerName") != "pgvector" {
		t.Errorf("containerName: %q", l.gotQuery.Get("containerName"))
	}
	if l.gotQuery.Get("tailLines") == "" || l.gotQuery.Get("tailLines") == "0" {
		t.Errorf("tailLines must be sent and positive, got %q", l.gotQuery.Get("tailLines"))
	}
	if l.gotQuery.Get("namespace") != "devtroncd" || l.gotQuery.Get("clusterId") != "1" {
		t.Errorf("query: %v", l.gotQuery)
	}
	// This is an /orchestrator call, so the plain token header, not Bearer.
	if l.gotToken != "t" {
		t.Errorf("token header: %q", l.gotToken)
	}
}

// The trap. The orchestrator's log handler calls its RBAC check, writes 403
// when it fails, and then streams the logs anyway. A reader that looked at
// the body would be consuming precisely the data the token was refused.
func TestPodLogsNeverReadsARefusedBody(t *testing.T) {
	t.Parallel()

	const secret = "SUPER-SECRET-LOG-LINE-THAT-MUST-NOT-ESCAPE"
	l, c := newLogServer(http.StatusForbidden,
		`{"code":403,"errors":[{"userMessage":"unauthorized"}]}`, secret)
	defer l.Close()

	got, err := c.PodLogs(t.Context(), fxOptions())
	if err == nil {
		t.Fatal("a refusal must be an error")
	}
	if got != "" {
		t.Fatalf("nothing may be returned from a refused read, got %q", got)
	}
	// Not in the error either. The message is built from the status alone.
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the refused body reached the error: %v", err)
	}
	var de *Error
	if !errors.As(err, &de) || de.Status != http.StatusForbidden {
		t.Errorf("want a 403 Error, got %v", err)
	}
}

func TestPodLogsStatusHandling(t *testing.T) {
	t.Parallel()

	// No content is not a failure; it means the container has written
	// nothing, which is a different thing from being unable to read it.
	l, c := newLogServer(http.StatusNoContent, "", "")
	defer l.Close()
	got, err := c.PodLogs(t.Context(), fxOptions())
	if err != nil || got != "" {
		t.Errorf("204: want empty and no error, got %q / %v", got, err)
	}

	// A 400 on previous=true means the container has never restarted.
	l2, c2 := newLogServer(http.StatusBadRequest, `{"code":400}`, "leaked")
	defer l2.Close()
	o := fxOptions()
	o.Previous = true
	if _, err := c2.PodLogs(t.Context(), o); !errors.Is(err, ErrNoPreviousContainer) {
		t.Errorf("want ErrNoPreviousContainer, got %v", err)
	}

	// The same 400 without previous is an ordinary failure.
	o.Previous = false
	if _, err := c2.PodLogs(t.Context(), o); errors.Is(err, ErrNoPreviousContainer) {
		t.Errorf("a 400 on the current instance is not a missing previous one")
	}
}

func TestPodLogsBounds(t *testing.T) {
	t.Parallel()

	l, c := newLogServer(http.StatusOK, strings.Repeat("x", MaxLogBytes*2), "")
	defer l.Close()

	// The orchestrator caps nothing and buffers the whole log in memory, so
	// the client's ceiling is the only one there is.
	got, err := c.PodLogs(t.Context(), fxOptions())
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if len(got) != MaxLogBytes {
		t.Errorf("want the read capped at %d bytes, got %d", MaxLogBytes, len(got))
	}

	// An absurd tailLines is clamped rather than passed through.
	o := fxOptions()
	o.TailLines = 1_000_000
	if _, err := c.PodLogs(t.Context(), o); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if l.gotQuery.Get("tailLines") != "2000" {
		t.Errorf("want the request clamped to %d, got %q", MaxLogTailLines, l.gotQuery.Get("tailLines"))
	}
}

func TestPodLogsRequiresItsIdentifiers(t *testing.T) {
	t.Parallel()

	l, c := newLogServer(http.StatusOK, fxLogText(), "")
	defer l.Close()

	for name, mutate := range map[string]func(*PodLogOptions){
		"no cluster":   func(o *PodLogOptions) { o.ClusterID = 0 },
		"no namespace": func(o *PodLogOptions) { o.Namespace = " " },
		"no pod":       func(o *PodLogOptions) { o.Pod = "" },
		// Without it the route does not match at all, so this has to be
		// caught here rather than surfacing as a confusing 404.
		"no container": func(o *PodLogOptions) { o.Container = "" },
	} {
		t.Run(name, func(t *testing.T) {
			o := fxOptions()
			mutate(&o)
			if _, err := c.PodLogs(t.Context(), o); err == nil {
				t.Error("want a refusal before the request is sent")
			}
		})
	}
}

func TestPodLogsPreviousAndSince(t *testing.T) {
	t.Parallel()

	l, c := newLogServer(http.StatusOK, fxLogText(), "")
	defer l.Close()

	o := fxOptions()
	o.Previous = true
	o.SinceSeconds = 900
	if _, err := c.PodLogs(t.Context(), o); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if l.gotQuery.Get("previous") != "true" {
		t.Errorf("previous: %q", l.gotQuery.Get("previous"))
	}
	if l.gotQuery.Get("sinceSeconds") != "900" {
		t.Errorf("sinceSeconds: %q", l.gotQuery.Get("sinceSeconds"))
	}
	// follow is never sent: the download route forces it off, and an open
	// stream is not something this ever wants.
	if l.gotQuery.Has("follow") {
		t.Error("follow must not be sent")
	}
}
