package handler

import (
	"fmt"
	// "reflect"
)

type Handler interface {
	Process(args map[string]interface{}) ([]byte, error)
}

var handlers map[string]Handler

func init() {
	handlers = map[string]Handler{}
}

type ErrorHandleNotExists struct {
	Message string
}

func (e ErrorHandleNotExists) Error() string {
	return e.Message
}

func GetHandlers() map[string]Handler {
	return handlers
}

func HandlerExists(n string) bool {
	return handlers[n] != nil
}

func Run(n string, args map[string]interface{}) ([]byte, error) {
	if !HandlerExists(n) {
		return nil, ErrorHandleNotExists{fmt.Sprintf("Method \"%s\" not exists!", n)}
	}
	return handlers[n].Process(args)
}
