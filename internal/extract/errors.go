package extract

// StatusError carries the HTTP status code a caller should respond with.
type StatusError struct {
	Status int
	Msg    string
}

func (e *StatusError) Error() string {
	return e.Msg
}
