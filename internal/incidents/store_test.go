package incidents

import (
	"regexp"
	"strconv"
	"testing"
)

var placeholder = regexp.MustCompile(`\$(\d+)`)

// Postgres counts the placeholders and pgx counts the arguments, and when they
// disagree the statement is refused at execution — not at compile time, not in
// review. That is how resolving an alert silently did nothing: every branch of
// this switch was handed `id, actor` while only one of them had a `$2`, so the
// row never changed and the API returned the unmodified alert, which reads
// exactly like a resolve that stuck.
func TestStateUpdateArgumentsMatchPlaceholders(t *testing.T) {
	t.Parallel()

	for _, st := range []State{StateFiring, StateAcknowledged, StateResolved, State("something new")} {
		q, args := stateUpdate("alert-1", st, "satya")

		highest := 0
		for _, m := range placeholder.FindAllStringSubmatch(q, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("%s: unparseable placeholder %q", st, m[0])
			}
			if n > highest {
				highest = n
			}
		}
		if highest != len(args) {
			t.Errorf("%s: query uses $%d but %d arguments were supplied", st, highest, len(args))
		}
		if len(args) == 0 || args[0] != "alert-1" {
			t.Errorf("%s: the id must be $1, got %v", st, args)
		}
	}
}

// An unknown state must behave like reopening rather than like nothing. A
// transition this code does not recognise still has to leave the row in a
// state somebody can act on.
func TestStateUpdateUnknownStateReopens(t *testing.T) {
	t.Parallel()

	q, _ := stateUpdate("alert-1", State("garbage"), "satya")
	if !regexp.MustCompile(`state = 'firing'`).MatchString(q) {
		t.Errorf("an unknown state produced %q", q)
	}
}

// Who acknowledged it is the one piece of actor information this table keeps,
// so it must be the acknowledge branch that carries it and no other.
func TestStateUpdateOnlyAcknowledgeRecordsTheActor(t *testing.T) {
	t.Parallel()

	if _, args := stateUpdate("alert-1", StateAcknowledged, "satya"); len(args) != 2 || args[1] != "satya" {
		t.Errorf("acknowledge dropped the actor: %v", args)
	}
	for _, st := range []State{StateResolved, StateFiring} {
		if _, args := stateUpdate("alert-1", st, "satya"); len(args) != 1 {
			t.Errorf("%s passed an actor it has nowhere to put: %v", st, args)
		}
	}
}

// Unicode, empty strings and absurd lengths all reach the database as
// arguments rather than as SQL, so none of them may change the statement.
func TestStateUpdateNeverInterpolates(t *testing.T) {
	t.Parallel()

	hostile := []string{"", "satya'; drop table alerts; --", "サティヤ", "a\x00b"}
	base, _ := stateUpdate("alert-1", StateAcknowledged, "satya")

	for _, actor := range hostile {
		q, args := stateUpdate("alert-1", StateAcknowledged, actor)
		if q != base {
			t.Errorf("actor %q changed the statement", actor)
		}
		if args[1] != actor {
			t.Errorf("actor %q did not survive as an argument", actor)
		}
	}
}
