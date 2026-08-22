package postgres

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// CommitWasRefused decides between two outcomes a person acts on differently,
// so every branch of it is exercised here with a constructed error. A live
// server can produce some of these and not others — a FATAL in an ordinary
// class, an ERROR in a connection class — and the ones it cannot produce are
// exactly the ones that were unpinned.
func TestCommitWasRefused(t *testing.T) {
	pg := func(severity, code string) error {
		return &pgconn.PgError{Severity: severity, Code: code, Message: "constructed"}
	}

	for name, c := range map[string]struct {
		err  error
		want bool
	}{
		// The case this exists for: a deferred constraint firing at COMMIT.
		"an integrity violation":  {pg("ERROR", "23505"), true},
		"a check violation":       {pg("ERROR", "23514"), true},
		"a serialisation failure": {pg("ERROR", "40001"), true},

		// The server is going away, not declining. 57P01 is what terminating a
		// backend produces, and it arrives as a PgError — which is why "the
		// server sent a message" is not the test.
		"admin shutdown":     {pg("FATAL", "57P01"), false},
		"crash shutdown":     {pg("FATAL", "57P02"), false},
		"cannot connect now": {pg("FATAL", "57P03"), false},
		// An ERROR in ANY class is the server declining: it processed the
		// COMMIT and answered. A deferred constraint trigger that RAISEs
		// produces XX000, and application code chooses that SQLSTATE — so
		// excluding the class would call a definite rollback indeterminate.
		"an ERROR in a connection class": {pg("ERROR", "08006"), true},
		"an ERROR in a system class":     {pg("ERROR", "58030"), true},
		"a raised internal error":        {pg("ERROR", "XX000"), true},
		"a cancelled commit":             {pg("ERROR", "57014"), true},

		// Severity alone settles it, whatever the class: a FATAL in an
		// ordinary class is still the session ending.
		"a FATAL in an ordinary class": {pg("FATAL", "23505"), false},
		"a PANIC in an ordinary class": {pg("PANIC", "23505"), false},

		// No message at all: the connection went away.
		"a network error":   {&net.OpError{Op: "read", Err: errors.New("connection reset")}, false},
		"a plain error":     {errors.New("something"), false},
		"a wrapped PgError": {fmt.Errorf("committing: %w", pg("ERROR", "23505")), true},
		"nothing":           {nil, false},

		// A non-English server: Severity is translated and SeverityUnlocalized
		// is not. Reading the localised field would call this a refusal, and it
		// is a session ending.
		"a localised FATAL": {&pgconn.PgError{
			Severity: "SCHWERWIEGEND", SeverityUnlocalized: "FATAL",
			Code: "57P01", Message: "constructed",
		}, false},
		"a localised ERROR": {&pgconn.PgError{
			Severity: "FEHLER", SeverityUnlocalized: "ERROR",
			Code: "23505", Message: "constructed",
		}, true},
		// A server too old to send the unlocalized field: the localised one is
		// all there is, and the fallback reads it.
		"an old server sending only Severity": {&pgconn.PgError{
			Severity: "FATAL", Code: "57P01", Message: "constructed",
		}, false},
		// The cross-product the two cases above miss: an old server AND
		// another language, so the only severity field there is is
		// translated. A negative check calls this a refusal.
		"an old non-English server": {&pgconn.PgError{
			Severity: "SCHWERWIEGEND", Code: "57P01", Message: "constructed",
		}, false},
		// And its mirror: a translated ERROR on an old server is also not
		// recognised, which is the cost of checking positively and is the safe
		// direction — a human looks at a database that turns out to be
		// unchanged, rather than being told nothing happened when it may have.
		"an old non-English server declining": {&pgconn.PgError{
			Severity: "FEHLER", Code: "23505", Message: "constructed",
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CommitWasRefused(c.err); got != c.want {
				t.Errorf("CommitWasRefused(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
