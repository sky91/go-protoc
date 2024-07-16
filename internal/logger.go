package internal

type FuncLogger func(format string, args ...any)

func (thisV FuncLogger) Infof(format string, args ...any) {
	thisV(format, args...)
}

func (thisV FuncLogger) Errorf(format string, args ...any) {
	thisV(format, args...)
}
