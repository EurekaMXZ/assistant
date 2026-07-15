package tool

import "errors"

type recoverableToolError struct {
	err error
}

func (e recoverableToolError) Error() string {
	return e.err.Error()
}

func (e recoverableToolError) Unwrap() error {
	return e.err
}

func RecoverableError(err error) error {
	if err == nil || IsRecoverableError(err) {
		return err
	}
	return recoverableToolError{err: err}
}

func IsRecoverableError(err error) bool {
	var target recoverableToolError
	return errors.As(err, &target)
}
