package common

var (
	journal runJournal
)

type (
	runJournal interface {
		Post(string, string, string, ...interface{}) error
	}
)

func RegisterJournal(target runJournal) {
	journal = target
}

func RunJournal(event, detail, commentForm string, fields ...interface{}) error {
	if journal != nil {
		return journal.Post(event, detail, commentForm, fields...)
	}
	return nil
}
