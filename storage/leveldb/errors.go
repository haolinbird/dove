package leveldb

type DbError struct {
	code ErrorCode
	m    string
}
type ErrorCode int

const (
	ErrorDbLockFailed   = ErrorCode(1)
	ErrorDbOpenFailed   = ErrorCode(2)
	ErrorDbCreateFailed = ErrorCode(3)
)

func (e DbError) Error() string {
	return e.m
}
func (e DbError) Code() ErrorCode {
	return e.code
}

func NewError(code ErrorCode, m string) error {
	return DbError{code, m}
}
