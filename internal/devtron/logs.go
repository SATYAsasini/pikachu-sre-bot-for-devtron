package devtron

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Log read bounds.
//
// The orchestrator imposes none of its own — it buffers the whole log in
// memory and sends it — so every limit here is the only limit there is.
const (
	// MaxLogBytes caps one read. A crashing container can produce megabytes
	// a minute, and none of it past the first screenful changes a diagnosis.
	MaxLogBytes = 256 << 10
	// DefaultLogTailLines is what a caller gets for asking for "the logs".
	DefaultLogTailLines = 200
	// MaxLogTailLines bounds what a caller may ask for.
	MaxLogTailLines = 2000
)

// PodLogOptions bounds one container's log read.
type PodLogOptions struct {
	ClusterID int
	Namespace string
	Pod       string
	// Container is required. The orchestrator's route only matches when
	// containerName is present in the query, so without it the request does
	// not reach the handler at all.
	Container string
	// TailLines defaults to DefaultLogTailLines and is clamped to
	// MaxLogTailLines. It is never sent as zero: the validator rejects that.
	TailLines int
	// SinceSeconds, when positive, is the age of the oldest line wanted.
	SinceSeconds int
	// Previous reads the container instance before the current one, which is
	// the only place a CrashLoopBackOff or an OOMKill leaves its reason.
	Previous bool
}

// ErrNoPreviousContainer reports that a container has not restarted, so
// there is no previous instance to read.
var ErrNoPreviousContainer = fmt.Errorf("this container has no previous instance")

// PodLogs reads a bounded slice of one container's log.
//
// It uses the download route rather than the streaming one on purpose: that
// one forces follow=false, ends at EOF and returns plain text, where the
// streaming route is SSE-framed with a heartbeat and would have to be held
// open. Nothing here ever wants an open stream.
//
// The status code is checked before a single byte of the body is read, and
// this is not defensive habit. The orchestrator's log handler calls its RBAC
// check, writes 403 when it fails — and then carries on and streams the logs
// anyway, appended to the error it just wrote. A reader that looked at the
// body first would be consuming exactly the data the token was refused. So
// on anything but 200 the body is closed unread and nothing is returned from
// it, including for the error message.
func (c *Client) PodLogs(ctx context.Context, o PodLogOptions) (string, error) {
	switch {
	case o.ClusterID == 0:
		return "", fmt.Errorf("a clusterId is required to read logs")
	case strings.TrimSpace(o.Namespace) == "":
		return "", fmt.Errorf("a namespace is required to read logs")
	case strings.TrimSpace(o.Pod) == "":
		return "", fmt.Errorf("a pod name is required to read logs")
	case strings.TrimSpace(o.Container) == "":
		return "", fmt.Errorf("a container name is required; the orchestrator's log route does not match without one")
	}

	tail := o.TailLines
	if tail <= 0 {
		tail = DefaultLogTailLines
	}
	if tail > MaxLogTailLines {
		tail = MaxLogTailLines
	}

	q := url.Values{}
	q.Set("clusterId", strconv.Itoa(o.ClusterID))
	q.Set("namespace", o.Namespace)
	q.Set("containerName", o.Container)
	q.Set("tailLines", strconv.Itoa(tail))
	if o.SinceSeconds > 0 {
		q.Set("sinceSeconds", strconv.Itoa(o.SinceSeconds))
	}
	if o.Previous {
		q.Set("previous", "true")
	}

	base, token := c.creds()
	path := "/orchestrator/k8s/pods/logs/download/" + url.PathEscape(o.Pod)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("token", token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent:
		// The buffer was empty. Not a failure, and not logs either.
		return "", nil
	case http.StatusBadRequest:
		if o.Previous {
			return "", ErrNoPreviousContainer
		}
		return "", &Error{Status: resp.StatusCode, Path: path, Body: logBodyWithheld}
	default:
		return "", &Error{Status: resp.StatusCode, Path: path, Body: logBodyWithheld}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxLogBytes))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// logBodyWithheld is what a failed log read reports instead of the response
// body. The handler appends the logs to its own error, so quoting the body
// back — even to explain the failure — would leak the read that was refused.
const logBodyWithheld = "response body discarded unread: this endpoint appends log output after a refusal, so it is never read unless the status is 200"
