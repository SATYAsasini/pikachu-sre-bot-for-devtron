package runs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRow feeds scanRun a fixed set of values, so the column-count contract
// can be checked without a database.
type fakeRow struct {
	vals []any
	err  error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if len(dest) != len(f.vals) {
		return errors.New("number of field descriptions must equal number of destinations, " +
			"got " + itoa(len(f.vals)) + " and " + itoa(len(dest)))
	}
	for i, v := range f.vals {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *time.Time:
			*d = v.(time.Time)
		case **time.Time:
			if v == nil {
				*d = nil
			} else {
				t := v.(time.Time)
				*d = &t
			}
		case *[]byte:
			if v == nil {
				*d = nil
			} else {
				*d = v.([]byte)
			}
		default:
			return errors.New("fakeRow: unhandled destination type")
		}
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// The count in runColumns and the count scanRun destinations must agree.
// When they drifted, pgx failed every Get and List with an error that names
// no column, and the only visible symptom was runs stuck in `queued` because
// the worker could not load the row it had just been handed.
func TestRunColumnsMatchScanDestinations(t *testing.T) {
	t.Parallel()

	want := len(strings.Split(strings.ReplaceAll(runColumns, "\n\t", " "), ","))

	var got int
	_, _ = scanRun(probeRow{n: &got})

	if got != want {
		t.Fatalf("runColumns selects %d columns but scanRun binds %d destinations", want, got)
	}
}

// probeRow records how many destinations scanRun asks for.
type probeRow struct{ n *int }

func (p probeRow) Scan(dest ...any) error {
	*p.n = len(dest)
	return errStop
}

var errStop = errors.New("probe")

func TestScanRunReadsOptions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	opts, err := json.Marshal(RunOptions{Depth: DepthQuick, MaxToolCalls: 7, WindowMinutes: 180})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		opt  []byte
		want RunOptions
	}{
		{"options round-trip", opts, RunOptions{Depth: DepthQuick, MaxToolCalls: 7, WindowMinutes: 180}},
		{"empty object", []byte(`{}`), RunOptions{}},
		{"sql null", nil, RunOptions{}},
		{"json null", []byte(`null`), RunOptions{}},
		{"malformed is ignored rather than fatal", []byte(`{ this is not json`), RunOptions{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := fakeRow{vals: []any{
				"run-1", StatusSucceeded, now, nil, nil,
				[]byte(`{"clusterId":1,"clusterName":"default_cluster"}`),
				[]byte(`{"kind":"alert","ask":""}`),
				nil, nil, nil,
				[]byte(`{"toolCalls":3}`),
				tt.opt,
				"",
			}}
			got, err := scanRun(row)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if got.Options != tt.want {
				t.Errorf("want %+v, got %+v", tt.want, got.Options)
			}
			if got.Scope.ClusterName != "default_cluster" {
				t.Errorf("scope did not survive: %+v", got.Scope)
			}
		})
	}
}
